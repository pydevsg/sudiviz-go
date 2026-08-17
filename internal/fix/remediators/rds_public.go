package remediators

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/fix"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// RDSPublicAccess disables public accessibility on an RDS instance.
type RDSPublicAccess struct{}

func (RDSPublicAccess) Name() string { return "rds_public" }

func (RDSPublicAccess) Match(f diagnose.Finding) bool {
	return strings.Contains(f.Title, "publicly accessible") && strings.Contains(f.Title, "RDS")
}

func (RDSPublicAccess) Plan(f diagnose.Finding, _ *graph.InfraGraph, region string) *fix.Action {
	dbID := "DB_INSTANCE_ID"
	if f.ResourceID != "" {
		parts := strings.Split(f.ResourceID, ":")
		dbID = parts[len(parts)-1]
	}
	cli := fmt.Sprintf(
		"aws rds modify-db-instance \\\n"+
			"  --region %s \\\n"+
			"  --db-instance-identifier %s \\\n"+
			"  --no-publicly-accessible \\\n"+
			"  --apply-immediately",
		region, dbID)

	return fix.NewAction(
		f,
		cli,
		fmt.Sprintf("Disable public accessibility on RDS: %s", dbID),
		false,
		func(ctx context.Context, c *fix.AWSClients) error {
			_, err := c.RDS.ModifyDBInstance(ctx, &rds.ModifyDBInstanceInput{
				DBInstanceIdentifier: aws.String(dbID),
				PubliclyAccessible:   aws.Bool(false),
				ApplyImmediately:     aws.Bool(true),
			})
			return err
		},
	)
}
