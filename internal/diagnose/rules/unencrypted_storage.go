package rules

import (
	"fmt"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// UnencryptedStorage flags storage without encryption at rest: RDS instances,
// S3 buckets without default SSE, and EC2 instances with unencrypted EBS
// volumes.
type UnencryptedStorage struct{}

func (UnencryptedStorage) Name() string                { return "unencrypted_storage" }
func (UnencryptedStorage) Severity() diagnose.Severity { return diagnose.SeverityWarning }

func (UnencryptedStorage) Evaluate(g *graph.InfraGraph) []diagnose.Finding {
	var out []diagnose.Finding

	for _, db := range g.ResourcesByKind(graph.KindRDS) {
		if db.AttrBool("storage_encrypted") {
			continue
		}
		out = append(out, diagnose.Finding{
			Severity:   diagnose.SeverityWarning,
			ResourceID: db.ID,
			Title:      fmt.Sprintf("RDS '%s': storage not encrypted", db.Label),
			Detail:     "Enable storage encryption at rest. Requires a snapshot restore to enable on existing instances.",
		})
	}

	for _, bucket := range g.ResourcesByKind(graph.KindS3) {
		if bucket.AttrBool("encryption_enabled") {
			continue
		}
		out = append(out, diagnose.Finding{
			Severity:   diagnose.SeverityWarning,
			ResourceID: bucket.ID,
			Title:      fmt.Sprintf("S3 bucket '%s': server-side encryption not enabled", bucket.Label),
			Detail:     "Enable SSE-S3 or SSE-KMS default encryption on this bucket.",
		})
	}

	for _, inst := range g.ResourcesByKind(graph.KindInstance) {
		// Only flag instances whose volumes were actually inspected.
		if !inst.AttrBool("ebs_checked") || inst.AttrBool("ebs_encrypted") {
			continue
		}
		out = append(out, diagnose.Finding{
			Severity:   diagnose.SeverityWarning,
			ResourceID: inst.ID,
			Title:      fmt.Sprintf("Instance '%s': EBS volumes not encrypted", inst.Label),
			Detail:     "One or more attached EBS volumes are unencrypted. Enable EBS encryption by default and migrate via snapshot copy.",
		})
	}
	return out
}
