package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dpilawa/vidsplash/internal/concat"
	"github.com/dpilawa/vidsplash/internal/ffmpeg"
	"github.com/dpilawa/vidsplash/internal/probe"
	"github.com/dpilawa/vidsplash/internal/splash"
	"github.com/dpilawa/vidsplash/internal/ui"
)

var (
	flagPosition     string
	flagDuration     float64
	flagFadeOuter    float64 // fade on the edge facing away from the video
	flagFadeInner    float64 // fade on the edge facing towards the video
	flagBGColor      string
	flagVideoFadeIn  float64
	flagVideoFadeOut float64
	flagNoAudioFade  bool
)

var splashCmd = &cobra.Command{
	Use:   "splash INPUT_VIDEO SPLASH_IMAGE OUTPUT_VIDEO",
	Short: "Prepend or append a splash screen to a video file",
	Args:  cobra.ExactArgs(3),
	RunE:  runSplash,
}

const (
	splashStageProbe  = 0
	splashStageSplash = 1
	splashStageConcat = 2
)

var splashStageLabels = []string{"Probing video", "Building splash", "Concatenating"}

func init() {
	splashCmd.Flags().StringVarP(&flagPosition, "position", "p", "prepend", "prepend, append, or both")
	splashCmd.Flags().Float64VarP(&flagDuration, "duration", "d", 3.0, "Splash segment duration in seconds")
	splashCmd.Flags().Float64Var(&flagFadeOuter, "fade-outer", 0.5, "Fade duration on the outer edge of the splash (away from video); 0 = no fade")
	splashCmd.Flags().Float64Var(&flagFadeInner, "fade-inner", 0.5, "Fade duration on the inner edge of the splash (towards video); 0 = no fade")
	splashCmd.Flags().StringVarP(&flagBGColor, "bg-color", "b", "black", "Background color (any ffmpeg color string)")
	splashCmd.Flags().Float64Var(&flagVideoFadeIn, "video-fade-in", 0, "Fade-in duration for the main video (seconds, 0 = off)")
	splashCmd.Flags().Float64Var(&flagVideoFadeOut, "video-fade-out", 0, "Fade-out duration for the main video (seconds, 0 = off)")
	splashCmd.Flags().BoolVar(&flagNoAudioFade, "no-audio-fade", false, "Disable audio fade in/out on the main video")
}

func runSplash(cmd *cobra.Command, args []string) error {
	videoPath := args[0]
	imagePath := args[1]
	outputPath := args[2]

	if flagPosition != "prepend" && flagPosition != "append" && flagPosition != "both" {
		return fmt.Errorf("--position must be 'prepend', 'append', or 'both', got %q", flagPosition)
	}
	if flagFadeOuter+flagFadeInner > flagDuration {
		return fmt.Errorf("--fade-outer (%.2fs) + --fade-inner (%.2fs) exceeds --duration (%.2fs)", flagFadeOuter, flagFadeInner, flagDuration)
	}
	if err := checkInputs(videoPath, imagePath); err != nil {
		return err
	}
	if err := checkOutput(outputPath); err != nil {
		return err
	}
	if err := checkBinaries(); err != nil {
		return err
	}

	ctx, cancel := newSignalContext()
	defer cancel()

	return runStages(ctx, "vidsplash splash", splashStageLabels, func(ctx context.Context, r Reporter) error {
		return splashPipeline(ctx, r, videoPath, imagePath, outputPath)
	})
}

// splashOptsFor returns splash.Options for a segment at the given role.
//
//	role "start": outer edge faces the beginning of the output → fade-in=outer, fade-out=inner
//	role "end":   outer edge faces the end of the output      → fade-in=inner, fade-out=outer
func splashOptsFor(imagePath, outputPath, role string) splash.Options {
	var fadeIn, fadeOut float64
	switch role {
	case "start":
		fadeIn = flagFadeOuter
		fadeOut = flagFadeInner
	case "end":
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

func splashPipeline(ctx context.Context, r Reporter, videoPath, imagePath, outputPath string) error {
	runner := &ffmpeg.ExecRunner{FFmpegPath: flagFFmpeg}

	r.StageStart(splashStageProbe)
	t0 := time.Now()
	probeResult, err := probe.Run(ctx, flagFFprobe, videoPath)
	if err != nil {
		r.StageError(splashStageProbe, err)
		return err
	}
	r.StageDone(splashStageProbe, time.Since(t0))

	r.StageStart(splashStageSplash)
	t1 := time.Now()

	splashStartPath, cleanStart, err := makeTempFile(".mp4")
	if err != nil {
		return err
	}
	defer cleanStart()

	splashEndPath := splashStartPath
	var cleanEnd func()
	if flagPosition == "both" {
		splashEndPath, cleanEnd, err = makeTempFile(".mp4")
		if err != nil {
			return err
		}
		defer cleanEnd()
	}

	startRole := "start"
	if flagPosition == "append" {
		startRole = "end"
	}
	err = splash.Build(ctx, probeResult, splashOptsFor(imagePath, splashStartPath, startRole), runner, func(e ffmpeg.ProgressEvent) {
		r.Progress(splashStageSplash, e.OutTimeUS, flagDuration, e.FPS, e.Speed)
	})
	if err != nil {
		r.StageError(splashStageSplash, err)
		return err
	}

	if flagPosition == "both" {
		err = splash.Build(ctx, probeResult, splashOptsFor(imagePath, splashEndPath, "end"), runner, func(e ffmpeg.ProgressEvent) {
			r.Progress(splashStageSplash, e.OutTimeUS, flagDuration, e.FPS, e.Speed)
		})
		if err != nil {
			r.StageError(splashStageSplash, err)
			return err
		}
	}
	r.StageDone(splashStageSplash, time.Since(t1))

	r.StageStart(splashStageConcat)
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
		r.Progress(splashStageConcat, e.OutTimeUS, probeResult.Duration+splashCount*flagDuration, e.FPS, e.Speed)
	})
	if err != nil {
		os.Remove(outputPath)
		r.StageError(splashStageConcat, err)
		return err
	}
	r.StageDone(splashStageConcat, time.Since(t2))

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

	r.Summary([]ui.StatItem{
		{Label: "Input ", Value: ui.FormatDuration(probeResult.Duration)},
		{Label: "Splash", Value: ui.FormatDuration(splashCount * flagDuration)},
		{Label: "Output", Value: ui.FormatDuration(outDur)},
	}, outSize, outputPath)

	return nil
}
