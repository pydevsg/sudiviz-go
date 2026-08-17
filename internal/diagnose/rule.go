package diagnose

import "github.com/pydevsg/sudiviz-go/internal/graph"

// Rule is one pluggable diagnostic check. Rules are stateless and evaluate
// against the assembled topology graph only — they never call cloud APIs, so
// they are trivially unit-testable and cloud-agnostic.
type Rule interface {
	// Name is a short stable identifier for the rule.
	Name() string
	// Severity is the highest severity this rule can emit.
	Severity() Severity
	// Evaluate inspects the graph and returns zero or more findings.
	Evaluate(g *graph.InfraGraph) []Finding
}
