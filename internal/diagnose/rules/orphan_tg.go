package rules

import (
	"fmt"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// OrphanTargetGroup flags target groups no load balancer listener forwards
// to: their backends are unreachable even when healthy.
type OrphanTargetGroup struct{}

func (OrphanTargetGroup) Name() string                { return "orphan_tg" }
func (OrphanTargetGroup) Severity() diagnose.Severity { return diagnose.SeverityWarning }

func (OrphanTargetGroup) Evaluate(g *graph.InfraGraph) []diagnose.Finding {
	var out []diagnose.Finding
	for _, tg := range diagnose.FindOrphanTargetGroups(g) {
		out = append(out, diagnose.Finding{
			Severity:   diagnose.SeverityWarning,
			ResourceID: tg.ID,
			Title:      fmt.Sprintf("Orphan target group: %s", tg.Label),
			Detail: "No load balancer listener forwards to this target group. " +
				"Either delete it, or add a listener rule that routes traffic to it.",
			Category:        diagnose.CategoryOrphanTargetGroup,
			CategoryPayload: map[string]any{"id": tg.ID, "label": tg.Label, "kind": string(tg.Kind)},
		})
	}
	return out
}

// OrphanInstance flags instances that receive no traffic from any LB.
type OrphanInstance struct{}

func (OrphanInstance) Name() string                { return "orphan_instance" }
func (OrphanInstance) Severity() diagnose.Severity { return diagnose.SeverityInfo }

func (OrphanInstance) Evaluate(g *graph.InfraGraph) []diagnose.Finding {
	var out []diagnose.Finding
	for _, inst := range diagnose.FindOrphanInstances(g) {
		out = append(out, diagnose.Finding{
			Severity:   diagnose.SeverityInfo,
			ResourceID: inst.ID,
			Title:      fmt.Sprintf("Instance not in any target group: %s", inst.Label),
			Detail: "This instance receives no traffic from any ALB/NLB. " +
				"Register it with a target group, or remove it if unused.",
			Category:        diagnose.CategoryOrphanInstance,
			CategoryPayload: map[string]any{"id": inst.ID, "label": inst.Label, "kind": string(inst.Kind)},
		})
	}
	return out
}
