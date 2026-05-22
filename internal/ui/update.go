package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	case StageStartMsg:
		m.stages[msg.Stage].state = stageRunning
		m.stages[msg.Stage].startAt = time.Now()
		m.activeStage = msg.Stage
		m.currentPct = 0
		m.currentFPS = ""
		m.currentSpd = ""

	case StageDoneMsg:
		m.stages[msg.Stage].state = stageDone
		m.stages[msg.Stage].elapsed = msg.Elapsed

	case StageErrorMsg:
		m.stages[msg.Stage].state = stageFailed
		m.err = msg.Err
		return m, tea.Quit

	case ProgressMsg:
		if msg.TotalDuration > 0 {
			secs := float64(msg.OutTimeUS) / 1e6
			m.currentPct = secs / msg.TotalDuration
			if m.currentPct > 1 {
				m.currentPct = 1
			}
		}
		if msg.FPS > 0 {
			m.currentFPS = fmt.Sprintf("%.0ffps", msg.FPS)
		}
		if msg.Speed != "" && msg.Speed != "N/A" {
			m.currentSpd = msg.Speed
		}
		pbCmd := m.progressBar.SetPercent(m.currentPct)
		return m, pbCmd

	case SummaryMsg:
		m.summary = &msg
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tickMsg:
		return m, tickCmd()
	}

	return m, nil
}
