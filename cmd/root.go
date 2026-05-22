package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/term"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/dpilawa/vidsplash/internal/concat"
	"github.com/dpilawa/vidsplash/internal/ffmpeg"
	"github.com/dpilawa/vidsplash/internal/probe"
	"github.com/dpilawa/vidsplash/internal/splash"
	"github.com/dpilawa/vidsplash/internal/ui"
)

var (
	flagPosition      string
	flagDuration      float64
	flagFadeOuter     float64 // fade on the edge facing away from the video
	flagFadeInner     float64 // fade on the edge facing towards the video
	flagBGColor       string
	flagVideoFadeIn   float64
	flagVideoFadeOut  float64
	flagNoAudioFade   bool
	flagOverwrite     bool
	flagVerbose       bool
	flagFFmpeg        string
	flagFFprobe       string
)

var RootCmd = &cobra.Command{
	Use:   "vidsplash [flags] INPUT_VIDEO SPLASH_IMAGE OUTPUT_VIDEO",
	Short: "Prepend or append a splash screen to a video file",
	Args:  cobra.ExactArgs(3),
	RunE:  run,
}

func init() {
	RootCmd.Flags().StringVarP(&flagPosition, "position", "p", "prepend", "prepend, append, or both")
	RootCmd.Flags().Float64VarP(&flagDuration, "duration", "d", 3.0, "Splash segment duration in seconds")
	RootCmd.Flags().Float64Var(&flagFadeOuter, "fade-outer", 0.5, "Fade duration on the outer edge of the splash (away from video); 0 = no fade")
	RootCmd.Flags().Float64Var(&flagFadeInner, "fade-inner", 0.5, "Fade duration on the inner edge of the splash (towards video); 0 = no fade")
	RootCmd.Flags().StringVarP(&flagBGColor, "bg-color", "b", "black", "Background color (any ffmpeg color string)")
	RootCmd.Flags().Float64Var(&flagVideoFadeIn, "video-fade-in", 0, "Fade-in duration for the main video (seconds, 0 = off)")
	RootCmd.Flags().Float64Var(&flagVideoFadeOut, "video-fade-out", 0, "Fade-out duration for the main video (seconds, 0 = off)")
	RootCmd.Flags().BoolVar(&flagNoAudioFade, "no-audio-fade", false, "Disable audio fade in/out on the main video")
	RootCmd.Flags().BoolVar(&flagOverwrite, "overwrite", false, "Overwrite output file if it exists")
	RootCmd.Flags().BoolVarP(&flagVerbose, "verbose", "v", false, "Stream raw ffmpeg output instead of TUI")
	RootCmd.Flags().StringVar(&flagFFmpeg, "ffmpeg", "ffmpeg", "Path to ffmpeg binary")
	RootCmd.Flags().StringVar(&flagFFprobe, "ffprobe", "ffprobe", "Path to ffprobe binary")
}

func run(cmd *cobra.Command, args []string) error {
	videoPath := args[0]
	imagePath := args[1]
	outputPath := args[2]

	if flagPosition != "prepend" && flagPosition != "append" && flagPosition != "both" {
		return fmt.Errorf("--position must be 'prepend', 'append', or 'both', got %q", flagPosition)
	}
	if flagFadeOuter+flagFadeInner > flagDuration {
		return fmt.Errorf("--fade-outer (%.2fs) + --fade-inner (%.2fs) exceeds --duration (%.2fs)", flagFadeOuter, flagFadeInner, flagDuration)
	}

	for _, path := range []string{videoPath, imagePath} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("input file not found: %q", path)
		}
	}

	outDir := filepath.Dir(outputPath)
	if _, err := os.Stat(outDir); err != nil {
		return fmt.Errorf("output directory does not exist: %q", outDir)
	}

	if !flagOverwrite {
		if _, err := os.Stat(outputPath); err == nil {
			return fmt.Errorf("output file already exists: %q (use --overwrite to replace)", outputPath)
		}
	}

	for _, bin := range []string{flagFFmpeg, flagFFprobe} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("binary not found: %q — install with: brew install ffmpeg", bin)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if flagVerbose || !isTTY() {
		return runVerbose(ctx, videoPath, imagePath, outputPath)
	}
	return runTUI(ctx, videoPath, imagePath, outputPath)
}

// splashOptsFor returns splash.Options for a segment at the given role.
//
//	role "start": outer edge faces the beginning of the output → fade-in=outer, fade-out=inner
//	role "end":   outer edge faces the end of the output      → fade-in=inner, fade-out=outer
//	role "only":  single splash (prepend or append alone)     → outer=outer, inner=inner
//	              prepend: fade-in=outer, fade-out=inner
//	              append:  fade-in=inner, fade-out=outer
func splashOptsFor(imagePath, outputPath, role string) splash.Options {
	var fadeIn, fadeOut float64
	switch role {
	case "start": // prepend in a "both" arrangement, or plain prepend
		fadeIn = flagFadeOuter
		fadeOut = flagFadeInner
	case "end": // append in a "both" arrangement, or plain append
		fadeIn = flagFadeInner
		fadeOut = flagFadeOuter
	}
	return splash.Options{
		ImagePath:  imagePath,
		OutputPath: outputPath,
		Duration:   flagDuration,
		FadeIn:     fadeIn,
		FadeOut:    fadeOut,
		BGColor:    flagBGColor,
		FFmpegPath: flagFFmpeg,
	}
}

// makeTempSplash creates a named temp file and returns its path.
func makeTempSplash() (string, func(), error) {
	f, err := os.CreateTemp("", "vidsplash-splash-*.mp4")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp file: %w", err)
	}
	path := f.Name()
	f.Close()
	return path, func() { os.Remove(path) }, nil
}

func runTUI(ctx context.Context, videoPath, imagePath, outputPath string) error {
	model := ui.New()
	prog := tea.NewProgram(model)

	var pipelineErr error
	go func() {
		pipelineErr = pipeline(ctx, prog, videoPath, imagePath, outputPath)
	}()

	if _, err := prog.Run(); err != nil {
		return err
	}
	return pipelineErr
}

func pipeline(ctx context.Context, prog *tea.Program, videoPath, imagePath, outputPath string) error {
	runner := &ffmpeg.ExecRunner{FFmpegPath: flagFFmpeg}

	// Stage 1: Probe
	prog.Send(ui.StageStartMsg{Stage: ui.StageProbe})
	t0 := time.Now()
	probeResult, err := probe.Run(ctx, flagFFprobe, videoPath)
	if err != nil {
		prog.Send(ui.StageErrorMsg{Stage: ui.StageProbe, Err: err})
		return err
	}
	prog.Send(ui.StageDoneMsg{Stage: ui.StageProbe, Elapsed: time.Since(t0)})

	// Stage 2: Build splash segment(s)
	prog.Send(ui.StageStartMsg{Stage: ui.StageSplash})
	t1 := time.Now()

	splashStartPath, cleanStart, err := makeTempSplash()
	if err != nil {
		return err
	}
	defer cleanStart()

	// For "both" we need a second segment with swapped inner/outer fades.
	splashEndPath := splashStartPath
	var cleanEnd func()
	if flagPosition == "both" {
		splashEndPath, cleanEnd, err = makeTempSplash()
		if err != nil {
			return err
		}
		defer cleanEnd()
	}

	// Build start splash (prepend or both-start)
	startRole := "start"
	if flagPosition == "append" {
		startRole = "end"
	}
	err = splash.Build(ctx, probeResult, splashOptsFor(imagePath, splashStartPath, startRole), runner, func(e ffmpeg.ProgressEvent) {
		prog.Send(ui.ProgressMsg{
			Stage:         ui.StageSplash,
			OutTimeUS:     e.OutTimeUS,
			TotalDuration: flagDuration,
			FPS:           e.FPS,
			Speed:         e.Speed,
		})
	})
	if err != nil {
		prog.Send(ui.StageErrorMsg{Stage: ui.StageSplash, Err: err})
		return err
	}

	// Build end splash if "both"
	if flagPosition == "both" {
		err = splash.Build(ctx, probeResult, splashOptsFor(imagePath, splashEndPath, "end"), runner, func(e ffmpeg.ProgressEvent) {
			prog.Send(ui.ProgressMsg{
				Stage:         ui.StageSplash,
				OutTimeUS:     e.OutTimeUS,
				TotalDuration: flagDuration,
				FPS:           e.FPS,
				Speed:         e.Speed,
			})
		})
		if err != nil {
			prog.Send(ui.StageErrorMsg{Stage: ui.StageSplash, Err: err})
			return err
		}
	}
	prog.Send(ui.StageDoneMsg{Stage: ui.StageSplash, Elapsed: time.Since(t1)})

	// Stage 3: Concatenate
	prog.Send(ui.StageStartMsg{Stage: ui.StageConcat})
	t2 := time.Now()

	splashCount := 1.0
	if flagPosition == "both" {
		splashCount = 2.0
	}

	concatOpts := concat.Options{
		SplashStartPath: splashStartPath,
		SplashEndPath:   splashEndPath,
		VideoPath:       videoPath,
		OutputPath:      outputPath,
		Position:        flagPosition,
		VideoFadeIn:     flagVideoFadeIn,
		VideoFadeOut:    flagVideoFadeOut,
		NoAudioFade:     flagNoAudioFade,
		VideoDuration:   probeResult.Duration,
		FFmpegPath:      flagFFmpeg,
		Overwrite:       flagOverwrite,
	}

	err = concat.Run(ctx, probeResult, concatOpts, runner, func(e ffmpeg.ProgressEvent) {
		prog.Send(ui.ProgressMsg{
			Stage:         ui.StageConcat,
			OutTimeUS:     e.OutTimeUS,
			TotalDuration: probeResult.Duration + splashCount*flagDuration,
			FPS:           e.FPS,
			Speed:         e.Speed,
		})
	})
	if err != nil {
		os.Remove(outputPath)
		prog.Send(ui.StageErrorMsg{Stage: ui.StageConcat, Err: err})
		return err
	}
	prog.Send(ui.StageDoneMsg{Stage: ui.StageConcat, Elapsed: time.Since(t2)})

	outInfo, _ := os.Stat(outputPath)
	var outSize int64
	if outInfo != nil {
		outSize = outInfo.Size()
	}
	outProbe, _ := probe.Run(ctx, flagFFprobe, outputPath)
	var outDur float64
	if outProbe != nil {
		outDur = outProbe.Duration
	}

	prog.Send(ui.SummaryMsg{
		InputDuration:  probeResult.Duration,
		SplashDuration: splashCount * flagDuration,
		OutputDuration: outDur,
		OutputSize:     outSize,
		OutputPath:     outputPath,
	})

	return nil
}

func isTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func runVerbose(ctx context.Context, videoPath, imagePath, outputPath string) error {
	runner := &ffmpeg.ExecRunner{FFmpegPath: flagFFmpeg}

	fmt.Fprintf(os.Stderr, "=== Probing video ===\n")
	probeResult, err := probe.Run(ctx, flagFFprobe, videoPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  %dx%d  %s fps  codec=%s  audio=%v\n\n",
		probeResult.Width, probeResult.Height, probeResult.FPS, probeResult.CodecName, probeResult.HasAudio)

	splashStartPath, cleanStart, err := makeTempSplash()
	if err != nil {
		return err
	}
	defer cleanStart()

	splashEndPath := splashStartPath
	var cleanEnd func()
	if flagPosition == "both" {
		splashEndPath, cleanEnd, err = makeTempSplash()
		if err != nil {
			return err
		}
		defer cleanEnd()
	}

	fmt.Fprintf(os.Stderr, "=== Building splash ===\n")
	startRole := "start"
	if flagPosition == "append" {
		startRole = "end"
	}
	if err := splash.Build(ctx, probeResult, splashOptsFor(imagePath, splashStartPath, startRole), runner, nil); err != nil {
		return err
	}
	if flagPosition == "both" {
		if err := splash.Build(ctx, probeResult, splashOptsFor(imagePath, splashEndPath, "end"), runner, nil); err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stderr, "\n=== Concatenating ===\n")
	concatOpts := concat.Options{
		SplashStartPath: splashStartPath,
		SplashEndPath:   splashEndPath,
		VideoPath:       videoPath,
		OutputPath:      outputPath,
		Position:        flagPosition,
		VideoFadeIn:     flagVideoFadeIn,
		VideoFadeOut:    flagVideoFadeOut,
		NoAudioFade:     flagNoAudioFade,
		VideoDuration:   probeResult.Duration,
		FFmpegPath:      flagFFmpeg,
		Overwrite:       flagOverwrite,
	}
	if err := concat.Run(ctx, probeResult, concatOpts, runner, nil); err != nil {
		os.Remove(outputPath)
		return err
	}

	fmt.Fprintf(os.Stderr, "\nDone → %s\n", outputPath)
	return nil
}
