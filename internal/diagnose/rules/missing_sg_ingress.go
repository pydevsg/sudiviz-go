package rules

import (
	"fmt"
	"strings"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// MissingSGIngress finds ALB→target security-group mismatches that block
// ALB→target traffic.
//
// Heuristic: for every (ALB, TG) forwards_to pair where the TG has registered
// instances, verify the instances' SGs allow ingress on the TG port from at
// least one of the ALB's SGs (or from 0.0.0.0/0). If not, traffic can never
// arrive — a high-confidence critical.
type MissingSGIngress struct{}

func (MissingSGIngress) Name() string                { return "missing_sg_ingress" }
func (MissingSGIngress) Severity() diagnose.Severity { return diagnose.SeverityCritical }

func (MissingSGIngress) Evaluate(g *graph.InfraGraph) []diagnose.Finding {
	var out []diagnose.Finding

	albSGs := map[string]map[string]bool{}
	for _, alb := range g.ResourcesByKind(graph.KindALB) {
		set := map[string]bool{}
		for _, sg := range alb.AttrStrings("security_group_ids") {
			set[sg] = true
		}
		albSGs[alb.ID] = set
	}

	for _, e := range g.Edges() {
		if e.Relation != graph.RelationForwardsTo {
			continue
		}
		albID, tgID := e.From, e.To
		tg := g.Resource(tgID)
		if tg == nil {
			continue
		}
		port := tg.AttrInt("port")
		if port == 0 {
			continue
		}
		for _, reg := range g.InEdges(tgID, graph.RelationRegisteredIn) {
			instID := reg.From
			var instSGIDs []string
			for _, guarded := range g.OutEdges(instID, graph.RelationGuardedBy) {
				if sg := g.Resource(guarded.To); sg != nil && sg.Kind == graph.KindSecurityGroup {
					instSGIDs = append(instSGIDs, sg.ID)
				}
			}
			if allowsIngressFrom(g, instSGIDs, albSGs[albID], port) {
				continue
			}
			albName := albID
			if idx := strings.LastIndex(albID, "/"); idx >= 0 {
				albName = albID[idx+1:]
			}
			out = append(out, diagnose.Finding{
				Severity:   diagnose.SeverityCritical,
				ResourceID: instID,
				Title:      fmt.Sprintf("Security group missing port %d from ALB SG", port),
				Detail: fmt.Sprintf(
					"Instance %s security groups do not allow port %d ingress from any ALB "+
						"security group. Add a rule allowing tcp/%d from sg of ALB %s.",
					instID, port, port, albName),
			})
		}
	}
	return out
}

// allowsIngressFrom reports whether any target SG allows tcp/port ingress
// from one of the source SGs (or from anywhere).
func allowsIngressFrom(g *graph.InfraGraph, targetSGIDs []string, sourceSGIDs map[string]bool, port int) bool {
	if len(sourceSGIDs) == 0 || len(targetSGIDs) == 0 {
		return false
	}
	for _, sgID := range targetSGIDs {
		sg := g.Resource(sgID)
		if sg == nil {
			continue
		}
		for _, rule := range sg.SGRules() {
			if rule.Direction != "ingress" {
				continue
			}
			if rule.Protocol != "tcp" && rule.Protocol != "-1" {
				continue
			}
			from, to := 0, 65535
			if rule.FromPort != nil {
				from = int(*rule.FromPort)
			}
			if rule.ToPort != nil {
				to = int(*rule.ToPort)
			}
			if port < from || port > to {
				continue
			}
			for _, ref := range rule.ReferencedSGIDs {
				if sourceSGIDs[ref] {
					return true
				}
			}
			// 0.0.0.0/0 accepts everything (still a misconfiguration, but it
			// does allow the traffic).
			for _, cidr := range rule.CIDRRanges {
				if cidr == "0.0.0.0/0" {
					return true
				}
			}
		}
	}
	return false
}
