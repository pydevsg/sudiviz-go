package static

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

func TestBuildDOT(t *testing.T) {
	g := graph.New()
	g.AddResource(graph.Resource{ID: "alb", Kind: graph.KindALB, Label: "public", Health: graph.HealthHealthy})
	g.AddResource(graph.Resource{ID: "tg", Kind: graph.KindTargetGroup, Label: "web", Orphan: true, Attrs: map[string]any{"healthy_count": 0, "total_count": 0}})
	g.AddEdge(graph.Edge{From: "alb", To: "tg", Relation: graph.RelationForwardsTo, Orphan: true})
	dot := BuildDOT(g)
	assert.Contains(t, dot, "digraph sudiviz")
	assert.Contains(t, dot, "public")
	assert.Contains(t, dot, "dashed")
	assert.True(t, strings.Contains(dot, "forwards_to"))
}
