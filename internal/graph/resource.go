// Package graph models the infrastructure topology as a directed graph of
// cloud-agnostic resources and typed relationships. Provider-specific detail
// lives in Resource.Attrs so GCP/Azure discoverers can populate the same
// structures without changes here.
package graph

// Health is a normalized, provider-agnostic health state. Provider-specific
// states (ELB target health, RDS status, Lambda state, ...) map onto these.
type Health string

const (
	HealthHealthy   Health = "healthy"
	HealthUnhealthy Health = "unhealthy"
	HealthInitial   Health = "initial"
	HealthDraining  Health = "draining"
	HealthUnused    Health = "unused"
	HealthUnknown   Health = "unknown"
)

// Kind identifies the generic resource type of a node.
type Kind string

const (
	KindALB           Kind = "alb"
	KindTargetGroup   Kind = "target_group"
	KindInstance      Kind = "instance"
	KindSecurityGroup Kind = "security_group"
	KindVPC           Kind = "vpc"
	KindECSCluster    Kind = "ecs_cluster"
	KindECSService    Kind = "ecs_service"
	KindEKSCluster    Kind = "eks_cluster"
	KindEKSNodeGroup  Kind = "eks_nodegroup"
	KindRDS           Kind = "rds"
	KindAurora        Kind = "aurora"
	KindLambda        Kind = "lambda"
	KindS3            Kind = "s3"
	KindUnknown       Kind = "unknown"
)

// SGRule is a single ingress/egress rule on a security group. It is stored in
// the security group node's Attrs under "rules".
type SGRule struct {
	Direction       string   `json:"direction"` // "ingress" | "egress"
	Protocol        string   `json:"protocol"`  // "tcp", "udp", "icmp", "-1"
	FromPort        *int32   `json:"from_port"`
	ToPort          *int32   `json:"to_port"`
	CIDRRanges      []string `json:"cidr_ranges"`
	ReferencedSGIDs []string `json:"referenced_sg_ids"`
	Description     string   `json:"description,omitempty"`
}

// Resource is one node in the topology graph.
type Resource struct {
	ID          string            `json:"id"` // ARN or provider-side ID; unique node key
	Kind        Kind              `json:"kind"`
	Label       string            `json:"label"`
	Provider    string            `json:"provider"` // "aws" | "gcp" | "azure"
	Region      string            `json:"region,omitempty"`
	AZ          string            `json:"az,omitempty"`
	Health      Health            `json:"health"`
	Orphan      bool              `json:"orphan"`
	MonthlyCost float64           `json:"monthly_cost"`
	Tags        map[string]string `json:"tags,omitempty"`
	Attrs       map[string]any    `json:"metadata,omitempty"`
}

// Attr returns a raw attribute value.
func (r *Resource) Attr(key string) any {
	if r.Attrs == nil {
		return nil
	}
	return r.Attrs[key]
}

// AttrString returns a string attribute, or "" if absent.
func (r *Resource) AttrString(key string) string {
	v, _ := r.Attr(key).(string)
	return v
}

// AttrInt returns an integer attribute, tolerating int/int32/int64/float64.
func (r *Resource) AttrInt(key string) int {
	switch v := r.Attr(key).(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// AttrBool returns a boolean attribute, or false if absent.
func (r *Resource) AttrBool(key string) bool {
	v, _ := r.Attr(key).(bool)
	return v
}

// AttrBoolDefault returns a boolean attribute, or def if the key is absent.
func (r *Resource) AttrBoolDefault(key string, def bool) bool {
	if r.Attr(key) == nil {
		return def
	}
	return r.AttrBool(key)
}

// AttrStrings returns a string-slice attribute, or nil if absent.
func (r *Resource) AttrStrings(key string) []string {
	switch v := r.Attr(key).(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// SGRules returns the parsed security group rules stored under "rules".
func (r *Resource) SGRules() []SGRule {
	v, _ := r.Attr("rules").([]SGRule)
	return v
}
