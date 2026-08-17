package remediators

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/fix"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// OrphanTargetGroup deletes a target group nothing routes to. Destructive:
// requires --force.
type OrphanTargetGroup struct{}

func (OrphanTargetGroup) Name() string { return "orphan_tg" }

func (OrphanTargetGroup) Match(f diagnose.Finding) bool {
	return strings.Contains(f.Title, "Orphan target group")
}

func (OrphanTargetGroup) Plan(f diagnose.Finding, _ *graph.InfraGraph, region string) *fix.Action {
	tgARN := f.ResourceID
	if tgARN == "" {
		tgARN = "arn:aws:elasticloadbalancing:REGION:ACCOUNT:targetgroup/NAME/ID"
	}
	cli := fmt.Sprintf(
		"aws elbv2 delete-target-group \\\n"+
			"  --region %s \\\n"+
			"  --target-group-arn %s",
		region, tgARN)

	return fix.NewAction(
		f,
		cli,
		fmt.Sprintf("Delete orphan target group: %s", f.ResourceID),
		true,
		func(ctx context.Context, c *fix.AWSClients) error {
			_, err := c.ELBV2.DeleteTargetGroup(ctx, &elbv2.DeleteTargetGroupInput{
				TargetGroupArn: aws.String(tgARN),
			})
			return err
		},
	)
}
