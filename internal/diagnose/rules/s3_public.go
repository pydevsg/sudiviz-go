package rules

import (
	"fmt"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// S3Public flags buckets whose Block Public Access configuration is not fully
// restrictive — publicly readable buckets can leak sensitive data.
type S3Public struct{}

func (S3Public) Name() string                { return "s3_public" }
func (S3Public) Severity() diagnose.Severity { return diagnose.SeverityCritical }

func (S3Public) Evaluate(g *graph.InfraGraph) []diagnose.Finding {
	var out []diagnose.Finding
	for _, bucket := range g.ResourcesByKind(graph.KindS3) {
		if bucket.AttrBoolDefault("public_access_blocked", true) {
			continue
		}
		out = append(out, diagnose.Finding{
			Severity:   diagnose.SeverityCritical,
			ResourceID: bucket.ID,
			Title:      fmt.Sprintf("S3 bucket '%s': public access not fully blocked", bucket.Label),
			Detail: "Enable S3 Block Public Access on this bucket. " +
				"Publicly readable buckets can leak sensitive data.",
			Category:        diagnose.CategoryInsecureS3Bucket,
			CategoryPayload: map[string]any{"id": bucket.ID, "label": bucket.Label},
		})
	}
	return out
}
