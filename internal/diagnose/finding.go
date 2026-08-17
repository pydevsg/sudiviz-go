// Package diagnose runs pluggable rules over the topology graph and produces
// severity-ranked findings.
package diagnose

// Severity classifies how urgent a finding is.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// Rank orders severities: critical < warning < info (lower = more urgent).
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityWarning:
		return 1
	case SeverityInfo:
		return 2
	}
	return 3
}

// Finding is one actionable diagnostic result.
type Finding struct {
	Severity       Severity `json:"severity"`
	Title          string   `json:"title"`
	Detail         string   `json:"detail"`
	ResourceID     string   `json:"resource_id,omitempty"`
	AWSConsoleHint *string  `json:"aws_console_hint"`

	// Category slots the finding into the corresponding Diagnosis list
	// ("unhealthy_target_group", "insecure_s3_bucket", ...). Optional.
	Category string `json:"-"`
	// CategoryPayload is the summary entry appended to the category list.
	CategoryPayload map[string]any `json:"-"`
}

// ToMap serializes a finding the way the Python CLI's Fix.__dict__ did.
func (f Finding) ToMap() map[string]any {
	return map[string]any{
		"severity":         f.Severity,
		"title":            f.Title,
		"detail":           f.Detail,
		"resource_id":      nilIfEmpty(f.ResourceID),
		"aws_console_hint": f.AWSConsoleHint,
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Diagnosis is the full diagnostic output: categorized problem lists plus the
// flat, severity-sorted findings ("fixes" for CLI/JSON parity with the
// original tool).
type Diagnosis struct {
	OrphanTargetGroups    []map[string]any `json:"orphan_target_groups"`
	OrphanInstances       []map[string]any `json:"orphan_instances"`
	OrphanSecurityGroups  []map[string]any `json:"orphan_security_groups"`
	UnhealthyTargetGroups []map[string]any `json:"unhealthy_target_groups"`
	UnhealthyECSServices  []map[string]any `json:"unhealthy_ecs_services"`
	UnhealthyEKSClusters  []map[string]any `json:"unhealthy_eks_clusters"`
	UnhealthyRDSInstances []map[string]any `json:"unhealthy_rds_instances"`
	InsecureS3Buckets     []map[string]any `json:"insecure_s3_buckets"`
	Fixes                 []Finding        `json:"fixes"`
}

// HasCritical reports whether any finding is critical (drives exit code 2).
func (d *Diagnosis) HasCritical() bool {
	for _, f := range d.Fixes {
		if f.Severity == SeverityCritical {
			return true
		}
	}
	return false
}

// CountBySeverity returns the number of findings with the given severity.
func (d *Diagnosis) CountBySeverity(s Severity) int {
	n := 0
	for _, f := range d.Fixes {
		if f.Severity == s {
			n++
		}
	}
	return n
}

// ToMap serializes the diagnosis for --json / MCP output.
func (d *Diagnosis) ToMap() map[string]any {
	fixes := make([]map[string]any, 0, len(d.Fixes))
	for _, f := range d.Fixes {
		fixes = append(fixes, f.ToMap())
	}
	return map[string]any{
		"orphan_target_groups":    d.OrphanTargetGroups,
		"orphan_instances":        d.OrphanInstances,
		"orphan_security_groups":  d.OrphanSecurityGroups,
		"unhealthy_target_groups": d.UnhealthyTargetGroups,
		"unhealthy_ecs_services":  d.UnhealthyECSServices,
		"unhealthy_eks_clusters":  d.UnhealthyEKSClusters,
		"unhealthy_rds_instances": d.UnhealthyRDSInstances,
		"insecure_s3_buckets":     d.InsecureS3Buckets,
		"fixes":                   fixes,
	}
}

// Filter returns a copy of the diagnosis keeping only findings at the given
// severities (empty = keep all).
func (d *Diagnosis) Filter(severities ...Severity) []Finding {
	if len(severities) == 0 {
		return d.Fixes
	}
	keep := map[Severity]bool{}
	for _, s := range severities {
		keep[s] = true
	}
	var out []Finding
	for _, f := range d.Fixes {
		if keep[f.Severity] {
			out = append(out, f)
		}
	}
	return out
}

// Category name constants used by rules to slot findings into lists.
const (
	CategoryOrphanTargetGroup    = "orphan_target_group"
	CategoryOrphanInstance       = "orphan_instance"
	CategoryOrphanSecurityGroup  = "orphan_security_group"
	CategoryUnhealthyTargetGroup = "unhealthy_target_group"
	CategoryUnhealthyECSService  = "unhealthy_ecs_service"
	CategoryUnhealthyEKSCluster  = "unhealthy_eks_cluster"
	CategoryUnhealthyRDSInstance = "unhealthy_rds_instance"
	CategoryInsecureS3Bucket     = "insecure_s3_bucket"
)
