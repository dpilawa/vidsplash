package ui

import "time"

type StageID int

const (
	StageProbe   StageID = 0
	StageSplash  StageID = 1
	StageConcat  StageID = 2
	stageCount           = 3
)

var stageLabels = [stageCount]string{
	"Probing video",
	"Building splash",
	"Concatenating",
}

type StageStartMsg struct{ Stage StageID }
type StageDoneMsg struct {
	Stage    StageID
	Elapsed  time.Duration
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
type SummaryMsg struct {
	InputDuration  float64
	SplashDuration float64
	OutputDuration float64
	OutputSize     int64
	OutputPath     string
}
type tickMsg time.Time
