// Package run is the shared discover → annotate → diagnose pipeline used by
// every CLI command, the web server, the TUI, and the MCP server.
package run

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/diagnose/rules"
	"github.com/pydevsg/sudiviz-go/internal/discovery"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// Snapshot is one complete discovery + diagnosis pass.
type Snapshot struct {
	Graph     *graph.InfraGraph
	Diagnosis *diagnose.Diagnosis
	Config    aws.Config
	Warnings  []string
}

// Options aliases discovery.Options so callers don't have to import both.
type Options = discovery.Options

// Live discovers the account, marks orphans, and runs every diagnostic rule.
func Live(ctx context.Context, opts Options) (*Snapshot, error) {
	rr, err := discovery.Run(ctx, opts)
	if err != nil {
		return nil, err
	}
	g := diagnose.MarkOrphanedEdges(rr.Graph)
	return &Snapshot{
		Graph:     g,
		Diagnosis: rules.NewEngine().Run(g),
		Config:    rr.Config,
		Warnings:  rr.Warnings,
	}, nil
}

// ResourceCounts returns a kind → count map for MCP / JSON summaries.
func ResourceCounts(g *graph.InfraGraph) map[string]int {
	out := map[string]int{}
	if g == nil {
		return out
	}
	for _, r := range g.Resources() {
		out[string(r.Kind)]++
	}
	return out
}
