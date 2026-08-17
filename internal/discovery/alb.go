package discovery

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// ELBV2API is the subset of the ELBv2 client used by the ALB discoverer.
type ELBV2API interface {
	DescribeLoadBalancers(ctx context.Context, in *elbv2.DescribeLoadBalancersInput, opts ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error)
	DescribeListeners(ctx context.Context, in *elbv2.DescribeListenersInput, opts ...func(*elbv2.Options)) (*elbv2.DescribeListenersOutput, error)
	DescribeRules(ctx context.Context, in *elbv2.DescribeRulesInput, opts ...func(*elbv2.Options)) (*elbv2.DescribeRulesOutput, error)
	DescribeTags(ctx context.Context, in *elbv2.DescribeTagsInput, opts ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error)
}

// ALBDiscoverer discovers ALB/NLB load balancers, their listeners, and the
// listener→target-group forwarding edges that prove reachability.
type ALBDiscoverer struct {
	client ELBV2API
	opts   Options
}

// NewALBDiscoverer builds an ALB discoverer over the given client.
func NewALBDiscoverer(client ELBV2API, opts Options) *ALBDiscoverer {
	return &ALBDiscoverer{client: client, opts: opts}
}

// ServiceName implements Discoverer.
func (d *ALBDiscoverer) ServiceName() string { return "alb" }

// Discover implements Discoverer.
func (d *ALBDiscoverer) Discover(ctx context.Context) (*Result, error) {
	var lbs []elbv2types.LoadBalancer
	var marker *string
	for {
		page, err := d.client.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{Marker: marker})
		if err != nil {
			return nil, fmt.Errorf("describe load balancers: %w", err)
		}
		for _, lb := range page.LoadBalancers {
			if d.opts.VPCID != "" && aws.ToString(lb.VpcId) != d.opts.VPCID {
				continue
			}
			lbs = append(lbs, lb)
		}
		if page.NextMarker == nil {
			break
		}
		marker = page.NextMarker
	}
	if len(lbs) == 0 {
		return &Result{}, nil
	}

	arns := make([]string, 0, len(lbs))
	for _, lb := range lbs {
		arns = append(arns, aws.ToString(lb.LoadBalancerArn))
	}
	tagsByARN, err := describeELBTags(ctx, d.client, arns)
	if err != nil {
		return nil, err
	}

	tagFilter := d.opts.TagFilter()
	res := &Result{}
	for _, lb := range lbs {
		arn := aws.ToString(lb.LoadBalancerArn)
		tags := tagsByARN[arn]
		if !matchesTags(tags, tagFilter) {
			continue
		}

		state := "unknown"
		if lb.State != nil {
			state = string(lb.State.Code)
		}
		health := graph.HealthUnknown
		if state == "active" {
			health = graph.HealthHealthy
		}
		lbType := string(lb.Type)
		if lbType == "" {
			lbType = "application"
		}
		var sgIDs []string
		sgIDs = append(sgIDs, lb.SecurityGroups...)
		var subnetIDs []string
		for _, az := range lb.AvailabilityZones {
			if az.SubnetId != nil {
				subnetIDs = append(subnetIDs, *az.SubnetId)
			}
		}

		res.Resources = append(res.Resources, graph.Resource{
			ID:          arn,
			Kind:        graph.KindALB,
			Label:       aws.ToString(lb.LoadBalancerName),
			Provider:    "aws",
			Region:      d.opts.Region,
			Health:      health,
			MonthlyCost: EstimateLBCost(lbType, state),
			Tags:        tags,
			Attrs: map[string]any{
				"dns_name":           aws.ToString(lb.DNSName),
				"scheme":             string(lb.Scheme),
				"type":               lbType,
				"state":              state,
				"vpc_id":             aws.ToString(lb.VpcId),
				"security_group_ids": sgIDs,
				"subnet_ids":         subnetIDs,
			},
		})

		// in_vpc is conditional: only materializes when the VPC node exists.
		if vpc := aws.ToString(lb.VpcId); vpc != "" {
			res.ConditionalEdges = append(res.ConditionalEdges, graph.Edge{
				From: arn, To: vpc, Relation: graph.RelationInVPC,
			})
		}
		for _, sg := range sgIDs {
			res.Edges = append(res.Edges, graph.Edge{From: arn, To: sg, Relation: graph.RelationGuardedBy})
		}

		if err := d.discoverListenerEdges(ctx, arn, res); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// discoverListenerEdges unrolls every listener (and every listener rule) into
// forwards_to edges. A target group that never appears as a target of these
// edges is an orphan.
func (d *ALBDiscoverer) discoverListenerEdges(ctx context.Context, lbARN string, res *Result) error {
	var marker *string
	for {
		page, err := d.client.DescribeListeners(ctx, &elbv2.DescribeListenersInput{
			LoadBalancerArn: aws.String(lbARN),
			Marker:          marker,
		})
		if err != nil {
			return fmt.Errorf("describe listeners for %s: %w", lbARN, err)
		}
		for _, lst := range page.Listeners {
			tgARNs := extractTargetGroupARNs(lst.DefaultActions)

			var ruleMarker *string
			for {
				rules, err := d.client.DescribeRules(ctx, &elbv2.DescribeRulesInput{
					ListenerArn: lst.ListenerArn,
					Marker:      ruleMarker,
				})
				if err != nil {
					return fmt.Errorf("describe rules for %s: %w", aws.ToString(lst.ListenerArn), err)
				}
				for _, rule := range rules.Rules {
					tgARNs = append(tgARNs, extractTargetGroupARNs(rule.Actions)...)
				}
				if rules.NextMarker == nil {
					break
				}
				ruleMarker = rules.NextMarker
			}

			seen := map[string]bool{}
			for _, tg := range tgARNs {
				if seen[tg] {
					continue
				}
				seen[tg] = true
				res.Edges = append(res.Edges, graph.Edge{
					From: lbARN, To: tg, Relation: graph.RelationForwardsTo,
					Attrs: map[string]any{
						"listener_arn":      aws.ToString(lst.ListenerArn),
						"listener_port":     int(aws.ToInt32(lst.Port)),
						"listener_protocol": string(lst.Protocol),
					},
				})
			}
		}
		if page.NextMarker == nil {
			break
		}
		marker = page.NextMarker
	}
	return nil
}

// extractTargetGroupARNs walks an Actions array and pulls out TG ARNs from
// both direct TargetGroupArn references and ForwardConfig weighted groups.
func extractTargetGroupARNs(actions []elbv2types.Action) []string {
	var arns []string
	for _, action := range actions {
		if action.TargetGroupArn != nil {
			arns = append(arns, *action.TargetGroupArn)
		}
		if action.ForwardConfig != nil {
			for _, tg := range action.ForwardConfig.TargetGroups {
				if tg.TargetGroupArn != nil {
					arns = append(arns, *tg.TargetGroupArn)
				}
			}
		}
	}
	return arns
}

// elbTagsAPI is the minimal client capability needed to fetch ELBv2 tags.
type elbTagsAPI interface {
	DescribeTags(ctx context.Context, in *elbv2.DescribeTagsInput, opts ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error)
}

// describeELBTags fetches ELBv2 resource tags in batches of 20 (API limit).
func describeELBTags(ctx context.Context, client elbTagsAPI, arns []string) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	for i := 0; i < len(arns); i += 20 {
		end := min(i+20, len(arns))
		resp, err := client.DescribeTags(ctx, &elbv2.DescribeTagsInput{ResourceArns: arns[i:end]})
		if err != nil {
			return nil, fmt.Errorf("describe tags: %w", err)
		}
		for _, td := range resp.TagDescriptions {
			tags := map[string]string{}
			for _, t := range td.Tags {
				tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			out[aws.ToString(td.ResourceArn)] = tags
		}
	}
	return out, nil
}
