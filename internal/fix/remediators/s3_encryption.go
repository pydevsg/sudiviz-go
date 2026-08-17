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

// S3Encryption enables SSE-S3 default encryption on a bucket.
type S3Encryption struct{}

func (S3Encryption) Name() string { return "s3_encryption" }

func (S3Encryption) Match(f diagnose.Finding) bool {
	return strings.Contains(f.Title, "server-side encryption not enabled")
}

func (S3Encryption) Plan(f diagnose.Finding, _ *graph.InfraGraph, _ string) *fix.Action {
	bucket := bucketNameFromARN(f.ResourceID)
	cli := fmt.Sprintf(
		"aws s3api put-bucket-encryption \\\n"+
			"  --bucket %s \\\n"+
			"  --server-side-encryption-configuration \\\n"+
			`    '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}'`,
		bucket)

	return fix.NewAction(
		f,
		cli,
		fmt.Sprintf("Enable SSE-S3 encryption on bucket: %s", bucket),
		false,
		func(ctx context.Context, c *fix.AWSClients) error {
			_, err := c.S3.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
				Bucket: aws.String(bucket),
				ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
					Rules: []s3types.ServerSideEncryptionRule{{
						ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
							SSEAlgorithm: s3types.ServerSideEncryptionAes256,
						},
					}},
				},
			})
			return err
		},
	)
}
