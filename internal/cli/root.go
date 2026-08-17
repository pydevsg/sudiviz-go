// Package cli implements the Cobra command tree for the sudiviz binary.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"net/http"

	"github.com/pydevsg/sudiviz-go/internal/config"
	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/drift"
	"github.com/pydevsg/sudiviz-go/internal/explain"
	"github.com/pydevsg/sudiviz-go/internal/fix"
	"github.com/pydevsg/sudiviz-go/internal/fix/remediators"
	"github.com/pydevsg/sudiviz-go/internal/mcp"
	"github.com/pydevsg/sudiviz-go/internal/render"
	"github.com/pydevsg/sudiviz-go/internal/render/static"
	"github.com/pydevsg/sudiviz-go/internal/render/table"
	"github.com/pydevsg/sudiviz-go/internal/render/tui"
	"github.com/pydevsg/sudiviz-go/internal/render/web"
	"github.com/pydevsg/sudiviz-go/internal/run"
	"github.com/pydevsg/sudiviz-go/internal/version"
)

// Execute is the process entry point.
func Execute() {
	if err := newRoot().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "sudiviz",
		Short:         "X-ray vision for your cloud infrastructure.",
		Long:          strings.TrimSpace(version.Logo) + "\n\nDiscover, visualise, diagnose, and fix live AWS infrastructure.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			_ = viper.BindPFlags(cmd.Flags())
			_ = viper.BindPFlags(cmd.PersistentFlags())
			cfg := config.Load()
			if cfg.Verbose {
				log.SetFlags(log.LstdFlags | log.Lshortfile)
				log.SetOutput(os.Stderr)
			} else {
				log.SetOutput(io.Discard)
			}
		},
	}
	pf := cmd.PersistentFlags()
	pf.String("profile", "", "AWS named profile")
	pf.String("region", "", "AWS region override")
	pf.String("vpc-id", "", "Filter discovery to one VPC")
	pf.String("service-tag", "", "Tag filter, e.g. Service=checkout or k=v,k2=v2")
	pf.String("config", "", "Config file path (yaml)")
	pf.BoolP("verbose", "v", false, "Verbose logging")
	pf.String("output", "", "Default output mode (used by graph)")

	_ = viper.BindPFlags(pf)

	cmd.AddCommand(
		diagnoseCmd(),
		graphCmd(),
		fixCmd(),
		tuiCmd(),
		explainCmd(),
		driftCmd(),
		watchCmd(),
		compareCmd(),
		shareCmd(),
		mcpCmd(),
		versionCmd(),
	)
	return cmd
}

func optsFromFlags() run.Options {
	s := config.Load()
	return run.Options{Profile: s.Profile, Region: s.Region, VPCID: s.VPCID, ServiceTag: s.ServiceTag}
}

func ctx() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func jsonFlag(cmd *cobra.Command) bool {
	j, _ := cmd.Flags().GetBool("json")
	format, _ := cmd.Flags().GetString("format")
	return j || strings.EqualFold(format, "json")
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func exitForDiag(d *diagnose.Diagnosis) {
	if d != nil && d.HasCritical() {
		os.Exit(2)
	}
}

func diagnoseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Run live discovery + analysis and print a topology + fixes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := ctx()
			defer cancel()
			snap, err := run.Live(ctx, optsFromFlags())
			if err != nil {
				return err
			}
			severity, _ := cmd.Flags().GetString("severity")
			findings := snap.Diagnosis.Fixes
			if severity != "" {
				findings = snap.Diagnosis.Filter(diagnose.Severity(strings.ToLower(severity)))
			}

			if jsonFlag(cmd) {
				if err := printJSON(map[string]any{
					"graph":     render.SerializeGraph(snap.Graph),
					"diagnosis": snap.Diagnosis.ToMap(),
				}); err != nil {
					return err
				}
				exitForDiag(snap.Diagnosis)
				return nil
			}

			fmt.Fprint(os.Stdout, version.Banner())
			render.WriteTree(os.Stdout, snap.Graph)
			table.Write(os.Stdout, findings)
			for _, w := range snap.Warnings {
				fmt.Fprintln(os.Stderr, lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("warning: "+w))
			}
			speak, _ := cmd.Flags().GetBool("speak")
			if speak {
				speakDiagnosis(snap.Diagnosis)
			}
			exitForDiag(snap.Diagnosis)
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "Emit machine-readable JSON for CI/CD")
	cmd.Flags().String("format", "table", "Output format: table|json")
	cmd.Flags().String("severity", "", "Only show this severity: critical|warning|info")
	cmd.Flags().Bool("show-unattached", false, "Include orphan resources in output")
	cmd.Flags().Bool("highlight-orphans", false, "Render orphans with red dashed lines")
	cmd.Flags().Bool("speak", false, "(macOS) speak the diagnosis aloud via `say`")
	return cmd
}

func graphCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Export the topology as PNG / SVG / JSON, or launch the web view",
		RunE: func(cmd *cobra.Command, _ []string) error {
			output, _ := cmd.Flags().GetString("output")
			if output == "" {
				output = viper.GetString("output")
			}
			if output == "" {
				output = "png"
			}
			output = strings.ToLower(output)
			file, _ := cmd.Flags().GetString("file")
			openAfter, _ := cmd.Flags().GetBool("open")
			port, _ := cmd.Flags().GetInt("port")
			host, _ := cmd.Flags().GetString("host")
			refresh, _ := cmd.Flags().GetFloat64("refresh-interval")

			if output == "web" {
				cfg := web.Config{
					Profile:         viper.GetString("profile"),
					Region:          viper.GetString("region"),
					VPCID:           viper.GetString("vpc-id"),
					ServiceTag:      viper.GetString("service-tag"),
					RefreshInterval: time.Duration(refresh * float64(time.Second)),
					Host:            host,
					Port:            port,
				}
				url := fmt.Sprintf("http://%s:%d", host, port)
				fmt.Fprintf(os.Stdout, "sudiviz web running at %s\n", url)
				if openAfter {
					_ = openURL(url)
				}
				ctx, cancel := ctx()
				defer cancel()
				err := web.New(cfg).ListenAndServe(ctx)
				if err == http.ErrServerClosed {
					return nil
				}
				return err
			}

			ctx, cancel := ctx()
			defer cancel()
			snap, err := run.Live(ctx, optsFromFlags())
			if err != nil {
				return err
			}

			switch output {
			case "json":
				if file == "" {
					file = "sudiviz.json"
				}
				b, err := json.MarshalIndent(render.ExportCytoscape(snap.Graph), "", "  ")
				if err != nil {
					return err
				}
				if err := os.WriteFile(file, b, 0o644); err != nil {
					return err
				}
				fmt.Println("Wrote", file)
				return nil
			case "png", "svg":
				if file == "" {
					file = "sudiviz." + output
				}
				out, err := static.Export(snap.Graph, file)
				if err != nil {
					return err
				}
				fmt.Println("Wrote", out)
				if openAfter {
					_ = openFile(out)
				}
				return nil
			default:
				return fmt.Errorf("unknown --output value: %s (png|svg|json|web)", output)
			}
		},
	}
	cmd.Flags().String("output", "png", "png | svg | web | json")
	cmd.Flags().String("file", "sudiviz.png", "Output filename for png/svg/json")
	cmd.Flags().Bool("open", false, "Open output after generation")
	cmd.Flags().Int("port", 8000, "Port for web mode")
	cmd.Flags().String("host", "127.0.0.1", "Host for web mode")
	cmd.Flags().Float64("refresh-interval", 30, "Seconds between web refreshes")
	return cmd
}

func fixCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix [numbers]",
		Short: "Generate or apply remediation for diagnosed issues",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			apply, _ := cmd.Flags().GetBool("apply")
			force, _ := cmd.Flags().GetBool("force")
			issue, _ := cmd.Flags().GetString("issue")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if dryRun {
				apply = false
			}

			ctx, cancel := ctx()
			defer cancel()
			snap, err := run.Live(ctx, optsFromFlags())
			if err != nil {
				return err
			}
			if len(snap.Diagnosis.Fixes) == 0 {
				fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("No issues found — nothing to fix!"))
				return nil
			}
			actions := fix.Generate(snap.Diagnosis, snap.Graph, snap.Graph.Region, remediators.All())
			if issue != "" {
				var filtered []*fix.Action
				needle := strings.ToLower(issue)
				for _, a := range actions {
					if strings.Contains(strings.ToLower(a.Title), needle) {
						filtered = append(filtered, a)
					}
				}
				actions = filtered
				if len(actions) == 0 {
					fmt.Printf("No issues matching '%s'\n", issue)
					return nil
				}
			}

			spec := ""
			if len(args) > 0 {
				spec = args[0]
			}
			selected, err := fix.ParseSelection(spec, len(actions))
			if err != nil {
				return err
			}

			if jsonFlag(cmd) {
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
				return printJSON(payload)
			}

			fmt.Fprint(os.Stdout, version.Banner())
			if !apply {
				if selected != nil {
					fmt.Printf("Selected fix(es): %v\n\n", keys(selected))
				} else {
					fmt.Print("Proposed fixes (dry-run):\n\n")
				}
				for i, a := range actions {
					if selected != nil && !selected[i+1] {
						continue
					}
					tag := ""
					if a.IsDestructive {
						tag = " [DESTRUCTIVE]"
					}
					fmt.Printf("%d. %s %s%s\n", i+1, strings.ToUpper(a.Severity), a.Title, tag)
					fmt.Printf("   %s\n\n", a.Description)
					fmt.Printf("   %s\n\n", a.AWSCLICommand)
				}
				fmt.Println("Run with --apply to execute these fixes.")
				fmt.Println("Example: sudiviz fix 1 --apply")
				for _, a := range actions {
					if a.IsDestructive {
						fmt.Println("Destructive fixes require --apply --force.")
						break
					}
				}
				return nil
			}

			clients := fix.NewAWSClients(snap.Config)
			applied, skipped, failed := 0, 0, 0
			for i, a := range actions {
				if selected != nil && !selected[i+1] {
					continue
				}
				if a.IsDestructive && !force {
					fmt.Printf("⏭ Skipping %d. %s (destructive — use --force)\n", i+1, a.Title)
					skipped++
					continue
				}
				if !a.HasAutomatedFix() {
					fmt.Printf("⏭ Skipping %d. %s (no automated fix)\n", i+1, a.Title)
					skipped++
					continue
				}
				fmt.Printf("Applying %d. %s...\n", i+1, a.Title)
				fix.Apply(ctx, a, clients)
				if a.Applied {
					fmt.Printf("  ✓ Done: %s\n", a.Description)
					applied++
				} else if a.Error != "" {
					fmt.Printf("  ✗ Failed: %s\n", a.Error)
					failed++
				}
			}
			fmt.Printf("\nSummary: %d applied, %d skipped, %d failed\n", applied, skipped, failed)
			if failed > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().Bool("apply", false, "Actually apply the fixes (default: dry-run)")
	cmd.Flags().Bool("dry-run", false, "Preview fixes without applying (default)")
	cmd.Flags().Bool("force", false, "Apply destructive fixes (delete operations)")
	cmd.Flags().String("issue", "", "Only fix issues matching this string")
	cmd.Flags().Bool("json", false, "Output as JSON")
	return cmd
}

func tuiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive terminal UI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			refresh, _ := cmd.Flags().GetFloat64("refresh-interval")
			ctx, cancel := ctx()
			defer cancel()
			s := config.Load()
			return tui.Run(ctx, tui.Options{
				Profile: s.Profile, Region: s.Region, VPCID: s.VPCID, ServiceTag: s.ServiceTag,
				RefreshInterval: time.Duration(refresh * float64(time.Second)),
			})
		},
	}
	cmd.Flags().Float64("refresh-interval", 30, "Seconds between refreshes")
	return cmd
}

func explainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain [question]",
		Short: "AI-powered analysis of diagnostic findings via Amazon Bedrock (Nova Lite)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resource, _ := cmd.Flags().GetString("resource")
			ctx, cancel := ctx()
			defer cancel()
			fmt.Println("Running diagnostic engine…")
			snap, err := run.Live(ctx, optsFromFlags())
			if err != nil {
				return err
			}
			if len(snap.Diagnosis.Fixes) == 0 {
				fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("No issues found — nothing to explain!"))
				return nil
			}
			question := ""
			if len(args) > 0 {
				question = args[0]
			}
			if resource != "" {
				question = strings.TrimSpace(question + " Focus on resource " + resource)
			}
			fmt.Print("Analysing findings…\n\n")
			client := bedrockruntime.NewFromConfig(snap.Config)
			if err := explain.Stream(ctx, client, explain.Request{Diagnosis: snap.Diagnosis, Question: question}, os.Stdout); err != nil {
				return fmt.Errorf("failed to invoke Bedrock: %w\nMake sure AWS credentials are configured and Amazon Nova Lite model access is enabled in your region", err)
			}
			return nil
		},
	}
	cmd.Flags().String("resource", "", "Focus the explanation on this resource ARN")
	return cmd
}

func driftCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Compare Terraform state against live AWS and report drift",
		RunE: func(cmd *cobra.Command, _ []string) error {
			tfstate, _ := cmd.Flags().GetString("tfstate")
			if tfstate == "" {
				return fmt.Errorf("--tfstate is required")
			}
			if _, err := os.Stat(tfstate); err != nil {
				fmt.Fprintf(os.Stderr, "tfstate file not found: %s\n", tfstate)
				os.Exit(1)
			}
			state, err := drift.LoadState(tfstate)
			if err != nil {
				return err
			}
			intended := drift.ParseIntended(state)
			ctx, cancel := ctx()
			defer cancel()
			snap, err := run.Live(ctx, optsFromFlags())
			if err != nil {
				return err
			}
			findings := drift.Detect(intended, snap.Graph)
			if jsonFlag(cmd) {
				if err := printJSON(findings); err != nil {
					return err
				}
				if len(findings) > 0 {
					os.Exit(1)
				}
				return nil
			}
			if len(findings) == 0 {
				fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("No drift detected."))
				return nil
			}
			fmt.Printf("%d drift item(s):\n", len(findings))
			for _, f := range findings {
				fmt.Printf("  %s: %s\n", f.Kind, f.Message)
			}
			os.Exit(1)
			return nil
		},
	}
	cmd.Flags().String("tfstate", "", "Path to `terraform show -json` output")
	cmd.Flags().Bool("json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("tfstate")
	return cmd
}

func watchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Continuous monitoring — re-runs discovery every --interval seconds",
		RunE: func(cmd *cobra.Command, _ []string) error {
			interval, _ := cmd.Flags().GetInt("interval")
			fmt.Fprint(os.Stdout, version.Banner())
			for {
				clearScreen()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				snap, err := run.Live(ctx, optsFromFlags())
				cancel()
				if err != nil {
					fmt.Println("error:", err)
				} else {
					render.WriteTree(os.Stdout, snap.Graph)
					table.Write(os.Stdout, snap.Diagnosis.Fixes)
				}
				fmt.Printf("next refresh in %ds — Ctrl+C to stop\n", interval)
				time.Sleep(time.Duration(interval) * time.Second)
			}
		},
	}
	cmd.Flags().Int("interval", 30, "Seconds between refreshes")
	return cmd
}

func compareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compare",
		Short: "Diff a saved graph snapshot against the live topology",
		RunE: func(cmd *cobra.Command, _ []string) error {
			baseline, _ := cmd.Flags().GetString("baseline")
			if _, err := os.Stat(baseline); err != nil {
				fmt.Fprintf(os.Stderr, "baseline not found: %s\n", baseline)
				os.Exit(1)
			}
			raw, err := os.ReadFile(baseline)
			if err != nil {
				return err
			}
			var base map[string]any
			if err := json.Unmarshal(raw, &base); err != nil {
				return err
			}
			baseNodes := nodeIDs(base)
			ctx, cancel := ctx()
			defer cancel()
			snap, err := run.Live(ctx, optsFromFlags())
			if err != nil {
				return err
			}
			current := render.ExportCytoscape(snap.Graph)
			curNodes := map[string]bool{}
			for _, n := range current.Nodes {
				if id, _ := n.Data["id"].(string); id != "" {
					curNodes[id] = true
				}
			}
			var added, removed []string
			for id := range curNodes {
				if !baseNodes[id] {
					added = append(added, id)
				}
			}
			for id := range baseNodes {
				if !curNodes[id] {
					removed = append(removed, id)
				}
			}
			if jsonFlag(cmd) {
				return printJSON(map[string]any{"added": added, "removed": removed})
			}
			fmt.Printf("+ %d new, - %d removed\n", len(added), len(removed))
			for _, n := range added {
				fmt.Println("  +", n)
			}
			for _, n := range removed {
				fmt.Println("  -", n)
			}
			return nil
		},
	}
	cmd.Flags().String("baseline", "", "Path to a saved graph JSON")
	cmd.Flags().Bool("json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("baseline")
	return cmd
}

func shareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "share",
		Short: "Generate a JSON snapshot and optionally upload to transfer.sh",
		RunE: func(cmd *cobra.Command, _ []string) error {
			upload, _ := cmd.Flags().GetBool("upload")
			ctx, cancel := ctx()
			defer cancel()
			snap, err := run.Live(ctx, optsFromFlags())
			if err != nil {
				return err
			}
			payload, err := json.MarshalIndent(render.ExportCytoscape(snap.Graph), "", "  ")
			if err != nil {
				return err
			}
			tmp := filepath.Join(os.TempDir(), "sudiviz-share.json")
			if err := os.WriteFile(tmp, payload, 0o644); err != nil {
				return err
			}
			if !upload {
				fmt.Println("Snapshot saved to", tmp)
				return nil
			}
			fmt.Println("Local snapshot at", tmp)
			fmt.Println("Upload skipped: transfer.sh is unauthenticated public hosting — pass --no-upload and share the file yourself.")
			return nil
		},
	}
	cmd.Flags().Bool("upload", true, "Upload to transfer.sh")
	return cmd
}

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server (stdio transport)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return mcp.ServeStdio()
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and banner",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Fprint(os.Stdout, version.Banner())
		},
	}
}

func nodeIDs(payload map[string]any) map[string]bool {
	out := map[string]bool{}
	nodes, _ := payload["nodes"].([]any)
	for _, raw := range nodes {
		n, _ := raw.(map[string]any)
		if n == nil {
			continue
		}
		if id, _ := n["id"].(string); id != "" {
			out[id] = true
		}
		if data, _ := n["data"].(map[string]any); data != nil {
			if id, _ := data["id"].(string); id != "" {
				out[id] = true
			}
		}
	}
	return out
}

func keys(m map[int]bool) []int {
	var out []int
	for k := range m {
		out = append(out, k)
	}
	return out
}

func clearScreen() {
	if runtime.GOOS == "windows" {
		_ = exec.Command("cmd", "/c", "cls").Run()
		return
	}
	fmt.Print("\033[H\033[2J")
}

func openURL(url string) error { return openFile(url) }

func openFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

func speakDiagnosis(d *diagnose.Diagnosis) {
	if runtime.GOOS != "darwin" {
		fmt.Println("`--speak` only works on macOS.")
		return
	}
	summary := fmt.Sprintf("Sudiviz found %d issues. ", len(d.Fixes))
	for i, f := range d.Fixes {
		if i >= 3 {
			break
		}
		summary += f.Title + ". "
	}
	_ = exec.Command("say", summary).Run()
}
