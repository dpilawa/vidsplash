package ui

import "time"

// StageID indexes into the stage list a verb registers via New.
type StageID int

type StageStartMsg struct{ Stage StageID }
type StageDoneMsg struct {
	Stage   StageID
	Elapsed time.Duration
}
type StageErrorMsg struct {
	Stage StageID
	Err   error
}
type ProgressMsg struct {
	Stage         StageID
	OutTimeUS     int64
	TotalDuration float64 // seconds
	FPS           float64
	Speed         string
}

// StatItem is a single labeled value shown in the completion summary.
type StatItem struct {
	Label string
	Value string
}

type SummaryMsg struct {
	Stats      []StatItem
	OutputSize int64
	OutputPath string
}
type tickMsg time.Time
