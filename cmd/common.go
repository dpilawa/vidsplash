package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dpilawa/vidsplash/internal/ui"
)

// Persistent flags shared by every subcommand.
var (
	flagOverwrite bool
	flagVerbose   bool
	flagFFmpeg    string
	flagFFprobe   string
)

func registerPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVar(&flagOverwrite, "overwrite", false, "Overwrite output file(s) if they exist")
	cmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Stream raw ffmpeg output instead of TUI")
	cmd.PersistentFlags().StringVar(&flagFFmpeg, "ffmpeg", "ffmpeg", "Path to ffmpeg binary")
	cmd.PersistentFlags().StringVar(&flagFFprobe, "ffprobe", "ffprobe", "Path to ffprobe binary")
}

func newSignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func isTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// checkOutput validates the output path's directory exists and, unless
// --overwrite was given, that the file doesn't already exist.
func checkOutput(outputPath string) error {
	outDir := "."
	if dir := dirOf(outputPath); dir != "" {
		outDir = dir
	}
	if _, err := os.Stat(outDir); err != nil {
		return fmt.Errorf("output directory does not exist: %q", outDir)
	}
	if !flagOverwrite {
		if _, err := os.Stat(outputPath); err == nil {
			return fmt.Errorf("output file already exists: %q (use --overwrite to replace)", outputPath)
		}
	}
	return nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return ""
}

// checkBinaries verifies the configured ffmpeg/ffprobe binaries are on PATH.
func checkBinaries() error {
	for _, bin := range []string{flagFFmpeg, flagFFprobe} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("binary not found: %q — install with: brew install ffmpeg", bin)
		}
	}
	return nil
}

// checkInputs verifies every given path exists.
func checkInputs(paths ...string) error {
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("input file not found: %q", path)
		}
	}
	return nil
}

// makeTempFile creates a named temp file with the given extension and
// returns its path plus a cleanup func that removes it.
func makeTempFile(ext string) (string, func(), error) {
	f, err := os.CreateTemp("", "vidsplash-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("creating temp file: %w", err)
	}
	path := f.Name()
	f.Close()
	return path, func() { os.Remove(path) }, nil
}

// Reporter is how a verb's pipeline reports stage progress, independent of
// whether the run is rendered as a TUI or streamed verbosely.
type Reporter interface {
	StageStart(stage int)
	StageDone(stage int, elapsed time.Duration)
	StageError(stage int, err error)
	Progress(stage int, outTimeUS int64, totalDuration float64, fps float64, speed string)
	Summary(stats []ui.StatItem, outputSize int64, outputPath string)
}

// runStages drives a verb's pipeline, choosing between the bubbletea TUI and
// a plain verbose/stderr renderer based on --verbose and TTY detection.
func runStages(ctx context.Context, title string, labels []string, fn func(ctx context.Context, r Reporter) error) error {
	if flagVerbose || !isTTY() {
		return fn(ctx, &verboseReporter{labels: labels})
	}

	model := ui.New(title, labels)
	prog := tea.NewProgram(model)
	r := &tuiReporter{prog: prog}

	var pipelineErr error
	go func() {
		pipelineErr = fn(ctx, r)
	}()

	if _, err := prog.Run(); err != nil {
		return err
	}
	return pipelineErr
}

type tuiReporter struct {
	prog *tea.Program
}

func (r *tuiReporter) StageStart(stage int) {
	r.prog.Send(ui.StageStartMsg{Stage: ui.StageID(stage)})
}
func (r *tuiReporter) StageDone(stage int, elapsed time.Duration) {
	r.prog.Send(ui.StageDoneMsg{Stage: ui.StageID(stage), Elapsed: elapsed})
}
func (r *tuiReporter) StageError(stage int, err error) {
	r.prog.Send(ui.StageErrorMsg{Stage: ui.StageID(stage), Err: err})
}
func (r *tuiReporter) Progress(stage int, outTimeUS int64, totalDuration float64, fps float64, speed string) {
	r.prog.Send(ui.ProgressMsg{
		Stage:         ui.StageID(stage),
		OutTimeUS:     outTimeUS,
		TotalDuration: totalDuration,
		FPS:           fps,
		Speed:         speed,
	})
}
func (r *tuiReporter) Summary(stats []ui.StatItem, outputSize int64, outputPath string) {
	r.prog.Send(ui.SummaryMsg{Stats: stats, OutputSize: outputSize, OutputPath: outputPath})
}

type verboseReporter struct {
	labels []string
	starts []time.Time
}

func (r *verboseReporter) StageStart(stage int) {
	if r.starts == nil {
		r.starts = make([]time.Time, len(r.labels))
	}
	r.starts[stage] = time.Now()
	fmt.Fprintf(os.Stderr, "=== %s ===\n", r.labels[stage])
}
func (r *verboseReporter) StageDone(stage int, elapsed time.Duration) {
	fmt.Fprintf(os.Stderr, "  done in %.1fs\n\n", elapsed.Seconds())
}
func (r *verboseReporter) StageError(stage int, err error) {
	fmt.Fprintf(os.Stderr, "  failed: %v\n", err)
}
func (r *verboseReporter) Progress(stage int, outTimeUS int64, totalDuration float64, fps float64, speed string) {
	// Verbose mode only prints stage headers/results; per-frame progress is omitted.
}
func (r *verboseReporter) Summary(stats []ui.StatItem, outputSize int64, outputPath string) {
	for _, s := range stats {
		fmt.Fprintf(os.Stderr, "%s: %s\n", s.Label, s.Value)
	}
	fmt.Fprintf(os.Stderr, "Size: %s\n", ui.FormatSize(outputSize))
	fmt.Fprintf(os.Stderr, "Done → %s\n", outputPath)
}
