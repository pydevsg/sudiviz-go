// Package fix translates diagnostic findings into executable remediation
// actions. By default actions are only described (dry-run); applying requires
// an explicit opt-in, and destructive actions additionally require --force.
package fix

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// Action is one remediation that can be previewed or applied.
type Action struct {
	Finding       diagnose.Finding `json:"-"`
	Title         string           `json:"title"`
	Severity      string           `json:"severity"`
	Description   string           `json:"description"`
	AWSCLICommand string           `json:"aws_cli_command"`
	IsDestructive bool             `json:"is_destructive"`
	Applied       bool             `json:"applied"`
	Error         string           `json:"error,omitempty"`

	apply func(ctx context.Context, c *AWSClients) error
}

// HasAutomatedFix reports whether the action can be applied programmatically.
func (a *Action) HasAutomatedFix() bool { return a.apply != nil }

// NewAction builds a remediation action. applyFn may be nil for manual-only
// fixes.
func NewAction(f diagnose.Finding, cliCommand, description string, destructive bool, applyFn func(ctx context.Context, c *AWSClients) error) *Action {
	return &Action{
		Finding:       f,
		Title:         f.Title,
		Severity:      string(f.Severity),
		AWSCLICommand: cliCommand,
		Description:   description,
		IsDestructive: destructive,
		apply:         applyFn,
	}
}

// Remediator plans a fix for findings it recognizes. Implementations are
// pure planners: the returned Action captures everything needed to apply.
type Remediator interface {
	// Name is a short stable identifier.
	Name() string
	// Match reports whether this remediator handles the finding.
	Match(f diagnose.Finding) bool
	// Plan builds the remediation action (command + apply closure).
	Plan(f diagnose.Finding, g *graph.InfraGraph, region string) *Action
}

// Generate builds remediation actions for every finding using the given
// remediators (first match wins). Findings nobody recognizes get a manual
// "no automated fix" action so the numbering matches the diagnosis.
func Generate(diag *diagnose.Diagnosis, g *graph.InfraGraph, region string, remediators []Remediator) []*Action {
	var actions []*Action
	for _, f := range diag.Fixes {
		var action *Action
		for _, r := range remediators {
			if r.Match(f) {
				action = r.Plan(f, g, region)
				break
			}
		}
		if action == nil {
			action = &Action{
				Finding:       f,
				AWSCLICommand: fmt.Sprintf("# No automated fix available for: %s", f.Title),
				Description:   fmt.Sprintf("Manual intervention required: %s", f.Detail),
			}
		}
		action.Title = f.Title
		action.Severity = string(f.Severity)
		actions = append(actions, action)
	}
	return actions
}

// Apply executes the action against AWS, updating Applied/Error in place.
func Apply(ctx context.Context, a *Action, clients *AWSClients) {
	if a.apply == nil {
		a.Error = "No automated fix available"
		return
	}
	if err := a.apply(ctx, clients); err != nil {
		a.Error = err.Error()
		return
	}
	a.Applied = true
}

// --- AWS client bundle -----------------------------------------------------

// EC2FixAPI is the EC2 capability needed by remediators.
type EC2FixAPI interface {
	AuthorizeSecurityGroupIngress(ctx context.Context, in *ec2.AuthorizeSecurityGroupIngressInput, opts ...func(*ec2.Options)) (*ec2.AuthorizeSecurityGroupIngressOutput, error)
	DeleteSecurityGroup(ctx context.Context, in *ec2.DeleteSecurityGroupInput, opts ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error)
}

// ELBV2FixAPI is the ELBv2 capability needed by remediators.
type ELBV2FixAPI interface {
	DeleteTargetGroup(ctx context.Context, in *elbv2.DeleteTargetGroupInput, opts ...func(*elbv2.Options)) (*elbv2.DeleteTargetGroupOutput, error)
	DeregisterTargets(ctx context.Context, in *elbv2.DeregisterTargetsInput, opts ...func(*elbv2.Options)) (*elbv2.DeregisterTargetsOutput, error)
}

// S3FixAPI is the S3 capability needed by remediators.
type S3FixAPI interface {
	PutPublicAccessBlock(ctx context.Context, in *s3.PutPublicAccessBlockInput, opts ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error)
	PutBucketEncryption(ctx context.Context, in *s3.PutBucketEncryptionInput, opts ...func(*s3.Options)) (*s3.PutBucketEncryptionOutput, error)
}

// RDSFixAPI is the RDS capability needed by remediators.
type RDSFixAPI interface {
	ModifyDBInstance(ctx context.Context, in *rds.ModifyDBInstanceInput, opts ...func(*rds.Options)) (*rds.ModifyDBInstanceOutput, error)
}

// AWSClients bundles the write-capable clients used to apply fixes.
type AWSClients struct {
	EC2   EC2FixAPI
	ELBV2 ELBV2FixAPI
	S3    S3FixAPI
	RDS   RDSFixAPI
}

// NewAWSClients builds real AWS clients from a config.
func NewAWSClients(cfg aws.Config) *AWSClients {
	return &AWSClients{
		EC2:   ec2.NewFromConfig(cfg),
		ELBV2: elbv2.NewFromConfig(cfg),
		S3:    s3.NewFromConfig(cfg),
		RDS:   rds.NewFromConfig(cfg),
	}
}
