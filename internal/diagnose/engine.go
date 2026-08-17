package diagnose

import (
	"sort"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// Engine runs a set of rules over a graph and aggregates the results.
type Engine struct {
	rules []Rule
}

// NewEngine builds an engine over the given rule set.
func NewEngine(rules ...Rule) *Engine { return &Engine{rules: rules} }

// Rules returns the configured rule set.
func (e *Engine) Rules() []Rule { return e.rules }

// Run evaluates every rule and returns the aggregated diagnosis. Findings are
// sorted by severity (critical > warning > info), then title, for stable
// output.
func (e *Engine) Run(g *graph.InfraGraph) *Diagnosis {
	diag := &Diagnosis{
		OrphanTargetGroups:    []map[string]any{},
		OrphanInstances:       []map[string]any{},
		OrphanSecurityGroups:  []map[string]any{},
		UnhealthyTargetGroups: []map[string]any{},
		UnhealthyECSServices:  []map[string]any{},
		UnhealthyEKSClusters:  []map[string]any{},
		UnhealthyRDSInstances: []map[string]any{},
		InsecureS3Buckets:     []map[string]any{},
		Fixes:                 []Finding{},
	}

	for _, rule := range e.rules {
		for _, f := range rule.Evaluate(g) {
			diag.Fixes = append(diag.Fixes, f)
			if f.CategoryPayload == nil {
				continue
			}
			switch f.Category {
			case CategoryOrphanTargetGroup:
				diag.OrphanTargetGroups = append(diag.OrphanTargetGroups, f.CategoryPayload)
			case CategoryOrphanInstance:
				diag.OrphanInstances = append(diag.OrphanInstances, f.CategoryPayload)
			case CategoryOrphanSecurityGroup:
				diag.OrphanSecurityGroups = append(diag.OrphanSecurityGroups, f.CategoryPayload)
			case CategoryUnhealthyTargetGroup:
				diag.UnhealthyTargetGroups = append(diag.UnhealthyTargetGroups, f.CategoryPayload)
			case CategoryUnhealthyECSService:
				diag.UnhealthyECSServices = append(diag.UnhealthyECSServices, f.CategoryPayload)
			case CategoryUnhealthyEKSCluster:
				diag.UnhealthyEKSClusters = append(diag.UnhealthyEKSClusters, f.CategoryPayload)
			case CategoryUnhealthyRDSInstance:
				diag.UnhealthyRDSInstances = append(diag.UnhealthyRDSInstances, f.CategoryPayload)
			case CategoryInsecureS3Bucket:
				diag.InsecureS3Buckets = append(diag.InsecureS3Buckets, f.CategoryPayload)
			}
		}
	}

	sort.SliceStable(diag.Fixes, func(i, j int) bool {
		a, b := diag.Fixes[i], diag.Fixes[j]
		if a.Severity.Rank() != b.Severity.Rank() {
			return a.Severity.Rank() < b.Severity.Rank()
		}
		return a.Title < b.Title
	})
	return diag
}
