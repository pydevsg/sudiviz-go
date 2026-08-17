package remediators

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/fix"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// UnhealthyTargets deregisters targets that are failing health checks from
// their target group. Marked destructive (requires --force) because it takes
// backends out of rotation.
type UnhealthyTargets struct{}

func (UnhealthyTargets) Name() string { return "unhealthy_targets" }

func (UnhealthyTargets) Match(f diagnose.Finding) bool {
	return strings.HasPrefix(f.Title, "Target group '") && strings.Contains(f.Title, "healthy")
}

func (UnhealthyTargets) Plan(f diagnose.Finding, g *graph.InfraGraph, region string) *fix.Action {
	tgARN := f.ResourceID
	unhealthy := unhealthyTargetIDs(g, tgARN)
	if len(unhealthy) == 0 {
		return fix.NewAction(
			f,
			fmt.Sprintf("# No automated fix available for: %s", f.Title),
			fmt.Sprintf("Manual intervention required: %s", f.Detail),
			false,
			nil,
		)
	}

	cli := fmt.Sprintf(
		"aws elbv2 deregister-targets \\\n"+
			"  --region %s \\\n"+
			"  --target-group-arn %s \\\n"+
			"  --targets %s",
		region, tgARN, "Id="+strings.Join(unhealthy, " Id="))

	targets := make([]elbv2types.TargetDescription, 0, len(unhealthy))
	for _, id := range unhealthy {
		targets = append(targets, elbv2types.TargetDescription{Id: aws.String(id)})
	}

	return fix.NewAction(
		f,
		cli,
		fmt.Sprintf("Deregister %d unhealthy target(s) from %s", len(unhealthy), tgARN),
		true,
		func(ctx context.Context, c *fix.AWSClients) error {
			_, err := c.ELBV2.DeregisterTargets(ctx, &elbv2.DeregisterTargetsInput{
				TargetGroupArn: aws.String(tgARN),
				Targets:        targets,
			})
			return err
		},
	)
}

// unhealthyTargetIDs pulls the failing target IDs out of the TG node's attrs.
func unhealthyTargetIDs(g *graph.InfraGraph, tgARN string) []string {
	tg := g.Resource(tgARN)
	if tg == nil {
		return nil
	}
	rawTargets, _ := tg.Attr("targets").([]map[string]any)
	var out []string
	for _, t := range rawTargets {
		health, _ := t["health"].(string)
		id, _ := t["target_id"].(string)
		if id != "" && health != string(graph.HealthHealthy) {
			out = append(out, id)
		}
	}
	return out
}
