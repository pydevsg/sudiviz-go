// Package table renders diagnostic findings as a terminal table.
package table

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
)

var (
	crit = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	warn = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	info = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	ok   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	head = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
)

// Write prints findings as a severity-sorted table. Empty findings print a
// green "No issues detected." panel matching the Python CLI.
func Write(w io.Writer, findings []diagnose.Finding) {
	if len(findings) == 0 {
		fmt.Fprintln(w, ok.Render("No issues detected."))
		return
	}
	fmt.Fprintln(w, head.Render("Diagnosis"))
	fmt.Fprintf(w, "%-12s  %-56s  %s\n", "SEVERITY", "TITLE", "DETAIL")
	fmt.Fprintln(w, strings.Repeat("─", 110))
	for _, f := range findings {
		fmt.Fprintf(w, "%-20s  %-56s  %s\n",
			paint(string(f.Severity)), clip(f.Title, 56), clip(f.Detail, 90))
	}
}

func paint(sev string) string {
	s := strings.ToUpper(sev)
	switch sev {
	case "critical":
		return crit.Render(s)
	case "warning":
		return warn.Render(s)
	case "info":
		return info.Render(s)
	default:
		return s
	}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
