package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

type mockS3 struct {
	buckets   []s3types.Bucket
	locations map[string]s3types.BucketLocationConstraint
	pab       map[string]*s3types.PublicAccessBlockConfiguration
	enc       map[string]bool
}

func (m *mockS3) ListBuckets(ctx context.Context, in *s3.ListBucketsInput, _ ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	return &s3.ListBucketsOutput{Buckets: m.buckets}, nil
}
func (m *mockS3) GetBucketLocation(ctx context.Context, in *s3.GetBucketLocationInput, _ ...func(*s3.Options)) (*s3.GetBucketLocationOutput, error) {
	return &s3.GetBucketLocationOutput{LocationConstraint: m.locations[aws.ToString(in.Bucket)]}, nil
}
func (m *mockS3) GetBucketVersioning(ctx context.Context, in *s3.GetBucketVersioningInput, _ ...func(*s3.Options)) (*s3.GetBucketVersioningOutput, error) {
	return &s3.GetBucketVersioningOutput{Status: s3types.BucketVersioningStatusEnabled}, nil
}
func (m *mockS3) GetPublicAccessBlock(ctx context.Context, in *s3.GetPublicAccessBlockInput, _ ...func(*s3.Options)) (*s3.GetPublicAccessBlockOutput, error) {
	return &s3.GetPublicAccessBlockOutput{PublicAccessBlockConfiguration: m.pab[aws.ToString(in.Bucket)]}, nil
}
func (m *mockS3) GetBucketEncryption(ctx context.Context, in *s3.GetBucketEncryptionInput, _ ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
	if !m.enc[aws.ToString(in.Bucket)] {
		return &s3.GetBucketEncryptionOutput{}, nil
	}
	return &s3.GetBucketEncryptionOutput{
		ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
			Rules: []s3types.ServerSideEncryptionRule{{}},
		},
	}, nil
}
func (m *mockS3) GetBucketTagging(ctx context.Context, in *s3.GetBucketTaggingInput, _ ...func(*s3.Options)) (*s3.GetBucketTaggingOutput, error) {
	return &s3.GetBucketTaggingOutput{}, nil
}

func TestS3Discoverer(t *testing.T) {
	now := time.Now()
	m := &mockS3{
		buckets: []s3types.Bucket{
			{Name: aws.String("open-bucket"), CreationDate: &now},
			{Name: aws.String("other-region"), CreationDate: &now},
		},
		locations: map[string]s3types.BucketLocationConstraint{
			"open-bucket":  "",
			"other-region": "eu-west-1",
		},
		pab: map[string]*s3types.PublicAccessBlockConfiguration{
			"open-bucket": {
				BlockPublicAcls: aws.Bool(false), IgnorePublicAcls: aws.Bool(true),
				BlockPublicPolicy: aws.Bool(true), RestrictPublicBuckets: aws.Bool(true),
			},
		},
		enc: map[string]bool{"open-bucket": true},
	}
	res, err := NewS3Discoverer(m, "us-east-1", Options{}).Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Resources, 1)
	assert.Equal(t, "open-bucket", res.Resources[0].Label)
	assert.False(t, res.Resources[0].AttrBool("public_access_blocked"))
	assert.Equal(t, graph.HealthUnhealthy, res.Resources[0].Health)
	assert.True(t, res.Resources[0].AttrBool("encryption_enabled"))
}

func TestParseServiceTag(t *testing.T) {
	assert.Equal(t, map[string]string{"Service": "web"}, ParseServiceTag("Service=web"))
	assert.Equal(t, map[string]string{"a": "1", "b": "2"}, ParseServiceTag("a=1,b=2"))
}

type stubDiscoverer struct {
	name string
	res  *Result
	err  error
}

func (s stubDiscoverer) ServiceName() string                       { return s.name }
func (s stubDiscoverer) Discover(context.Context) (*Result, error) { return s.res, s.err }

func TestDiscoverAllAssemblesAndWiresS3(t *testing.T) {
	g, errs := DiscoverAll(context.Background(), []Discoverer{
		stubDiscoverer{name: "alb", res: &Result{Resources: []graph.Resource{{ID: "alb-1", Kind: graph.KindALB, Label: "alb"}}}},
		stubDiscoverer{name: "s3", res: &Result{Resources: []graph.Resource{{ID: "arn:aws:s3:::app-logs", Kind: graph.KindS3, Label: "app-logs"}}}},
		stubDiscoverer{name: "ec2", err: assert.AnError},
	}, Meta{AccountID: "111", Region: "us-east-1"})
	require.NotNil(t, g)
	assert.Len(t, errs, 1)
	assert.True(t, g.HasNode("alb-1"))
	assert.True(t, g.HasNode("arn:aws:s3:::app-logs"))
	found := false
	for _, e := range g.Edges() {
		if e.Relation == graph.RelationLogsTo {
			found = true
		}
	}
	assert.True(t, found)
}

func TestCosts(t *testing.T) {
	assert.Greater(t, EstimateInstanceCost("t3.small", "running"), 0.0)
	assert.Equal(t, 0.0, EstimateInstanceCost("t3.small", "stopped"))
	assert.Equal(t, albMonthlyBase, EstimateLBCost("application", "active"))
	assert.Equal(t, "Free", FormatCost(0))
	g := graph.New()
	g.AddResource(graph.Resource{ID: "i", Kind: graph.KindInstance, MonthlyCost: 10})
	sum := SummarizeCosts(g)
	assert.Equal(t, 10.0, sum.Total)
	assert.Equal(t, 10.0, sum.ByService["instance"])
}
