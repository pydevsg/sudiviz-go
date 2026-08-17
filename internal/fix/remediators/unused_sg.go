package remediators

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/fix"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// UnusedSecurityGroup deletes a security group attached to nothing.
// Destructive: requires --force.
type UnusedSecurityGroup struct{}

func (UnusedSecurityGroup) Name() string { return "unused_sg" }

func (UnusedSecurityGroup) Match(f diagnose.Finding) bool {
	return strings.Contains(f.Title, "Unused security group")
}

func (UnusedSecurityGroup) Plan(f diagnose.Finding, _ *graph.InfraGraph, region string) *fix.Action {
	sgID := f.ResourceID
	if sgID == "" {
		sgID = "sg-SECURITY_GROUP_ID"
	}
	cli := fmt.Sprintf(
		"aws ec2 delete-security-group \\\n"+
			"  --region %s \\\n"+
			"  --group-id %s",
		region, sgID)

	return fix.NewAction(
		f,
		cli,
		fmt.Sprintf("Delete unused security group: %s", sgID),
		true,
		func(ctx context.Context, c *fix.AWSClients) error {
			_, err := c.EC2.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
				GroupId: aws.String(sgID),
			})
			return err
		},
	)
}
