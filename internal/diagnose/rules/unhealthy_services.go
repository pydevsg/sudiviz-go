package rules

import (
	"fmt"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

// UnhealthyECSServices flags services running fewer tasks than desired:
// critical when zero tasks run, warning otherwise.
type UnhealthyECSServices struct{}

func (UnhealthyECSServices) Name() string                { return "unhealthy_ecs_services" }
func (UnhealthyECSServices) Severity() diagnose.Severity { return diagnose.SeverityCritical }

func (UnhealthyECSServices) Evaluate(g *graph.InfraGraph) []diagnose.Finding {
	var out []diagnose.Finding
	for _, svc := range g.ResourcesByKind(graph.KindECSService) {
		running := svc.AttrInt("running_count")
		desired := svc.AttrInt("desired_count")
		if desired == 0 || running >= desired {
			continue
		}
		severity := diagnose.SeverityWarning
		if running == 0 {
			severity = diagnose.SeverityCritical
		}
		out = append(out, diagnose.Finding{
			Severity:   severity,
			ResourceID: svc.ID,
			Title:      fmt.Sprintf("ECS service '%s': %d/%d tasks running", svc.Label, running, desired),
			Detail: fmt.Sprintf(
				"%d task(s) not running. Check CloudWatch logs for the service, "+
					"verify the task definition is valid, and confirm the cluster has capacity "+
					"(CPU/memory). Also check security group rules if tasks fail health checks.",
				desired-running),
			Category: diagnose.CategoryUnhealthyECSService,
			CategoryPayload: map[string]any{
				"id": svc.ID, "label": svc.Label, "running": running, "desired": desired,
			},
		})
	}
	return out
}

// UnhealthyEKSClusters flags clusters and node groups not in ACTIVE state.
type UnhealthyEKSClusters struct{}

func (UnhealthyEKSClusters) Name() string                { return "unhealthy_eks_clusters" }
func (UnhealthyEKSClusters) Severity() diagnose.Severity { return diagnose.SeverityCritical }

func (UnhealthyEKSClusters) Evaluate(g *graph.InfraGraph) []diagnose.Finding {
	var out []diagnose.Finding
	for _, cluster := range g.ResourcesByKind(graph.KindEKSCluster) {
		if cluster.Health != graph.HealthHealthy {
			out = append(out, diagnose.Finding{
				Severity:   diagnose.SeverityCritical,
				ResourceID: cluster.ID,
				Title:      fmt.Sprintf("EKS cluster '%s' is not ACTIVE", cluster.Label),
				Detail: "Cluster status is not ACTIVE. Check the EKS console for creation/update errors, " +
					"verify IAM roles have the required permissions, and check subnet/VPC configuration.",
				Category:        diagnose.CategoryUnhealthyEKSCluster,
				CategoryPayload: map[string]any{"id": cluster.ID, "label": cluster.Label},
			})
		}
		for _, e := range g.OutEdges(cluster.ID, graph.RelationContains) {
			ng := g.Resource(e.To)
			if ng == nil || ng.Kind != graph.KindEKSNodeGroup || ng.Health == graph.HealthHealthy {
				continue
			}
			out = append(out, diagnose.Finding{
				Severity:   diagnose.SeverityWarning,
				ResourceID: ng.ID,
				Title:      fmt.Sprintf("EKS node group '%s' is not ACTIVE", ng.Label),
				Detail: "Node group is not in ACTIVE state. Check IAM node role, launch template, " +
					"and whether the desired instance type is available in the selected AZs.",
			})
		}
	}
	return out
}

// UnhealthyRDSInstances flags databases whose status is not "available".
type UnhealthyRDSInstances struct{}

func (UnhealthyRDSInstances) Name() string                { return "unhealthy_rds_instances" }
func (UnhealthyRDSInstances) Severity() diagnose.Severity { return diagnose.SeverityCritical }

func (UnhealthyRDSInstances) Evaluate(g *graph.InfraGraph) []diagnose.Finding {
	var out []diagnose.Finding
	for _, db := range g.ResourcesByKind(graph.KindRDS) {
		status := db.AttrString("status")
		if status == "available" {
			continue
		}
		if status == "" {
			status = "unknown"
		}
		severity := diagnose.SeverityWarning
		if status == "failed" || status == "incompatible-network" {
			severity = diagnose.SeverityCritical
		}
		out = append(out, diagnose.Finding{
			Severity:   severity,
			ResourceID: db.ID,
			Title:      fmt.Sprintf("RDS '%s' status: %s", db.Label, status),
			Detail: fmt.Sprintf(
				"Instance is not 'available' (current: %s). "+
					"Check RDS events in the console and verify parameter group and subnet group are valid.", status),
			Category:        diagnose.CategoryUnhealthyRDSInstance,
			CategoryPayload: map[string]any{"id": db.ID, "label": db.Label, "status": status},
		})
	}
	return out
}

// UnhealthyLambdas flags functions whose state is not Active.
type UnhealthyLambdas struct{}

func (UnhealthyLambdas) Name() string                { return "unhealthy_lambdas" }
func (UnhealthyLambdas) Severity() diagnose.Severity { return diagnose.SeverityWarning }

func (UnhealthyLambdas) Evaluate(g *graph.InfraGraph) []diagnose.Finding {
	var out []diagnose.Finding
	for _, fn := range g.ResourcesByKind(graph.KindLambda) {
		if fn.Health == graph.HealthHealthy {
			continue
		}
		out = append(out, diagnose.Finding{
			Severity:   diagnose.SeverityWarning,
			ResourceID: fn.ID,
			Title:      fmt.Sprintf("Lambda '%s' is not Active", fn.Label),
			Detail: "Function state is not Active. Check for failed deployments, " +
				"missing layers, or broken VPC config that prevents ENI creation.",
		})
	}
	return out
}
