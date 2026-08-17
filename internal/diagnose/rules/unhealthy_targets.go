// Package rules contains the built-in diagnostic rule set.
package rules

import (
	"fmt"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// UnhealthyTargets flags target groups whose targets are failing health
// checks: critical when zero targets are healthy, warning when only some are.
type UnhealthyTargets struct{}

func (UnhealthyTargets) Name() string                { return "unhealthy_targets" }
func (UnhealthyTargets) Severity() diagnose.Severity { return diagnose.SeverityCritical }

func (UnhealthyTargets) Evaluate(g *graph.InfraGraph) []diagnose.Finding {
	var out []diagnose.Finding
	for _, tg := range g.ResourcesByKind(graph.KindTargetGroup) {
		healthy := tg.AttrInt("healthy_count")
		total := tg.AttrInt("total_count")
		if total == 0 || healthy >= total {
			continue // empty TGs are surfaced as orphans instead
		}
		severity := diagnose.SeverityWarning
		if healthy == 0 {
			severity = diagnose.SeverityCritical
		}
		unhealthy := total - healthy
		out = append(out, diagnose.Finding{
			Severity:   severity,
			ResourceID: tg.ID,
			Title:      fmt.Sprintf("Target group '%s': %d/%d healthy", tg.Label, healthy, total),
			Detail: fmt.Sprintf(
				"%d target(s) failing health checks. Check (1) the health check path returns 200, "+
					"(2) the security group on the target allows the configured port from the ALB SG, "+
					"(3) the target's process is actually listening on that port.", unhealthy),
			Category: diagnose.CategoryUnhealthyTargetGroup,
			CategoryPayload: map[string]any{
				"id": tg.ID, "label": tg.Label, "healthy": healthy, "total": total,
			},
		})
	}
	return out
}
