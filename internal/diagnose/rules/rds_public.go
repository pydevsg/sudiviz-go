package rules

import (
	"fmt"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// RDSPublic flags database instances reachable from the public internet.
type RDSPublic struct{}

func (RDSPublic) Name() string                { return "rds_public" }
func (RDSPublic) Severity() diagnose.Severity { return diagnose.SeverityWarning }

func (RDSPublic) Evaluate(g *graph.InfraGraph) []diagnose.Finding {
	var out []diagnose.Finding
	for _, db := range g.ResourcesByKind(graph.KindRDS) {
		if !db.AttrBool("publicly_accessible") {
			continue
		}
		out = append(out, diagnose.Finding{
			Severity:   diagnose.SeverityWarning,
			ResourceID: db.ID,
			Title:      fmt.Sprintf("RDS '%s': publicly accessible", db.Label),
			Detail: "Instance is publicly accessible. Unless intentional, disable this and use " +
				"a bastion host or VPN for access.",
		})
	}
	return out
}
