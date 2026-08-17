package rules

import (
	"fmt"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// UnusedSecurityGroup flags security groups attached to nothing.
type UnusedSecurityGroup struct{}

func (UnusedSecurityGroup) Name() string                { return "unused_sg" }
func (UnusedSecurityGroup) Severity() diagnose.Severity { return diagnose.SeverityInfo }

func (UnusedSecurityGroup) Evaluate(g *graph.InfraGraph) []diagnose.Finding {
	var out []diagnose.Finding
	for _, sg := range diagnose.FindUnusedSecurityGroups(g) {
		out = append(out, diagnose.Finding{
			Severity:   diagnose.SeverityInfo,
			ResourceID: sg.ID,
			Title:      fmt.Sprintf("Unused security group: %s", sg.Label),
			Detail:     "This security group is attached to nothing. Safe to delete.",
			Category:   diagnose.CategoryOrphanSecurityGroup,
			CategoryPayload: map[string]any{
				"id": sg.ID, "label": sg.Label, "kind": string(sg.Kind),
			},
		})
	}
	return out
}
