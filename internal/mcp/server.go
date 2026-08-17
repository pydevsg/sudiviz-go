// Package mcp exposes sudiviz as an MCP server (stdio transport).
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/pydevsg/sudiviz-go/internal/discovery"
	"github.com/pydevsg/sudiviz-go/internal/drift"
	"github.com/pydevsg/sudiviz-go/internal/fix"
	"github.com/pydevsg/sudiviz-go/internal/fix/remediators"
	"github.com/pydevsg/sudiviz-go/internal/graph"
	"github.com/pydevsg/sudiviz-go/internal/render"
	"github.com/pydevsg/sudiviz-go/internal/run"
	"github.com/pydevsg/sudiviz-go/internal/version"
)

func awsOpts(req mcp.CallToolRequest) run.Options {
	return run.Options{
		Region:     req.GetString("region", ""),
		VPCID:      req.GetString("vpc_id", ""),
		ServiceTag: req.GetString("service_tag", ""),
		Profile:    req.GetString("profile", ""),
	}
}

func textJSON(v any) *mcp.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(`{"error":"` + err.Error() + `"}`)
	}
	return mcp.NewToolResultText(string(b))
}

func live(ctx context.Context, req mcp.CallToolRequest) (*run.Snapshot, error) {
	return run.Live(ctx, awsOpts(req))
}

// NewServer builds the sudiviz MCP server with tools, resources, and prompts.
func NewServer() *server.MCPServer {
	s := server.NewMCPServer(
		"sudiviz",
		version.Version,
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
	)

	common := []mcp.ToolOption{
		mcp.WithString("region", mcp.Description("AWS region (e.g. us-east-1). Uses default if omitted.")),
		mcp.WithString("vpc_id", mcp.Description("Filter to a specific VPC.")),
		mcp.WithString("service_tag", mcp.Description("Tag filter, e.g. Service=checkout.")),
		mcp.WithString("profile", mcp.Description("AWS profile name from ~/.aws/credentials.")),
	}

	s.AddTool(mcp.NewTool("sudiviz_discover", append([]mcp.ToolOption{
		mcp.WithDescription("Discover live AWS infrastructure resources. Returns ALBs, target groups, EC2 instances, security groups, ECS, EKS, RDS, Aurora, Lambda, and S3 buckets."),
	}, common...)...), handleDiscover)

	s.AddTool(mcp.NewTool("sudiviz_diagnose", append([]mcp.ToolOption{
		mcp.WithDescription("Discover AWS infrastructure and analyze for issues: orphan resources, unhealthy targets, security group misconfigurations, insecure S3 buckets, and more."),
	}, common...)...), handleDiagnose)

	s.AddTool(mcp.NewTool("sudiviz_graph", append([]mcp.ToolOption{
		mcp.WithDescription("Generate the infrastructure topology graph as Cytoscape-compatible JSON."),
	}, common...)...), handleGraph)

	s.AddTool(mcp.NewTool("sudiviz_fix", append([]mcp.ToolOption{
		mcp.WithDescription("Generate remediation commands for diagnosed infrastructure issues. Set dry_run=false to apply non-destructive fixes."),
		mcp.WithBoolean("dry_run", mcp.Description("If true (default), only show commands without applying.")),
		mcp.WithString("issue_filter", mcp.Description("Only show fixes matching this substring.")),
	}, common...)...), handleFix)

	s.AddTool(mcp.NewTool("sudiviz_drift", append([]mcp.ToolOption{
		mcp.WithDescription("Compare Terraform state against live AWS infrastructure to detect drift."),
		mcp.WithString("tfstate_path", mcp.Description("Path to the terraform state JSON file."), mcp.Required()),
	}, common...)...), handleDrift)

	s.AddTool(mcp.NewTool("sudiviz_costs", append([]mcp.ToolOption{
		mcp.WithDescription("Estimate monthly costs for discovered AWS resources."),
	}, common...)...), handleCosts)

	s.AddTool(mcp.NewTool("sudiviz_list_resources", append([]mcp.ToolOption{
		mcp.WithDescription("List discovered resources of a specific type."),
		mcp.WithString("kind", mcp.Description("Resource type to list."), mcp.Required(),
			mcp.Enum("alb", "target_group", "instance", "security_group", "ecs_cluster", "eks_cluster", "rds", "aurora", "lambda", "s3")),
	}, common...)...), handleList)

	s.AddResourceTemplate(mcp.NewResourceTemplate(
		"infra://aws/{region}/topology", "aws-topology",
		mcp.WithTemplateDescription("Live AWS infrastructure topology for a region as JSON."),
		mcp.WithTemplateMIMEType("application/json"),
	), handleResource)

	s.AddResourceTemplate(mcp.NewResourceTemplate(
		"infra://aws/{region}/health", "aws-health",
		mcp.WithTemplateDescription("Health status summary for AWS resources in a region."),
		mcp.WithTemplateMIMEType("application/json"),
	), handleResource)

	s.AddResourceTemplate(mcp.NewResourceTemplate(
		"infra://aws/{region}/costs", "aws-costs",
		mcp.WithTemplateDescription("Estimated monthly cost breakdown for a region."),
		mcp.WithTemplateMIMEType("application/json"),
	), handleResource)

	for _, p := range []struct{ name, desc string }{
		{"diagnose-infrastructure", "Analyze AWS infrastructure in a region for issues and recommend fixes."},
		{"cost-optimization", "Find cost-saving opportunities in AWS infrastructure."},
		{"security-audit", "Check for security misconfigurations: open security groups, public databases, unencrypted storage."},
		{"incident-triage", "Trace unhealthy resources through the dependency chain to identify root cause."},
	} {
		s.AddPrompt(mcp.NewPrompt(p.name,
			mcp.WithPromptDescription(p.desc),
			mcp.WithArgument("region", mcp.ArgumentDescription("AWS region (e.g. us-east-1)")),
			mcp.WithArgument("profile", mcp.ArgumentDescription("AWS profile name")),
		), handlePrompt)
	}
	return s
}

// ServeStdio starts the MCP server on stdin/stdout.
func ServeStdio() error {
	return server.ServeStdio(NewServer())
}

func handleDiscover(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	snap, err := live(ctx, req)
	if err != nil {
		return mcp.NewToolResultText(errJSON(err)), nil
	}
	return textJSON(map[string]any{
		"provider":        "aws",
		"account_id":      snap.Graph.AccountID,
		"region":          snap.Graph.Region,
		"vpc_id":          snap.Graph.VPCID,
		"resource_counts": run.ResourceCounts(snap.Graph),
		"resources":       render.SerializeGraph(snap.Graph),
	}), nil
}

func handleDiagnose(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	snap, err := live(ctx, req)
	if err != nil {
		return mcp.NewToolResultText(errJSON(err)), nil
	}
	return textJSON(map[string]any{
		"provider":        "aws",
		"account_id":      snap.Graph.AccountID,
		"region":          snap.Graph.Region,
		"resource_counts": run.ResourceCounts(snap.Graph),
		"diagnosis":       snap.Diagnosis.ToMap(),
		"fix_count":       len(snap.Diagnosis.Fixes),
		"critical_count":  snap.Diagnosis.CountBySeverity("critical"),
		"warning_count":   snap.Diagnosis.CountBySeverity("warning"),
	}), nil
}

func handleGraph(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	snap, err := live(ctx, req)
	if err != nil {
		return mcp.NewToolResultText(errJSON(err)), nil
	}
	return textJSON(render.ExportCytoscape(snap.Graph)), nil
}

func handleFix(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	snap, err := live(ctx, req)
	if err != nil {
		return mcp.NewToolResultText(errJSON(err)), nil
	}
	if len(snap.Diagnosis.Fixes) == 0 {
		return textJSON(map[string]any{"message": "No issues found — nothing to fix!", "fixes": []any{}}), nil
	}
	actions := fix.Generate(snap.Diagnosis, snap.Graph, snap.Graph.Region, remediators.All())
	if needle := strings.ToLower(req.GetString("issue_filter", "")); needle != "" {
		var filtered []*fix.Action
		for _, a := range actions {
			if strings.Contains(strings.ToLower(a.Title), needle) {
				filtered = append(filtered, a)
			}
		}
		actions = filtered
	}
	dryRun := req.GetBool("dry_run", true)
	if !dryRun {
		clients := fix.NewAWSClients(snap.Config)
		for _, a := range actions {
			if a.HasAutomatedFix() && !a.IsDestructive {
				fix.Apply(ctx, a, clients)
			}
		}
	}
	var payload []map[string]any
	for _, a := range actions {
		payload = append(payload, map[string]any{
			"title":           a.Title,
			"severity":        a.Severity,
			"description":     a.Description,
			"aws_cli_command": a.AWSCLICommand,
			"is_destructive":  a.IsDestructive,
			"applied":         a.Applied,
			"error":           a.Error,
		})
	}
	return textJSON(payload), nil
}

func handleDrift(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := req.GetString("tfstate_path", "")
	if _, err := os.Stat(path); err != nil {
		return textJSON(map[string]any{"error": fmt.Sprintf("File not found: %s", path)}), nil
	}
	state, err := drift.LoadState(path)
	if err != nil {
		return mcp.NewToolResultText(errJSON(err)), nil
	}
	snap, err := live(ctx, req)
	if err != nil {
		return mcp.NewToolResultText(errJSON(err)), nil
	}
	findings := drift.Detect(drift.ParseIntended(state), snap.Graph)
	return textJSON(map[string]any{
		"drift_detected": len(findings) > 0,
		"finding_count":  len(findings),
		"findings":       findings,
	}), nil
}

func handleCosts(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	snap, err := live(ctx, req)
	if err != nil {
		return mcp.NewToolResultText(errJSON(err)), nil
	}
	return textJSON(discovery.SummarizeCosts(snap.Graph)), nil
}

func handleList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kind := req.GetString("kind", "")
	snap, err := live(ctx, req)
	if err != nil {
		return mcp.NewToolResultText(errJSON(err)), nil
	}
	var resources []map[string]any
	for _, n := range snap.Graph.ResourcesByKind(graph.Kind(kind)) {
		resources = append(resources, map[string]any{
			"id": n.ID, "label": n.Label, "kind": n.Kind, "health": n.Health,
			"orphan": n.Orphan, "monthly_cost": n.MonthlyCost, "metadata": n.Attrs,
		})
	}
	return textJSON(map[string]any{
		"kind": kind, "count": len(resources), "region": snap.Graph.Region, "resources": resources,
	}), nil
}

func handleResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	uri := req.Params.URI
	region, kind, err := parseInfraURI(uri)
	if err != nil {
		return jsonResource(uri, map[string]any{"error": err.Error()}), nil
	}
	snap, err := run.Live(ctx, run.Options{Region: region})
	if err != nil {
		return jsonResource(uri, map[string]any{"error": err.Error()}), nil
	}
	var data any
	switch kind {
	case "topology":
		data = render.ExportCytoscape(snap.Graph)
	case "health":
		data = map[string]any{
			"region":          region,
			"account_id":      snap.Graph.AccountID,
			"resource_counts": run.ResourceCounts(snap.Graph),
			"fix_count":       len(snap.Diagnosis.Fixes),
			"critical_count":  snap.Diagnosis.CountBySeverity("critical"),
			"warning_count":   snap.Diagnosis.CountBySeverity("warning"),
			"fixes":           snap.Diagnosis.ToMap()["fixes"],
		}
	case "costs":
		data = discovery.SummarizeCosts(snap.Graph)
	default:
		data = map[string]any{"error": "Unknown resource kind: " + kind}
	}
	return jsonResource(uri, data), nil
}

func parseInfraURI(uri string) (region, kind string, err error) {
	const prefix = "infra://aws/"
	if !strings.HasPrefix(uri, prefix) {
		return "", "", fmt.Errorf("unknown resource URI: %s", uri)
	}
	rest := strings.TrimPrefix(uri, prefix)
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid resource path: %s", rest)
	}
	return parts[0], parts[1], nil
}

func jsonResource(uri string, v any) []mcp.ResourceContents {
	b, _ := json.MarshalIndent(v, "", "  ")
	return []mcp.ResourceContents{mcp.TextResourceContents{
		URI: uri, MIMEType: "application/json", Text: string(b),
	}}
}

func handlePrompt(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	args := req.Params.Arguments
	region := args["region"]
	if region == "" {
		region = "your default region"
	}
	profileNote := ""
	if p := args["profile"]; p != "" {
		profileNote = fmt.Sprintf(" using AWS profile '%s'", p)
	}
	text := promptText(req.Params.Name, region, profileNote)
	return mcp.NewGetPromptResult(fmt.Sprintf("%s for %s", req.Params.Name, region), []mcp.PromptMessage{
		mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(text)),
	}), nil
}

func promptText(name, region, profileNote string) string {
	switch name {
	case "diagnose-infrastructure":
		return fmt.Sprintf("Analyze my AWS infrastructure in %s%s for issues.\n\n1. First, use sudiviz_diagnose to discover resources and run diagnosis.\n2. Summarize the resource counts and any issues found.\n3. For each issue, explain the risk and recommend a fix.\n4. Prioritize critical issues first, then warnings.\n5. If there are fixable issues, show the AWS CLI commands using sudiviz_fix (dry_run=true).", region, profileNote)
	case "cost-optimization":
		return fmt.Sprintf("Analyze my AWS infrastructure costs in %s%s.\n\n1. Use sudiviz_costs to get the cost breakdown by service and resource.\n2. Identify the most expensive resources.\n3. Use sudiviz_diagnose to check for orphan resources that are wasting money.\n4. Suggest specific cost-saving actions (right-sizing, deleting orphans, reserved instances).\n5. Estimate potential monthly savings for each suggestion.", region, profileNote)
	case "security-audit":
		return fmt.Sprintf("Perform a security audit of my AWS infrastructure in %s%s.\n\n1. Use sudiviz_diagnose to discover resources and find security issues.\n2. Check for: open security groups (0.0.0.0/0), publicly accessible RDS, unencrypted S3 buckets, missing encryption at rest.\n3. Rate each finding by severity (critical / warning / info).\n4. For each finding, explain the risk and the remediation command.\n5. Use sudiviz_list_resources with kind='security_group' to review all SG rules.", region, profileNote)
	case "incident-triage":
		return fmt.Sprintf("Something may be wrong with my AWS infrastructure in %s%s. Help me triage.\n\n1. Use sudiviz_diagnose to find all current issues.\n2. Use sudiviz_graph to get the topology and trace dependencies.\n3. Focus on critical issues first — unhealthy targets, failing health checks.\n4. Trace the dependency chain: ALB → Target Group → Instance → Security Group.\n5. Identify the root cause and suggest immediate remediation steps.\n6. If a fix is available, show the dry-run command with sudiviz_fix.", region, profileNote)
	default:
		return "Unknown prompt. Available: diagnose-infrastructure, cost-optimization, security-audit, incident-triage."
	}
}

func errJSON(err error) string {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b)
}
