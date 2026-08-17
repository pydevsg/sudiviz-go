package remediators

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/fix"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

type fakeEC2 struct {
	ingress int
	deleted int
}

func (f *fakeEC2) AuthorizeSecurityGroupIngress(ctx context.Context, in *ec2.AuthorizeSecurityGroupIngressInput, _ ...func(*ec2.Options)) (*ec2.AuthorizeSecurityGroupIngressOutput, error) {
	f.ingress++
	return &ec2.AuthorizeSecurityGroupIngressOutput{}, nil
}
func (f *fakeEC2) DeleteSecurityGroup(ctx context.Context, in *ec2.DeleteSecurityGroupInput, _ ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
	f.deleted++
	return &ec2.DeleteSecurityGroupOutput{}, nil
}

type fakeELB struct {
	deleted, deregistered int
}

func (f *fakeELB) DeleteTargetGroup(ctx context.Context, in *elbv2.DeleteTargetGroupInput, _ ...func(*elbv2.Options)) (*elbv2.DeleteTargetGroupOutput, error) {
	f.deleted++
	return &elbv2.DeleteTargetGroupOutput{}, nil
}
func (f *fakeELB) DeregisterTargets(ctx context.Context, in *elbv2.DeregisterTargetsInput, _ ...func(*elbv2.Options)) (*elbv2.DeregisterTargetsOutput, error) {
	f.deregistered++
	return &elbv2.DeregisterTargetsOutput{}, nil
}

type fakeS3 struct{ pab, enc int }

func (f *fakeS3) PutPublicAccessBlock(ctx context.Context, in *s3.PutPublicAccessBlockInput, _ ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error) {
	f.pab++
	return &s3.PutPublicAccessBlockOutput{}, nil
}
func (f *fakeS3) PutBucketEncryption(ctx context.Context, in *s3.PutBucketEncryptionInput, _ ...func(*s3.Options)) (*s3.PutBucketEncryptionOutput, error) {
	f.enc++
	return &s3.PutBucketEncryptionOutput{}, nil
}

type fakeRDS struct{ mods int }

func (f *fakeRDS) ModifyDBInstance(ctx context.Context, in *rds.ModifyDBInstanceInput, _ ...func(*rds.Options)) (*rds.ModifyDBInstanceOutput, error) {
	f.mods++
	return &rds.ModifyDBInstanceOutput{}, nil
}

func clients(ec2c *fakeEC2, elb *fakeELB, s3c *fakeS3, r *fakeRDS) *fix.AWSClients {
	return &fix.AWSClients{EC2: ec2c, ELBV2: elb, S3: s3c, RDS: r}
}

func TestSGIngressPlanAndApply(t *testing.T) {
	g := graph.New()
	g.AddResource(graph.Resource{ID: "i-1", Kind: graph.KindInstance, Label: "web"})
	g.AddResource(graph.Resource{ID: "sg-web", Kind: graph.KindSecurityGroup, Label: "web"})
	g.AddResource(graph.Resource{
		ID: "alb", Kind: graph.KindALB, Label: "alb",
		Attrs: map[string]any{"security_group_ids": []string{"sg-alb"}},
	})
	g.AddResource(graph.Resource{ID: "tg", Kind: graph.KindTargetGroup, Label: "tg"})
	g.AddEdge(graph.Edge{From: "i-1", To: "sg-web", Relation: graph.RelationGuardedBy})
	g.AddEdge(graph.Edge{From: "i-1", To: "tg", Relation: graph.RelationRegisteredIn})
	g.AddEdge(graph.Edge{From: "alb", To: "tg", Relation: graph.RelationForwardsTo})

	f := diagnose.Finding{Title: "Security group missing port 80 from ALB SG", ResourceID: "i-1"}
	assert.True(t, SGIngress{}.Match(f))
	action := SGIngress{}.Plan(f, g, "us-east-1")
	require.NotNil(t, action)
	assert.False(t, action.IsDestructive)
	assert.Contains(t, action.AWSCLICommand, "authorize-security-group-ingress")
	ec2c := &fakeEC2{}
	fix.Apply(context.Background(), action, clients(ec2c, &fakeELB{}, &fakeS3{}, &fakeRDS{}))
	assert.True(t, action.Applied)
	assert.Equal(t, 1, ec2c.ingress)
}

func TestOrphanTGAndUnusedSGAreDestructive(t *testing.T) {
	g := graph.New()
	tg := diagnose.Finding{Title: "Orphan target group: web", ResourceID: "arn:tg"}
	assert.True(t, OrphanTargetGroup{}.Match(tg))
	a := OrphanTargetGroup{}.Plan(tg, g, "us-east-1")
	assert.True(t, a.IsDestructive)
	elb := &fakeELB{}
	fix.Apply(context.Background(), a, clients(&fakeEC2{}, elb, &fakeS3{}, &fakeRDS{}))
	assert.Equal(t, 1, elb.deleted)

	sg := diagnose.Finding{Title: "Unused security group: dusty", ResourceID: "sg-x"}
	assert.True(t, UnusedSecurityGroup{}.Match(sg))
	b := UnusedSecurityGroup{}.Plan(sg, g, "us-east-1")
	assert.True(t, b.IsDestructive)
	ec2c := &fakeEC2{}
	fix.Apply(context.Background(), b, clients(ec2c, &fakeELB{}, &fakeS3{}, &fakeRDS{}))
	assert.Equal(t, 1, ec2c.deleted)
}

func TestS3AndRDSRemediators(t *testing.T) {
	g := graph.New()
	s3c := &fakeS3{}
	r := &fakeRDS{}
	c := clients(&fakeEC2{}, &fakeELB{}, s3c, r)

	pub := diagnose.Finding{Title: "S3 bucket 'x': public access not fully blocked", ResourceID: "arn:aws:s3:::x"}
	assert.True(t, S3PublicAccess{}.Match(pub))
	a := S3PublicAccess{}.Plan(pub, g, "us-east-1")
	fix.Apply(context.Background(), a, c)
	assert.Equal(t, 1, s3c.pab)

	enc := diagnose.Finding{Title: "S3 bucket 'x': server-side encryption not enabled", ResourceID: "arn:aws:s3:::x"}
	assert.True(t, S3Encryption{}.Match(enc))
	b := S3Encryption{}.Plan(enc, g, "us-east-1")
	fix.Apply(context.Background(), b, c)
	assert.Equal(t, 1, s3c.enc)

	rdsF := diagnose.Finding{Title: "RDS 'prod': publicly accessible", ResourceID: "arn:aws:rds:us-east-1:1:db:prod"}
	assert.True(t, RDSPublicAccess{}.Match(rdsF))
	d := RDSPublicAccess{}.Plan(rdsF, g, "us-east-1")
	fix.Apply(context.Background(), d, c)
	assert.Equal(t, 1, r.mods)
}

func TestUnhealthyTargetsRemediator(t *testing.T) {
	g := graph.New()
	g.AddResource(graph.Resource{
		ID: "arn:tg", Kind: graph.KindTargetGroup, Label: "web",
		Attrs: map[string]any{"targets": []map[string]any{
			{"target_id": "i-1", "health": "unhealthy"},
			{"target_id": "i-2", "health": "healthy"},
		}},
	})
	f := diagnose.Finding{Title: "Target group 'web': 1/2 healthy", ResourceID: "arn:tg"}
	assert.True(t, UnhealthyTargets{}.Match(f))
	a := UnhealthyTargets{}.Plan(f, g, "us-east-1")
	assert.True(t, a.IsDestructive)
	elb := &fakeELB{}
	fix.Apply(context.Background(), a, clients(&fakeEC2{}, elb, &fakeS3{}, &fakeRDS{}))
	assert.Equal(t, 1, elb.deregistered)
}

func TestGenerateManualFallback(t *testing.T) {
	diag := &diagnose.Diagnosis{Fixes: []diagnose.Finding{{Title: "something unknown", Detail: "nope"}}}
	actions := fix.Generate(diag, graph.New(), "us-east-1", All())
	require.Len(t, actions, 1)
	assert.False(t, actions[0].HasAutomatedFix())
	assert.Contains(t, actions[0].AWSCLICommand, "No automated fix")
}
