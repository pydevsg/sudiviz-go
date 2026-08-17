// Package remediators contains the built-in remediation planners.
package remediators

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/fix"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

var portRe = regexp.MustCompile(`port (\d+)`)

// SGIngress adds the missing ALB→instance security-group ingress rule.
type SGIngress struct{}

func (SGIngress) Name() string { return "sg_ingress" }

func (SGIngress) Match(f diagnose.Finding) bool {
	return strings.Contains(f.Title, "Security group missing port")
}

func (SGIngress) Plan(f diagnose.Finding, g *graph.InfraGraph, region string) *fix.Action {
	port := 80
	if m := portRe.FindStringSubmatch(f.Title); m != nil {
		port, _ = strconv.Atoi(m[1])
	}

	// Walk the graph for context: the instance's SGs and the ALB's SGs.
	var instanceSGIDs, albSGIDs []string
	if inst := g.Resource(f.ResourceID); inst != nil {
		for _, e := range g.OutEdges(inst.ID, graph.RelationGuardedBy) {
			if sg := g.Resource(e.To); sg != nil && sg.Kind == graph.KindSecurityGroup {
				instanceSGIDs = append(instanceSGIDs, sg.ID)
			}
		}
		// instance -> registered_in -> TG <- forwards_to <- ALB
		for _, reg := range g.OutEdges(inst.ID, graph.RelationRegisteredIn) {
			for _, fwd := range g.InEdges(reg.To, graph.RelationForwardsTo) {
				if alb := g.Resource(fwd.From); alb != nil {
					if sgs := alb.AttrStrings("security_group_ids"); len(sgs) > 0 {
						albSGIDs = sgs
						break
					}
				}
			}
			if len(albSGIDs) > 0 {
				break
			}
		}
	}
	if len(albSGIDs) == 0 {
		for _, alb := range g.ResourcesByKind(graph.KindALB) {
			if sgs := alb.AttrStrings("security_group_ids"); len(sgs) > 0 {
				albSGIDs = sgs
				break
			}
		}
	}

	targetSG := "sg-INSTANCE_SG_ID"
	if len(instanceSGIDs) > 0 {
		targetSG = instanceSGIDs[0]
	}
	sourceSG := "sg-ALB_SG_ID"
	if len(albSGIDs) > 0 {
		sourceSG = albSGIDs[0]
	}

	cli := fmt.Sprintf(
		"aws ec2 authorize-security-group-ingress \\\n"+
			"  --region %s \\\n"+
			"  --group-id %s \\\n"+
			"  --protocol tcp \\\n"+
			"  --port %d \\\n"+
			"  --source-group %s",
		region, targetSG, port, sourceSG)

	return fix.NewAction(
		f,
		cli,
		fmt.Sprintf("Add inbound rule to %s: allow TCP/%d from %s", targetSG, port, sourceSG),
		false,
		func(ctx context.Context, c *fix.AWSClients) error {
			_, err := c.EC2.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
				GroupId: aws.String(targetSG),
				IpPermissions: []ec2types.IpPermission{{
					IpProtocol: aws.String("tcp"),
					FromPort:   aws.Int32(int32(port)),
					ToPort:     aws.Int32(int32(port)),
					UserIdGroupPairs: []ec2types.UserIdGroupPair{
						{GroupId: aws.String(sourceSG)},
					},
				}},
			})
			return err
		},
	)
}
