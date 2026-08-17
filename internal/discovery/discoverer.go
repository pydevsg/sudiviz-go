// Package discovery finds live cloud resources and their relationships.
//
// Each service has its own Discoverer implementation; DiscoverAll fans them
// out concurrently with errgroup and assembles the combined topology graph.
// The Discoverer interface is cloud-agnostic — AWS is the only provider at
// v1.0, but GCP/Azure discoverers can implement the same interface because
// all provider-specific state is injected at construction time.
package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// Discoverer discovers resources of one service. Implementations hold their
// own (provider-specific) clients; Discover must be safe to call concurrently
// with other discoverers.
type Discoverer interface {
	// ServiceName is a short stable identifier ("alb", "ec2", ...).
	ServiceName() string
	// Discover returns the resources and relationship edges for the service.
	Discover(ctx context.Context) (*Result, error)
}

// Result is the output of one discoverer.
type Result struct {
	Resources []graph.Resource
	// Edges are always added to the graph; endpoints that were not
	// discovered become placeholder nodes (mirrors networkx semantics).
	Edges []graph.Edge
	// ConditionalEdges are only added when both endpoints already exist in
	// the assembled graph (e.g. lambda "invokes" edges from event sources
	// that may live outside the discovered topology).
	ConditionalEdges []graph.Edge
}

// Options scope a discovery run.
type Options struct {
	Profile    string
	Region     string
	VPCID      string
	ServiceTag string // "k=v" or "k1=v1,k2=v2"
}

// TagFilter returns the parsed service tag filter.
func (o Options) TagFilter() map[string]string { return ParseServiceTag(o.ServiceTag) }

// ParseServiceTag parses "k=v" / "k1=v1,k2=v2" tag filters.
func ParseServiceTag(s string) map[string]string {
	out := map[string]string{}
	for _, piece := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(piece, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

// matchesTags reports whether all filter pairs are present in tags.
func matchesTags(tags map[string]string, filter map[string]string) bool {
	for k, v := range filter {
		if tags[k] != v {
			return false
		}
	}
	return true
}

// Meta carries account-level metadata attached to the assembled graph.
type Meta struct {
	AccountID string
	Region    string
	VPCID     string
}

// assemblyOrder fixes the order in which per-service results are merged into
// the graph so that node-merge semantics are deterministic (target-group
// placeholders before full EC2 data, etc.).
var assemblyOrder = []string{"alb", "target_group", "ec2", "security_group", "ecs", "eks", "rds", "lambda", "s3"}

// DiscoverAll fans out all discoverers concurrently and assembles the
// topology graph. Per-service failures do not abort the run; they are
// returned as warnings (mirrors the Python behaviour of substituting an
// empty result for a failed service).
func DiscoverAll(ctx context.Context, discoverers []Discoverer, meta Meta) (*graph.InfraGraph, []string) {
	var (
		mu      sync.Mutex
		results = map[string]*Result{}
		errs    []string
	)

	g, ctx := errgroup.WithContext(ctx)
	for _, discoverer := range discoverers {
		d := discoverer
		g.Go(func() error {
			res, err := d.Discover(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Sprintf("discovering %s: %v", d.ServiceName(), err))
				return nil
			}
			results[d.ServiceName()] = res
			return nil
		})
	}
	_ = g.Wait()

	return assemble(meta, results), errs
}

// assemble merges per-service results into one InfraGraph in canonical order.
func assemble(meta Meta, results map[string]*Result) *graph.InfraGraph {
	ig := graph.New()
	ig.AccountID = meta.AccountID
	ig.Region = meta.Region
	ig.VPCID = meta.VPCID

	// VPC node only exists when discovery is scoped to one VPC; in_vpc edges
	// are conditional so they only materialize in that case.
	if meta.VPCID != "" {
		ig.AddResource(graph.Resource{
			ID:       meta.VPCID,
			Kind:     graph.KindVPC,
			Label:    "VPC " + meta.VPCID,
			Provider: "aws",
			Health:   graph.HealthHealthy,
		})
	}

	// Phase 1: nodes, in canonical service order.
	for _, svc := range assemblyOrder {
		res := results[svc]
		if res == nil {
			continue
		}
		for _, r := range res.Resources {
			ig.AddResource(r)
		}
	}

	// Phase 2: edges. Unconditional edges may create placeholders;
	// conditional edges require both endpoints to exist.
	for _, svc := range assemblyOrder {
		res := results[svc]
		if res == nil {
			continue
		}
		for _, e := range res.Edges {
			ig.AddEdge(e)
		}
		for _, e := range res.ConditionalEdges {
			if ig.HasNode(e.From) && ig.HasNode(e.To) {
				ig.AddEdge(e)
			}
		}
	}

	// Phase 3: wire S3 buckets into the topology via naming heuristics so
	// they don't float as islands (same heuristic as the Python builder).
	wireS3Buckets(ig)

	return ig
}

func wireS3Buckets(ig *graph.InfraGraph) {
	var instanceIDs, rdsIDs, albIDs []string
	for _, n := range ig.Resources() {
		switch n.Kind {
		case graph.KindInstance:
			instanceIDs = append(instanceIDs, n.ID)
		case graph.KindRDS:
			rdsIDs = append(rdsIDs, n.ID)
		case graph.KindALB:
			albIDs = append(albIDs, n.ID)
		}
	}

	for _, bucket := range ig.ResourcesByKind(graph.KindS3) {
		name := strings.ToLower(bucket.Label)
		switch {
		case containsAny(name, "log", "logs", "access-log"):
			for _, alb := range albIDs {
				ig.AddEdge(graph.Edge{From: alb, To: bucket.ID, Relation: graph.RelationLogsTo, Style: "dashed"})
			}
		case containsAny(name, "asset", "assets", "upload", "uploads", "media", "static"):
			for _, inst := range instanceIDs {
				ig.AddEdge(graph.Edge{From: inst, To: bucket.ID, Relation: graph.RelationReadsFrom, Style: "dashed"})
			}
		default:
			for _, inst := range instanceIDs {
				ig.AddEdge(graph.Edge{From: inst, To: bucket.ID, Relation: graph.RelationAccesses, Style: "dashed"})
			}
			for _, rds := range rdsIDs {
				ig.AddEdge(graph.Edge{From: rds, To: bucket.ID, Relation: graph.RelationBacksUpTo, Style: "dashed"})
			}
		}
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
