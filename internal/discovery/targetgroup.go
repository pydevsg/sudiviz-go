package discovery

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// TargetGroupAPI is the subset of the ELBv2 client used by the target-group
// discoverer.
type TargetGroupAPI interface {
	DescribeTargetGroups(ctx context.Context, in *elbv2.DescribeTargetGroupsInput, opts ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error)
	DescribeTargetHealth(ctx context.Context, in *elbv2.DescribeTargetHealthInput, opts ...func(*elbv2.Options)) (*elbv2.DescribeTargetHealthOutput, error)
	DescribeTags(ctx context.Context, in *elbv2.DescribeTagsInput, opts ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error)
}

// TargetGroupDiscoverer discovers target groups, per-target health, and the
// target→TG registration edges.
type TargetGroupDiscoverer struct {
	client TargetGroupAPI
	opts   Options
}

// NewTargetGroupDiscoverer builds a target-group discoverer.
func NewTargetGroupDiscoverer(client TargetGroupAPI, opts Options) *TargetGroupDiscoverer {
	return &TargetGroupDiscoverer{client: client, opts: opts}
}

// ServiceName implements Discoverer.
func (d *TargetGroupDiscoverer) ServiceName() string { return "target_group" }

// healthMap normalizes ELBv2 TargetHealth states.
var healthMap = map[elbv2types.TargetHealthStateEnum]graph.Health{
	elbv2types.TargetHealthStateEnumHealthy:     graph.HealthHealthy,
	elbv2types.TargetHealthStateEnumUnhealthy:   graph.HealthUnhealthy,
	elbv2types.TargetHealthStateEnumInitial:     graph.HealthInitial,
	elbv2types.TargetHealthStateEnumDraining:    graph.HealthDraining,
	elbv2types.TargetHealthStateEnumUnused:      graph.HealthUnused,
	elbv2types.TargetHealthStateEnumUnavailable: graph.HealthUnknown,
}

// Discover implements Discoverer.
func (d *TargetGroupDiscoverer) Discover(ctx context.Context) (*Result, error) {
	var tgs []elbv2types.TargetGroup
	var marker *string
	for {
		page, err := d.client.DescribeTargetGroups(ctx, &elbv2.DescribeTargetGroupsInput{Marker: marker})
		if err != nil {
			return nil, fmt.Errorf("describe target groups: %w", err)
		}
		for _, tg := range page.TargetGroups {
			if d.opts.VPCID != "" && aws.ToString(tg.VpcId) != d.opts.VPCID {
				continue
			}
			tgs = append(tgs, tg)
		}
		if page.NextMarker == nil {
			break
		}
		marker = page.NextMarker
	}
	if len(tgs) == 0 {
		return &Result{}, nil
	}

	arns := make([]string, 0, len(tgs))
	for _, tg := range tgs {
		arns = append(arns, aws.ToString(tg.TargetGroupArn))
	}
	tagsByARN, err := describeELBTags(ctx, d.client, arns)
	if err != nil {
		return nil, err
	}

	res := &Result{}
	for _, tg := range tgs {
		arn := aws.ToString(tg.TargetGroupArn)
		targetType := string(tg.TargetType)
		if targetType == "" {
			targetType = "instance"
		}

		health, hErr := d.client.DescribeTargetHealth(ctx, &elbv2.DescribeTargetHealthInput{
			TargetGroupArn: aws.String(arn),
		})
		var descriptions []elbv2types.TargetHealthDescription
		if hErr == nil {
			descriptions = health.TargetHealthDescriptions
		}

		healthy, total := 0, 0
		var targets []map[string]any
		for _, h := range descriptions {
			if h.Target == nil {
				continue
			}
			total++
			state := graph.HealthUnknown
			reason, description := "", ""
			if h.TargetHealth != nil {
				if mapped, ok := healthMap[h.TargetHealth.State]; ok {
					state = mapped
				}
				reason = string(h.TargetHealth.Reason)
				description = aws.ToString(h.TargetHealth.Description)
			}
			if state == graph.HealthHealthy {
				healthy++
			}
			targetID := aws.ToString(h.Target.Id)
			targets = append(targets, map[string]any{
				"target_id":   targetID,
				"target_type": targetType,
				"port":        int(aws.ToInt32(h.Target.Port)),
				"health":      string(state),
				"health_reason": func() any {
					if reason == "" {
						return nil
					}
					return reason
				}(),
				"health_description": description,
			})

			// IP-type targets appear as phantom floating nodes without
			// topology context — skip them (matches the Python builder).
			if targetType == "ip" {
				continue
			}
			kind := graph.KindInstance
			if targetType != "instance" {
				kind = graph.Kind(targetType)
			}
			res.Resources = append(res.Resources, graph.Resource{
				ID:       targetID,
				Kind:     kind,
				Label:    targetID,
				Provider: "aws",
				Region:   d.opts.Region,
				Health:   state,
				Attrs:    map[string]any{"target_type": targetType},
			})
			res.Edges = append(res.Edges, graph.Edge{
				From: targetID, To: arn, Relation: graph.RelationRegisteredIn,
				Attrs: map[string]any{"health": string(state), "health_reason": reason},
			})
		}

		res.Resources = append(res.Resources, graph.Resource{
			ID:       arn,
			Kind:     graph.KindTargetGroup,
			Label:    aws.ToString(tg.TargetGroupName),
			Provider: "aws",
			Region:   d.opts.Region,
			Health:   aggregateTargetHealth(targets),
			Tags:     tagsByARN[arn],
			Attrs: map[string]any{
				"protocol":      string(tg.Protocol),
				"port":          int(aws.ToInt32(tg.Port)),
				"vpc_id":        aws.ToString(tg.VpcId),
				"target_type":   targetType,
				"targets":       targets,
				"healthy_count": healthy,
				"total_count":   total,
			},
		})
	}
	return res, nil
}

// aggregateTargetHealth reduces per-target health into a TG-level status:
// all healthy → healthy; none → unknown (surfaced as orphan); any unhealthy
// → unhealthy; only initial → initial; mixed → unhealthy.
func aggregateTargetHealth(targets []map[string]any) graph.Health {
	if len(targets) == 0 {
		return graph.HealthUnknown
	}
	states := map[string]bool{}
	for _, t := range targets {
		if s, ok := t["health"].(string); ok {
			states[s] = true
		}
	}
	switch {
	case len(states) == 1 && states[string(graph.HealthHealthy)]:
		return graph.HealthHealthy
	case states[string(graph.HealthUnhealthy)]:
		return graph.HealthUnhealthy
	case len(states) == 1 && states[string(graph.HealthInitial)]:
		return graph.HealthInitial
	default:
		return graph.HealthUnhealthy
	}
}
