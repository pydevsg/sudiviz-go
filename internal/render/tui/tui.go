// Package tui is the Bubble Tea interactive terminal UI.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
	"github.com/pydevsg/sudiviz-go/internal/graph"
	"github.com/pydevsg/sudiviz-go/internal/run"
	"github.com/pydevsg/sudiviz-go/internal/version"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("4"))
	critStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	infoStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	panelStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	statusStyle = lipgloss.NewStyle().Background(lipgloss.Color("4")).Foreground(lipgloss.Color("15")).Padding(0, 1)
)

// Options configure a TUI session.
type Options struct {
	Profile         string
	Region          string
	VPCID           string
	ServiceTag      string
	RefreshInterval time.Duration
}

type loadedMsg struct {
	snap *run.Snapshot
	err  error
}

type model struct {
	opts        Options
	snap        *run.Snapshot
	err         string
	status      string
	resources   []*graph.Resource
	cursor      int
	orphanOnly  bool
	width       int
	height      int
	lastRefresh time.Time
}

// Run starts the Bubble Tea TUI (blocking).
func Run(ctx context.Context, opts Options) error {
	if opts.RefreshInterval <= 0 {
		opts.RefreshInterval = 30 * time.Second
	}
	p := tea.NewProgram(newModel(opts), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

func newModel(opts Options) model {
	return model{opts: opts, status: "Discovering…"}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(loadCmd(m.opts), tickCmd(m.opts.RefreshInterval))
}

func loadCmd(opts Options) tea.Cmd {
	return func() tea.Msg {
		snap, err := run.Live(context.Background(), run.Options{
			Profile: opts.Profile, Region: opts.Region, VPCID: opts.VPCID, ServiceTag: opts.ServiceTag,
		})
		return loadedMsg{snap: snap, err: err}
	}
}

type tickMsg time.Time

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case loadedMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			m.status = "error"
		} else {
			m.snap = msg.snap
			m.err = ""
			m.lastRefresh = time.Now()
			m.status = fmt.Sprintf("account %s  region %s", msg.snap.Graph.AccountID, msg.snap.Graph.Region)
			m.rebuild()
		}
	case tickMsg:
		m.status = "Refreshing…"
		return m, tea.Batch(loadCmd(m.opts), tickCmd(m.opts.RefreshInterval))
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.status = "Refreshing…"
			return m, loadCmd(m.opts)
		case "o":
			m.orphanOnly = !m.orphanOnly
			m.rebuild()
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.resources)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m *model) rebuild() {
	m.resources = nil
	if m.snap == nil {
		return
	}
	for _, r := range m.snap.Graph.Resources() {
		if m.orphanOnly && !r.Orphan {
			continue
		}
		m.resources = append(m.resources, r)
	}
	if m.cursor >= len(m.resources) {
		m.cursor = max(0, len(m.resources)-1)
	}
}

func (m model) View() string {
	header := titleStyle.Render(fmt.Sprintf("sudiviz v%s  TUI", version.Version))
	status := statusStyle.Render(m.status)
	if m.orphanOnly {
		status += "  " + critStyle.Render("[orphans only]")
	}
	if m.err != "" {
		return header + "\n" + critStyle.Render(m.err) + "\n" + mutedStyle.Render("q quit  r refresh")
	}

	left := m.resourcePane()
	right := m.detailPane()
	bottom := m.findingsPane()

	help := mutedStyle.Render("q quit  r refresh  o toggle orphans  j/k move")
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, header, status, body, bottom, help)
}

func (m model) resourcePane() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Resources") + "\n")
	if len(m.resources) == 0 {
		b.WriteString(mutedStyle.Render("  (none)") + "\n")
	}
	maxRows := 20
	if m.height > 16 {
		maxRows = m.height - 16
	}
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := min(len(m.resources), start+maxRows)
	for i := start; i < end; i++ {
		r := m.resources[i]
		line := fmt.Sprintf(" %-14s  %-28s  %-10s", r.Kind, clip(r.Label, 28), r.Health)
		if r.Orphan {
			line += "  ORPHAN"
		}
		if i == m.cursor {
			b.WriteString(selStyle.Render(line) + "\n")
		} else if r.Orphan || r.Health == graph.HealthUnhealthy {
			b.WriteString(critStyle.Render(line) + "\n")
		} else {
			b.WriteString(line + "\n")
		}
	}
	w := 62
	if m.width > 120 {
		w = m.width / 2
	}
	return panelStyle.Width(w).Render(b.String())
}

func (m model) detailPane() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Details") + "\n")
	if m.cursor < 0 || m.cursor >= len(m.resources) {
		b.WriteString(mutedStyle.Render("Select a resource.") + "\n")
	} else {
		r := m.resources[m.cursor]
		fmt.Fprintf(&b, "Kind:    %s\n", r.Kind)
		fmt.Fprintf(&b, "Label:   %s\n", r.Label)
		fmt.Fprintf(&b, "Health:  %s\n", r.Health)
		fmt.Fprintf(&b, "Orphan:  %v\n", r.Orphan)
		fmt.Fprintf(&b, "Cost:    ~$%.0f/mo\n", r.MonthlyCost)
		fmt.Fprintf(&b, "ID:      %s\n", r.ID)
		if r.Region != "" {
			fmt.Fprintf(&b, "Region:  %s\n", r.Region)
		}
		if r.AZ != "" {
			fmt.Fprintf(&b, "AZ:      %s\n", r.AZ)
		}
		for k, v := range r.Attrs {
			if k == "rules" || k == "targets" {
				continue
			}
			fmt.Fprintf(&b, "%s: %v\n", k, v)
		}
	}
	w := 50
	if m.width > 120 {
		w = m.width / 2
	}
	return panelStyle.Width(w).Render(b.String())
}

func (m model) findingsPane() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Findings") + "\n")
	if m.snap == nil || len(m.snap.Diagnosis.Fixes) == 0 {
		b.WriteString(okStyle.Render("No issues detected.") + "\n")
	} else {
		for _, f := range m.snap.Diagnosis.Fixes {
			b.WriteString(paintSev(f.Severity).Render(strings.ToUpper(string(f.Severity))) + "  " + f.Title + "\n")
		}
	}
	w := m.width
	if w < 40 {
		w = 80
	}
	return panelStyle.Width(w - 2).Render(b.String())
}

func paintSev(s diagnose.Severity) lipgloss.Style {
	switch s {
	case diagnose.SeverityCritical:
		return critStyle
	case diagnose.SeverityWarning:
		return warnStyle
	default:
		return infoStyle
	}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
