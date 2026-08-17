package graph

// Relation is the semantic type of an edge. Every edge in the topology has
// exactly one relation; diagnostic rules query the graph by relation.
type Relation string

const (
	// RelationForwardsTo connects a load balancer to a target group via a
	// listener (ALB -> TG). This is the edge that proves reachability.
	RelationForwardsTo Relation = "forwards_to"
	// RelationRegisteredIn connects a backend target to the target group it
	// is registered in (instance/lambda -> TG).
	RelationRegisteredIn Relation = "registered_in"
	// RelationGuardedBy connects a resource to a security group attached to
	// it (instance/alb/service/db -> SG).
	RelationGuardedBy Relation = "guarded_by"
	// RelationInVPC connects a resource to its VPC node.
	RelationInVPC Relation = "in_vpc"
	// RelationContains connects a cluster to its members (ECS cluster ->
	// service, EKS cluster -> node group).
	RelationContains Relation = "contains"
	// RelationBackedBy connects an ECS service to the target group backing it.
	RelationBackedBy Relation = "backed_by"
	// RelationInvokes connects an event source to the Lambda it triggers.
	RelationInvokes Relation = "invokes"
	// RelationAllowsIngress / RelationAllowsEgress connect security groups
	// that reference each other in rules (traffic-flow edges).
	RelationAllowsIngress Relation = "allows_ingress"
	RelationAllowsEgress  Relation = "allows_egress"
	// S3 wiring heuristics (mirror the Python version): connect buckets into
	// the topology instead of leaving them floating.
	RelationLogsTo    Relation = "logs_to"
	RelationReadsFrom Relation = "reads_from"
	RelationAccesses  Relation = "accesses"
	RelationBacksUpTo Relation = "backs_up_to"
)

// Edge is one directed, typed edge in the topology graph.
type Edge struct {
	From     string         `json:"source"`
	To       string         `json:"target"`
	Relation Relation       `json:"relation"`
	Style    string         `json:"style"`  // "solid" | "dashed"
	Orphan   bool           `json:"orphan"` // touches an orphan node
	Attrs    map[string]any `json:"attrs,omitempty"`
}

// AttrInt returns an integer edge attribute, tolerating common numeric types.
func (e *Edge) AttrInt(key string) int {
	switch v := e.Attrs[key].(type) {
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

// AttrString returns a string edge attribute, or "" if absent.
func (e *Edge) AttrString(key string) string {
	v, _ := e.Attrs[key].(string)
	return v
}
