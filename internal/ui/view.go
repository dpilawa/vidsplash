package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	doneStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	failStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	labelStyle   = lipgloss.NewStyle().Width(20)
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	boldStyle    = lipgloss.NewStyle().Bold(true)
	valueStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
)

func (m Model) View() string {
	if m.summary != nil {
		return m.summaryView()
	}

	var sb strings.Builder

	sb.WriteString(titleStyle.Render("vidsplash") + "  " + dimStyle.Render(strings.Repeat("─", 38)) + "\n\n")

	for i := StageID(0); i < stageCount; i++ {
		sb.WriteString(m.stageRow(i))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	return sb.String()
}

func (m Model) stageRow(id StageID) string {
	s := m.stages[id]
	label := labelStyle.Render(stageLabels[id])

	switch s.state {
	case stageDone:
		icon := doneStyle.Render("✓")
		elapsed := dimStyle.Render(fmt.Sprintf("%5.1fs", s.elapsed.Seconds()))
		return fmt.Sprintf("  %s  %s  %s", icon, label, elapsed)

	case stageFailed:
		icon := failStyle.Render("✗")
		return fmt.Sprintf("  %s  %s  %s", icon, label, failStyle.Render("failed"))

	case stageRunning:
		icon := m.spinner.View()
		var right string
		if m.currentPct > 0 {
			bar := m.progressBar.ViewAs(m.currentPct)
			meta := ""
			if m.currentSpd != "" {
				meta = dimStyle.Render("  " + m.currentSpd)
			}
			right = bar + meta
		}
		return fmt.Sprintf("  %s  %s  %s", runningStyle.Render(icon), label, right)

	default: // pending
		icon := pendingStyle.Render("·")
		return fmt.Sprintf("  %s  %s", icon, pendingStyle.Render(stageLabels[id]))
	}
}

func (m Model) summaryView() string {
	sum := m.summary
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("vidsplash") + "  " + dimStyle.Render(strings.Repeat("─", 38)) + "\n\n")

	for i := StageID(0); i < stageCount; i++ {
		s := m.stages[i]
		icon := doneStyle.Render("✓")
		label := labelStyle.Render(stageLabels[i])
		elapsed := dimStyle.Render(fmt.Sprintf("%5.1fs", s.elapsed.Seconds()))
		sb.WriteString(fmt.Sprintf("  %s  %s  %s\n", icon, label, elapsed))
	}

	sb.WriteString("\n")

	sb.WriteString(fmt.Sprintf("  %s  %s    %s  %s    %s  %s\n",
		boldStyle.Render("Input "), valueStyle.Render(formatDuration(sum.InputDuration)),
		boldStyle.Render("Splash"), valueStyle.Render(formatDuration(sum.SplashDuration)),
		boldStyle.Render("Output"), valueStyle.Render(formatDuration(sum.OutputDuration)),
	))
	sb.WriteString(fmt.Sprintf("  %s  %s   →  %s\n",
		boldStyle.Render("Size  "),
		valueStyle.Render(formatSize(sum.OutputSize)),
		dimStyle.Render(sum.OutputPath),
	))
	sb.WriteString("\n")

	return sb.String()
}

func formatDuration(secs float64) string {
	d := time.Duration(secs * float64(time.Second))
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
