package discovery

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// S3API is the subset of the S3 client used by the bucket discoverer.
type S3API interface {
	ListBuckets(ctx context.Context, in *s3.ListBucketsInput, opts ...func(*s3.Options)) (*s3.ListBucketsOutput, error)
	GetBucketLocation(ctx context.Context, in *s3.GetBucketLocationInput, opts ...func(*s3.Options)) (*s3.GetBucketLocationOutput, error)
	GetBucketVersioning(ctx context.Context, in *s3.GetBucketVersioningInput, opts ...func(*s3.Options)) (*s3.GetBucketVersioningOutput, error)
	GetPublicAccessBlock(ctx context.Context, in *s3.GetPublicAccessBlockInput, opts ...func(*s3.Options)) (*s3.GetPublicAccessBlockOutput, error)
	GetBucketEncryption(ctx context.Context, in *s3.GetBucketEncryptionInput, opts ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error)
	GetBucketTagging(ctx context.Context, in *s3.GetBucketTaggingInput, opts ...func(*s3.Options)) (*s3.GetBucketTaggingOutput, error)
}

// S3Discoverer discovers buckets in the session's region along with their
// public-access-block and encryption configuration.
type S3Discoverer struct {
	client S3API
	region string
	opts   Options
}

// NewS3Discoverer builds an S3 discoverer scoped to the session region.
func NewS3Discoverer(client S3API, region string, opts Options) *S3Discoverer {
	if region == "" {
		region = "us-east-1"
	}
	return &S3Discoverer{client: client, region: region, opts: opts}
}

// ServiceName implements Discoverer.
func (d *S3Discoverer) ServiceName() string { return "s3" }

// Discover implements Discoverer.
func (d *S3Discoverer) Discover(ctx context.Context) (*Result, error) {
	resp, err := d.client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("list buckets: %w", err)
	}

	res := &Result{}
	for _, raw := range resp.Buckets {
		name := aws.ToString(raw.Name)

		// S3 is global but buckets have a home region; scope the graph to
		// the selected region.
		region := d.region
		if loc, err := d.client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{Bucket: aws.String(name)}); err == nil {
			if lc := string(loc.LocationConstraint); lc != "" {
				region = lc
			} else {
				region = "us-east-1"
			}
		}
		if region != d.region {
			continue
		}

		versioning := false
		if v, err := d.client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(name)}); err == nil {
			versioning = v.Status == s3types.BucketVersioningStatusEnabled
		}

		// If the API call fails (no PAB configured), the bucket is treated as
		// blocked=true only when the account default applies; the Python
		// version defaults to true on error, so mirror that.
		publicBlocked := true
		if pab, err := d.client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: aws.String(name)}); err == nil && pab.PublicAccessBlockConfiguration != nil {
			c := pab.PublicAccessBlockConfiguration
			publicBlocked = aws.ToBool(c.BlockPublicAcls) &&
				aws.ToBool(c.BlockPublicPolicy) &&
				aws.ToBool(c.IgnorePublicAcls) &&
				aws.ToBool(c.RestrictPublicBuckets)
		}

		encryption := false
		if enc, err := d.client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{Bucket: aws.String(name)}); err == nil {
			encryption = enc.ServerSideEncryptionConfiguration != nil &&
				len(enc.ServerSideEncryptionConfiguration.Rules) > 0
		}

		tags := map[string]string{}
		if tagging, err := d.client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: aws.String(name)}); err == nil {
			for _, t := range tagging.TagSet {
				tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
		}

		created := ""
		if raw.CreationDate != nil {
			created = raw.CreationDate.UTC().Format("2006-01-02T15:04:05Z")
		}

		// A publicly-exposable bucket is a security problem — surface it as
		// unhealthy so every renderer flags it.
		health := graph.HealthHealthy
		if !publicBlocked {
			health = graph.HealthUnhealthy
		}

		res.Resources = append(res.Resources, graph.Resource{
			ID:          "arn:aws:s3:::" + name,
			Kind:        graph.KindS3,
			Label:       name,
			Provider:    "aws",
			Region:      region,
			Health:      health,
			MonthlyCost: EstimateS3Cost(),
			Tags:        tags,
			Attrs: map[string]any{
				"region":                region,
				"creation_date":         created,
				"versioning_enabled":    versioning,
				"public_access_blocked": publicBlocked,
				"encryption_enabled":    encryption,
			},
		})
	}
	return res, nil
}
