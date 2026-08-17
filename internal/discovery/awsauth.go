package discovery

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// LoadAWSConfig builds an aws.Config using the standard credential chain
// (env vars, shared config/credentials files, SSO, IMDS). Credentials are
// never accepted as CLI flags. Adaptive retries with jittered backoff are
// enabled so parallel discovery of large accounts doesn't trip throttling.
func LoadAWSConfig(ctx context.Context, profile, region string) (aws.Config, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRetryer(func() aws.Retryer {
			return retry.NewAdaptiveMode(func(o *retry.AdaptiveModeOptions) {
				o.StandardOptions = append(o.StandardOptions, func(so *retry.StandardOptions) {
					so.MaxAttempts = 8
				})
			})
		}),
		awsconfig.WithAppID("sudiviz"),
	}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("loading AWS config: %w", err)
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	return cfg, nil
}

// STSAPI is the subset of the STS client used to resolve caller identity.
type STSAPI interface {
	GetCallerIdentity(ctx context.Context, in *sts.GetCallerIdentityInput, opts ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// Identity is the resolved caller identity, shown in status bars.
type Identity struct {
	AccountID string
	ARN       string
	UserID    string
	Region    string
}

// Whoami resolves the caller identity. This is the first AWS call sudiviz
// makes, so failures produce an actionable error message.
func Whoami(ctx context.Context, api STSAPI, region string) (Identity, error) {
	out, err := api.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return Identity{}, fmt.Errorf(
			"failed to resolve AWS identity (configure credentials via env vars, ~/.aws/credentials, SSO, or an instance profile — sudiviz never accepts credentials as flags): %w", err)
	}
	return Identity{
		AccountID: aws.ToString(out.Account),
		ARN:       aws.ToString(out.Arn),
		UserID:    aws.ToString(out.UserId),
		Region:    region,
	}, nil
}

// NewAWSDiscoverers constructs the full v1.0 AWS discoverer set.
func NewAWSDiscoverers(cfg aws.Config, opts Options) []Discoverer {
	elbv2Client := elasticloadbalancingv2.NewFromConfig(cfg)
	ec2Client := ec2.NewFromConfig(cfg)
	return []Discoverer{
		NewALBDiscoverer(elbv2Client, opts),
		NewTargetGroupDiscoverer(elbv2Client, opts),
		NewEC2Discoverer(ec2Client, opts),
		NewSecurityGroupDiscoverer(ec2Client, opts),
		NewECSDiscoverer(ecs.NewFromConfig(cfg), opts),
		NewEKSDiscoverer(eks.NewFromConfig(cfg), opts),
		NewRDSDiscoverer(rds.NewFromConfig(cfg), opts),
		NewLambdaDiscoverer(lambda.NewFromConfig(cfg), opts),
		NewS3Discoverer(s3.NewFromConfig(cfg), cfg.Region, opts),
	}
}

// RunResult is the output of a full discovery pass.
type RunResult struct {
	Graph    *graph.InfraGraph
	Warnings []string
	Config   aws.Config
}

// Run performs a full discovery pass: resolve identity, fan out all
// discoverers, and return the assembled topology graph. This is the single
// entry point used by the CLI, web server, TUI, and MCP server.
func Run(ctx context.Context, opts Options) (*RunResult, error) {
	cfg, err := LoadAWSConfig(ctx, opts.Profile, opts.Region)
	if err != nil {
		return nil, err
	}
	identity, err := Whoami(ctx, sts.NewFromConfig(cfg), cfg.Region)
	if err != nil {
		return nil, err
	}
	discoverers := NewAWSDiscoverers(cfg, opts)
	g, warnings := DiscoverAll(ctx, discoverers, Meta{
		AccountID: identity.AccountID,
		Region:    cfg.Region,
		VPCID:     opts.VPCID,
	})
	return &RunResult{Graph: g, Warnings: warnings, Config: cfg}, nil
}
