// Package drift compares Terraform state against the live topology graph.
package drift

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// IntendedResource is a resource as Terraform expects it to exist.
type IntendedResource struct {
	Address    string
	Type       string
	Name       string
	Attributes map[string]any
}

// AWSID returns the AWS-side ID/ARN exported by Terraform, if any.
func (r IntendedResource) AWSID() string {
	for _, key := range []string{"arn", "id", "group_id"} {
		if v, ok := r.Attributes[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// Item is one drift finding.
type Item struct {
	Kind         string         `json:"kind"` // missing | orphan_in_aws | attribute_mismatch | orphan_listener
	ResourceType string         `json:"resource_type"`
	Address      string         `json:"address,omitempty"`
	AWSID        string         `json:"aws_id,omitempty"`
	Message      string         `json:"message"`
	Details      map[string]any `json:"details,omitempty"`
}

var tfTypeToKind = map[string]graph.Kind{
	"aws_lb":               graph.KindALB,
	"aws_alb":              graph.KindALB,
	"aws_lb_target_group":  graph.KindTargetGroup,
	"aws_alb_target_group": graph.KindTargetGroup,
	"aws_security_group":   graph.KindSecurityGroup,
	"aws_instance":         graph.KindInstance,
	"aws_ecs_cluster":      graph.KindECSCluster,
	"aws_ecs_service":      graph.KindECSService,
	"aws_eks_cluster":      graph.KindEKSCluster,
	"aws_db_instance":      graph.KindRDS,
	"aws_lambda_function":  graph.KindLambda,
}

// LoadState reads `terraform show -json` output (or a raw tfstate) from disk.
func LoadState(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("parse terraform state: %w", err)
	}
	return data, nil
}

// ParseIntended walks the state tree and emits a flat list of resources.
func ParseIntended(state map[string]any) []IntendedResource {
	var out []IntendedResource
	if values, _ := state["values"].(map[string]any); values != nil {
		if root, _ := values["root_module"].(map[string]any); root != nil {
			out = append(out, walkModule(root)...)
		}
	}
	if len(out) == 0 {
		if resources, _ := state["resources"].([]any); resources != nil {
			for _, raw := range resources {
				res, _ := raw.(map[string]any)
				typ, _ := res["type"].(string)
				name, _ := res["name"].(string)
				instances, _ := res["instances"].([]any)
				for _, inst := range instances {
					im, _ := inst.(map[string]any)
					attrs, _ := im["attributes"].(map[string]any)
					out = append(out, IntendedResource{Address: name, Type: typ, Name: name, Attributes: attrs})
				}
			}
		}
	}
	return out
}

func walkModule(module map[string]any) []IntendedResource {
	var out []IntendedResource
	resources, _ := module["resources"].([]any)
	for _, raw := range resources {
		res, _ := raw.(map[string]any)
		addr, _ := res["address"].(string)
		typ, _ := res["type"].(string)
		name, _ := res["name"].(string)
		if addr == "" {
			addr = name
		}
		attrs, _ := res["values"].(map[string]any)
		if attrs == nil {
			attrs = map[string]any{}
		}
		out = append(out, IntendedResource{Address: addr, Type: typ, Name: name, Attributes: attrs})
	}
	children, _ := module["child_modules"].([]any)
	for _, child := range children {
		if cm, ok := child.(map[string]any); ok {
			out = append(out, walkModule(cm)...)
		}
	}
	return out
}

// Detect compares intended Terraform resources against the live graph.
func Detect(intended []IntendedResource, g *graph.InfraGraph) []Item {
	var findings []Item

	liveIDs := map[string]bool{}
	liveByKind := map[graph.Kind]map[string]bool{}
	for _, n := range g.Resources() {
		liveIDs[n.ID] = true
		if liveByKind[n.Kind] == nil {
			liveByKind[n.Kind] = map[string]bool{}
		}
		liveByKind[n.Kind][n.ID] = true
	}

	intendedByType := map[string][]IntendedResource{}
	intendedIDs := map[string]bool{}
	for _, r := range intended {
		intendedByType[r.Type] = append(intendedByType[r.Type], r)
		if id := r.AWSID(); id != "" {
			intendedIDs[id] = true
		}
	}

	for tfType, kind := range tfTypeToKind {
		live := liveByKind[kind]
		for _, intent := range intendedByType[tfType] {
			awsID := intent.AWSID()
			if awsID == "" {
				continue
			}
			if !live[awsID] {
				findings = append(findings, Item{
					Kind:         "missing",
					ResourceType: tfType,
					Address:      intent.Address,
					AWSID:        awsID,
					Message:      fmt.Sprintf("%s declared in Terraform but not found in AWS", intent.Address),
				})
			}
		}
	}

	for id := range liveIDs {
		if intendedIDs[id] {
			continue
		}
		n := g.Resource(id)
		if n == nil || n.Kind == graph.KindUnknown || n.Kind == graph.KindVPC {
			continue
		}
		findings = append(findings, Item{
			Kind:         "orphan_in_aws",
			ResourceType: strings.TrimSuffix(string(n.Kind), "s"),
			AWSID:        id,
			Message:      fmt.Sprintf("%s exists in AWS but not in Terraform state", id),
		})
	}

	listenerTargets := map[string]bool{}
	for _, e := range g.Edges() {
		if e.Relation == graph.RelationForwardsTo {
			listenerTargets[e.To] = true
		}
	}
	for _, intent := range intendedByType["aws_lb_listener"] {
		tgARN := extractListenerTarget(intent.Attributes)
		if tgARN != "" && !listenerTargets[tgARN] {
			findings = append(findings, Item{
				Kind:         "orphan_listener",
				ResourceType: "aws_lb_listener",
				Address:      intent.Address,
				AWSID:        tgARN,
				Message: fmt.Sprintf("%s should forward to %s, but no live listener references that target group",
					intent.Address, tgARN),
			})
		}
	}
	return findings
}

func extractListenerTarget(attrs map[string]any) string {
	actions, _ := attrs["default_action"].([]any)
	if len(actions) == 0 {
		return ""
	}
	first, _ := actions[0].(map[string]any)
	if first == nil {
		return ""
	}
	if arn, _ := first["target_group_arn"].(string); arn != "" {
		return arn
	}
	forward, _ := first["forward"].([]any)
	if len(forward) == 0 {
		return ""
	}
	fwd, _ := forward[0].(map[string]any)
	tgs, _ := fwd["target_group"].([]any)
	if len(tgs) == 0 {
		return ""
	}
	tg, _ := tgs[0].(map[string]any)
	arn, _ := tg["arn"].(string)
	return arn
}
