package remediators

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/fix"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// S3PublicAccess enables the full Block Public Access configuration.
type S3PublicAccess struct{}

func (S3PublicAccess) Name() string { return "s3_public" }

func (S3PublicAccess) Match(f diagnose.Finding) bool {
	return strings.Contains(f.Title, "public access not fully blocked")
}

func (S3PublicAccess) Plan(f diagnose.Finding, _ *graph.InfraGraph, _ string) *fix.Action {
	bucket := bucketNameFromARN(f.ResourceID)
	cli := fmt.Sprintf(
		"aws s3api put-public-access-block \\\n"+
			"  --bucket %s \\\n"+
			"  --public-access-block-configuration \\\n"+
			"    BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true",
		bucket)

	return fix.NewAction(
		f,
		cli,
		fmt.Sprintf("Enable public access block on S3 bucket: %s", bucket),
		false,
		func(ctx context.Context, c *fix.AWSClients) error {
			_, err := c.S3.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
				Bucket: aws.String(bucket),
				PublicAccessBlockConfiguration: &s3types.PublicAccessBlockConfiguration{
					BlockPublicAcls:       aws.Bool(true),
					IgnorePublicAcls:      aws.Bool(true),
					BlockPublicPolicy:     aws.Bool(true),
					RestrictPublicBuckets: aws.Bool(true),
				},
			})
			return err
		},
	)
}

func bucketNameFromARN(arn string) string {
	if arn == "" {
		return "BUCKET_NAME"
	}
	return strings.TrimPrefix(arn, "arn:aws:s3:::")
}
