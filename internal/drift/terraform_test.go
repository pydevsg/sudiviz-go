package drift

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

func testdata(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "terraform_state.json")
}

func TestParseAndDetect(t *testing.T) {
	state, err := LoadState(testdata(t))
	require.NoError(t, err)
	intended := ParseIntended(state)
	require.Len(t, intended, 5)

	g := graph.New()
	g.AddResource(graph.Resource{
		ID:   "arn:aws:elasticloadbalancing:us-east-1:111111111111:loadbalancer/app/public/abc",
		Kind: graph.KindALB, Label: "public",
	})
	g.AddResource(graph.Resource{
		ID:   "arn:aws:elasticloadbalancing:us-east-1:111111111111:targetgroup/web/123",
		Kind: graph.KindTargetGroup, Label: "web",
	})
	g.AddResource(graph.Resource{ID: "sg-alb", Kind: graph.KindSecurityGroup, Label: "alb"})
	g.AddResource(graph.Resource{ID: "i-orphan", Kind: graph.KindInstance, Label: "manual"})
	g.AddEdge(graph.Edge{
		From:     "arn:aws:elasticloadbalancing:us-east-1:111111111111:loadbalancer/app/public/abc",
		To:       "arn:aws:elasticloadbalancing:us-east-1:111111111111:targetgroup/web/123",
		Relation: graph.RelationForwardsTo,
	})

	findings := Detect(intended, g)
	kinds := map[string]int{}
	for _, f := range findings {
		kinds[f.Kind]++
	}
	assert.Equal(t, 1, kinds["missing"]) // aws_instance.web not in live
	assert.GreaterOrEqual(t, kinds["orphan_in_aws"], 1)
}
