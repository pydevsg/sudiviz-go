package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

func ptr32(v int32) *int32 { return &v }

func albGraph(healthy, total, port int, allowFromALB bool) *graph.InfraGraph {
	g := graph.New()
	g.AddResource(graph.Resource{
		ID: "arn:alb", Kind: graph.KindALB, Label: "public", Health: graph.HealthHealthy,
		Attrs: map[string]any{"security_group_ids": []string{"sg-alb"}},
	})
	g.AddResource(graph.Resource{
		ID: "arn:tg", Kind: graph.KindTargetGroup, Label: "web",
		Attrs: map[string]any{"port": port, "healthy_count": healthy, "total_count": total},
	})
	g.AddResource(graph.Resource{ID: "i-1", Kind: graph.KindInstance, Label: "web-1", Health: graph.HealthHealthy})
	rules := []graph.SGRule{{
		Direction: "ingress", Protocol: "tcp", FromPort: ptr32(22), ToPort: ptr32(22),
		CIDRRanges: []string{"10.0.0.0/8"},
	}}
	if allowFromALB {
		rules = append(rules, graph.SGRule{
			Direction: "ingress", Protocol: "tcp", FromPort: ptr32(int32(port)), ToPort: ptr32(int32(port)),
			ReferencedSGIDs: []string{"sg-alb"},
		})
	}
	g.AddResource(graph.Resource{
		ID: "sg-web", Kind: graph.KindSecurityGroup, Label: "web-sg",
		Attrs: map[string]any{"rules": rules},
	})
	g.AddResource(graph.Resource{ID: "sg-alb", Kind: graph.KindSecurityGroup, Label: "alb-sg"})
	g.AddEdge(graph.Edge{From: "arn:alb", To: "arn:tg", Relation: graph.RelationForwardsTo})
	g.AddEdge(graph.Edge{From: "i-1", To: "arn:tg", Relation: graph.RelationRegisteredIn})
	g.AddEdge(graph.Edge{From: "i-1", To: "sg-web", Relation: graph.RelationGuardedBy})
	g.AddEdge(graph.Edge{From: "arn:alb", To: "sg-alb", Relation: graph.RelationGuardedBy})
	return g
}

func TestUnhealthyTargets(t *testing.T) {
	g := albGraph(0, 2, 80, true)
	findings := UnhealthyTargets{}.Evaluate(g)
	require.Len(t, findings, 1)
	assert.Equal(t, diagnose.SeverityCritical, findings[0].Severity)
	assert.Contains(t, findings[0].Title, "0/2 healthy")

	g2 := albGraph(1, 2, 80, true)
	findings = UnhealthyTargets{}.Evaluate(g2)
	require.Len(t, findings, 1)
	assert.Equal(t, diagnose.SeverityWarning, findings[0].Severity)

	g3 := albGraph(2, 2, 80, true)
	assert.Empty(t, UnhealthyTargets{}.Evaluate(g3))

	g4 := albGraph(0, 0, 80, true)
	assert.Empty(t, UnhealthyTargets{}.Evaluate(g4)) // empty TGs are orphans
}

func TestMissingSGIngress(t *testing.T) {
	g := albGraph(2, 2, 80, false)
	findings := MissingSGIngress{}.Evaluate(g)
	require.Len(t, findings, 1)
	assert.Equal(t, diagnose.SeverityCritical, findings[0].Severity)
	assert.Contains(t, findings[0].Title, "port 80")

	g2 := albGraph(2, 2, 80, true)
	assert.Empty(t, MissingSGIngress{}.Evaluate(g2))
}

func TestMissingSGIngressOpenCIDRAllows(t *testing.T) {
	g := albGraph(2, 2, 80, false)
	sg := g.Resource("sg-web")
	sg.Attrs["rules"] = []graph.SGRule{{
		Direction: "ingress", Protocol: "tcp", FromPort: ptr32(80), ToPort: ptr32(80),
		CIDRRanges: []string{"0.0.0.0/0"},
	}}
	assert.Empty(t, MissingSGIngress{}.Evaluate(g))
}

func TestS3Public(t *testing.T) {
	g := graph.New()
	g.AddResource(graph.Resource{
		ID: "arn:aws:s3:::open", Kind: graph.KindS3, Label: "open",
		Attrs: map[string]any{"public_access_blocked": false},
	})
	g.AddResource(graph.Resource{
		ID: "arn:aws:s3:::closed", Kind: graph.KindS3, Label: "closed",
		Attrs: map[string]any{"public_access_blocked": true},
	})
	g.AddResource(graph.Resource{ID: "arn:aws:s3:::unknown", Kind: graph.KindS3, Label: "unknown"})
	findings := S3Public{}.Evaluate(g)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Title, "open")
	assert.Equal(t, diagnose.SeverityCritical, findings[0].Severity)
}

func TestRDSPublicAndUnencrypted(t *testing.T) {
	g := graph.New()
	g.AddResource(graph.Resource{
		ID: "db-1", Kind: graph.KindRDS, Label: "prod",
		Attrs: map[string]any{"publicly_accessible": true, "storage_encrypted": false, "status": "available"},
	})
	pub := RDSPublic{}.Evaluate(g)
	require.Len(t, pub, 1)
	assert.Equal(t, diagnose.SeverityWarning, pub[0].Severity)

	enc := UnencryptedStorage{}.Evaluate(g)
	require.GreaterOrEqual(t, len(enc), 1)
	assert.Contains(t, enc[0].Title, "storage not encrypted")
}

func TestUnencryptedS3AndEBS(t *testing.T) {
	g := graph.New()
	g.AddResource(graph.Resource{
		ID: "arn:aws:s3:::plain", Kind: graph.KindS3, Label: "plain",
		Attrs: map[string]any{"encryption_enabled": false, "public_access_blocked": true},
	})
	g.AddResource(graph.Resource{
		ID: "i-enc", Kind: graph.KindInstance, Label: "enc",
		Attrs: map[string]any{"ebs_checked": true, "ebs_encrypted": false},
	})
	findings := UnencryptedStorage{}.Evaluate(g)
	assert.Len(t, findings, 2)
}

func TestOrphanTargetGroupAndInstance(t *testing.T) {
	g := graph.New()
	g.AddResource(graph.Resource{ID: "tg-orphan", Kind: graph.KindTargetGroup, Label: "forgotten"})
	g.AddResource(graph.Resource{ID: "i-orphan", Kind: graph.KindInstance, Label: "lonely"})
	tg := OrphanTargetGroup{}.Evaluate(g)
	require.Len(t, tg, 1)
	assert.Equal(t, diagnose.SeverityWarning, tg[0].Severity)
	inst := OrphanInstance{}.Evaluate(g)
	require.Len(t, inst, 1)
	assert.Equal(t, diagnose.SeverityInfo, inst[0].Severity)
}

func TestUnusedSecurityGroup(t *testing.T) {
	g := graph.New()
	g.AddResource(graph.Resource{ID: "sg-unused", Kind: graph.KindSecurityGroup, Label: "dusty"})
	g.AddResource(graph.Resource{ID: "sg-used", Kind: graph.KindSecurityGroup, Label: "live"})
	g.AddResource(graph.Resource{ID: "i-1", Kind: graph.KindInstance, Label: "web"})
	g.AddEdge(graph.Edge{From: "i-1", To: "sg-used", Relation: graph.RelationGuardedBy})
	findings := UnusedSecurityGroup{}.Evaluate(g)
	require.Len(t, findings, 1)
	assert.Equal(t, "sg-unused", findings[0].ResourceID)
}

func TestUnhealthyServices(t *testing.T) {
	g := graph.New()
	g.AddResource(graph.Resource{
		ID: "svc", Kind: graph.KindECSService, Label: "api",
		Attrs: map[string]any{"running_count": 0, "desired_count": 2},
	})
	g.AddResource(graph.Resource{ID: "eks", Kind: graph.KindEKSCluster, Label: "prod", Health: graph.HealthUnhealthy})
	g.AddResource(graph.Resource{ID: "db", Kind: graph.KindRDS, Label: "main", Attrs: map[string]any{"status": "failed"}})
	g.AddResource(graph.Resource{ID: "fn", Kind: graph.KindLambda, Label: "worker", Health: graph.HealthUnhealthy})

	assert.Equal(t, diagnose.SeverityCritical, UnhealthyECSServices{}.Evaluate(g)[0].Severity)
	assert.Equal(t, diagnose.SeverityCritical, UnhealthyEKSClusters{}.Evaluate(g)[0].Severity)
	assert.Equal(t, diagnose.SeverityCritical, UnhealthyRDSInstances{}.Evaluate(g)[0].Severity)
	assert.Equal(t, diagnose.SeverityWarning, UnhealthyLambdas{}.Evaluate(g)[0].Severity)
}

func TestEngineSortsBySeverity(t *testing.T) {
	g := graph.New()
	g.AddResource(graph.Resource{ID: "sg-x", Kind: graph.KindSecurityGroup, Label: "x"})
	g.AddResource(graph.Resource{
		ID: "arn:aws:s3:::open", Kind: graph.KindS3, Label: "open",
		Attrs: map[string]any{"public_access_blocked": false},
	})
	diag := NewEngine().Run(g)
	require.NotEmpty(t, diag.Fixes)
	assert.Equal(t, diagnose.SeverityCritical, diag.Fixes[0].Severity)
}
