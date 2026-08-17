package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddEdgeCreatesPlaceholders(t *testing.T) {
	g := New()
	g.AddEdge(Edge{From: "alb-1", To: "tg-1", Relation: RelationForwardsTo})
	require.True(t, g.HasNode("alb-1"))
	require.True(t, g.HasNode("tg-1"))
	assert.Equal(t, KindUnknown, g.Resource("alb-1").Kind)
	assert.Equal(t, 1, g.EdgeCount())
}

func TestAddResourceMergesPlaceholder(t *testing.T) {
	g := New()
	g.AddEdge(Edge{From: "i-1", To: "tg-1", Relation: RelationRegisteredIn})
	g.Resource("i-1").Health = HealthUnhealthy

	g.AddResource(Resource{ID: "i-1", Kind: KindInstance, Label: "web", Health: HealthUnknown, MonthlyCost: 12})
	n := g.Resource("i-1")
	require.NotNil(t, n)
	assert.Equal(t, KindInstance, n.Kind)
	assert.Equal(t, "web", n.Label)
	assert.Equal(t, HealthUnhealthy, n.Health) // preserved from placeholder
	assert.Equal(t, 12.0, n.MonthlyCost)
}

func TestAddEdgeReplacesPayload(t *testing.T) {
	g := New()
	g.AddEdge(Edge{From: "a", To: "b", Relation: RelationForwardsTo, Style: "solid"})
	g.AddEdge(Edge{From: "a", To: "b", Relation: RelationForwardsTo, Style: "dashed", Orphan: true})
	assert.Equal(t, 1, g.EdgeCount())
	e := g.OutEdges("a")[0]
	assert.Equal(t, "dashed", e.Style)
	assert.True(t, e.Orphan)
}

func TestResourcesByKind(t *testing.T) {
	g := New()
	g.AddResource(Resource{ID: "a", Kind: KindALB, Label: "alb"})
	g.AddResource(Resource{ID: "b", Kind: KindInstance, Label: "i"})
	g.AddResource(Resource{ID: "c", Kind: KindALB, Label: "alb2"})
	assert.Len(t, g.ResourcesByKind(KindALB), 2)
	assert.Len(t, g.ResourcesByKind(KindInstance), 1)
}

func TestSelfLoopSkipped(t *testing.T) {
	g := New()
	g.AddEdge(Edge{From: "sg-1", To: "sg-1", Relation: RelationAllowsIngress})
	assert.Equal(t, 0, g.EdgeCount())
}

func TestAttrHelpers(t *testing.T) {
	r := Resource{Attrs: map[string]any{
		"port": float64(80), "ok": true, "ids": []string{"a", "b"},
	}}
	assert.Equal(t, 80, r.AttrInt("port"))
	assert.True(t, r.AttrBool("ok"))
	assert.Equal(t, []string{"a", "b"}, r.AttrStrings("ids"))
	assert.True(t, r.AttrBoolDefault("missing", true))
	assert.True(t, r.AttrBoolDefault("ok", false))
}
