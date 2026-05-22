package ui

import (
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

type stageState int

const (
	stagePending stageState = iota
	stageRunning
	stageDone
	stageFailed
)

type stageData struct {
	state   stageState
	elapsed time.Duration
	startAt time.Time
}

// Model is the Bubbletea model for the vidsplash TUI.
type Model struct {
	stages      [stageCount]stageData
	spinner     spinner.Model
	progressBar progress.Model
	currentPct  float64
	currentFPS  string
	currentSpd  string
	activeStage StageID
	summary     *SummaryMsg
	err         error
}

// New creates a fresh Model.
func New() Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(30),
		progress.WithoutPercentage(),
	)

	return Model{
		spinner:     s,
		progressBar: p,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
