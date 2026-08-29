package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dpilawa/vidsplash/internal/concat"
	"github.com/dpilawa/vidsplash/internal/ffmpeg"
	"github.com/dpilawa/vidsplash/internal/probe"
	"github.com/dpilawa/vidsplash/internal/splash"
	"github.com/dpilawa/vidsplash/internal/ui"
)

var (
	flagConcatOutput   string
	flagConcatConfig   string
	flagConcatImageDur float64
	flagConcatBGColor  string
	flagConcatWidth    int
	flagConcatHeight   int
	flagConcatFPS      string
	flagConcatNoAudio  bool
)

var concatCmd = &cobra.Command{
	Use:   "concat [FILE...]",
	Short: "Combine multiple videos and images into one output",
	Long: "Combine multiple videos and images into one output.\n\n" +
		"Pass files positionally for the simple case (images use --image-duration),\n" +
		"or use --config for per-item control over fades, background color, and target format.",
	Args: cobra.ArbitraryArgs,
	RunE: runConcat,
}

func init() {
	concatCmd.Flags().StringVarP(&flagConcatOutput, "output", "o", "", "Output video path (required)")
	concatCmd.Flags().StringVarP(&flagConcatConfig, "config", "c", "", "JSON config file describing items (alternative to positional args)")
	concatCmd.Flags().Float64Var(&flagConcatImageDur, "image-duration", 3.0, "Duration for image items given positionally (seconds)")
	concatCmd.Flags().StringVarP(&flagConcatBGColor, "bg-color", "b", "black", "Default background/pad color (any ffmpeg color string)")
	concatCmd.Flags().IntVar(&flagConcatWidth, "width", 0, "Target width (defaults to the first video item's width)")
	concatCmd.Flags().IntVar(&flagConcatHeight, "height", 0, "Target height (defaults to the first video item's height)")
	concatCmd.Flags().StringVar(&flagConcatFPS, "fps", "", "Target frame rate, e.g. 30/1 (defaults to the first video item's fps)")
	concatCmd.Flags().BoolVar(&flagConcatNoAudio, "no-audio", false, "Strip audio from the output")
}

// concatItem is the internal, resolved representation of one thing to
// concatenate, whether it came from positional args or a JSON config.
type concatItem struct {
	isImage  bool
	path     string
	duration float64 // image items only
	bgColor  string
	fadeIn   float64
	fadeOut  float64
}

type concatConfigFile struct {
	Target *concatConfigTarget `json:"target,omitempty"`
	Items  []concatConfigItem  `json:"items"`
}

type concatConfigTarget struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	FPS    string `json:"fps"`
}

type concatConfigItem struct {
	Video    string  `json:"video,omitempty"`
	Image    string  `json:"image,omitempty"`
	Duration float64 `json:"duration,omitempty"`
	BGColor  string  `json:"bg_color,omitempty"`
	FadeIn   float64 `json:"fade_in,omitempty"`
	FadeOut  float64 `json:"fade_out,omitempty"`
}

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".bmp": true, ".webp": true, ".gif": true, ".tiff": true,
}

func isImagePath(path string) bool {
	return imageExts[strings.ToLower(filepath.Ext(path))]
}

const (
	concatStageProbe     = 0
	concatStageNormalize = 1
	concatStageJoin      = 2
)

var concatStageLabels = []string{"Probing clips", "Normalizing clips", "Concatenating"}

func runConcat(cmd *cobra.Command, args []string) error {
	if flagConcatOutput == "" {
		return fmt.Errorf("--output is required")
	}
	if flagConcatConfig != "" && len(args) > 0 {
		return fmt.Errorf("cannot combine --config with positional file arguments")
	}

	var items []concatItem
	var targetOverride *concatConfigTarget

	if flagConcatConfig != "" {
		cfg, err := loadConcatConfig(flagConcatConfig)
		if err != nil {
			return err
		}
		targetOverride = cfg.Target
		for _, ci := range cfg.Items {
			item, err := resolveConfigItem(ci)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
	} else {
		if len(args) == 0 {
			return fmt.Errorf("no input files given: pass files positionally or use --config")
		}
		for _, path := range args {
			items = append(items, concatItem{
				isImage:  isImagePath(path),
				path:     path,
				duration: flagConcatImageDur,
				bgColor:  flagConcatBGColor,
			})
		}
	}

	if flagConcatWidth > 0 {
		if targetOverride == nil {
			targetOverride = &concatConfigTarget{}
		}
		targetOverride.Width = flagConcatWidth
	}
	if flagConcatHeight > 0 {
		if targetOverride == nil {
			targetOverride = &concatConfigTarget{}
		}
		targetOverride.Height = flagConcatHeight
	}
	if flagConcatFPS != "" {
		if targetOverride == nil {
			targetOverride = &concatConfigTarget{}
		}
		targetOverride.FPS = flagConcatFPS
	}

	if len(items) < 2 {
		return fmt.Errorf("need at least 2 items to concatenate, got %d", len(items))
	}

	paths := make([]string, len(items))
	for i, it := range items {
		paths[i] = it.path
	}
	if err := checkInputs(paths...); err != nil {
		return err
	}
	if err := checkOutput(flagConcatOutput); err != nil {
		return err
	}
	if err := checkBinaries(); err != nil {
		return err
	}

	ctx, cancel := newSignalContext()
	defer cancel()

	return runStages(ctx, "vidsplash concat", concatStageLabels, func(ctx context.Context, r Reporter) error {
		return concatPipeline(ctx, r, items, targetOverride, flagConcatOutput)
	})
}

func loadConcatConfig(path string) (*concatConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg concatConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if len(cfg.Items) == 0 {
		return nil, fmt.Errorf("config has no items")
	}
	return &cfg, nil
}

func resolveConfigItem(ci concatConfigItem) (concatItem, error) {
	switch {
	case ci.Video != "" && ci.Image != "":
		return concatItem{}, fmt.Errorf("item has both 'video' and 'image': %+v", ci)
	case ci.Video != "":
		return concatItem{isImage: false, path: ci.Video, bgColor: ci.BGColor, fadeIn: ci.FadeIn, fadeOut: ci.FadeOut}, nil
	case ci.Image != "":
		if ci.Duration <= 0 {
			return concatItem{}, fmt.Errorf("image item %q needs a positive 'duration'", ci.Image)
		}
		bg := ci.BGColor
		if bg == "" {
			bg = flagConcatBGColor
		}
		return concatItem{isImage: true, path: ci.Image, duration: ci.Duration, bgColor: bg, fadeIn: ci.FadeIn, fadeOut: ci.FadeOut}, nil
	default:
		return concatItem{}, fmt.Errorf("item must have a 'video' or 'image' key: %+v", ci)
	}
}

func concatPipeline(ctx context.Context, r Reporter, items []concatItem, targetOverride *concatConfigTarget, outputPath string) error {
	runner := &ffmpeg.ExecRunner{FFmpegPath: flagFFmpeg}

	r.StageStart(concatStageProbe)
	t0 := time.Now()

	probes := make([]*probe.Result, len(items))
	for i, it := range items {
		if it.isImage {
			continue
		}
		p, err := probe.Run(ctx, flagFFprobe, it.path)
		if err != nil {
			r.StageError(concatStageProbe, fmt.Errorf("probing %q: %w", it.path, err))
			return err
		}
		probes[i] = p
	}

	target, err := resolveConcatTarget(items, probes, targetOverride)
	if err != nil {
		r.StageError(concatStageProbe, err)
		return err
	}
	if flagConcatNoAudio {
		target.HasAudio = false
	}
	r.StageDone(concatStageProbe, time.Since(t0))

	r.StageStart(concatStageNormalize)
	t1 := time.Now()

	segmentPaths := make([]string, len(items))
	var cleanups []func()
	defer func() {
		for _, c := range cleanups {
			c()
		}
	}()

	var totalDuration float64
	for i, it := range items {
		segPath, cleanup, err := makeTempFile(".mp4")
		if err != nil {
			r.StageError(concatStageNormalize, err)
			return err
		}
		cleanups = append(cleanups, cleanup)
		segmentPaths[i] = segPath

		if it.isImage {
			opts := splash.Options{
				ImagePath:  it.path,
				OutputPath: segPath,
				Duration:   it.duration,
				FadeIn:     it.fadeIn,
				FadeOut:    it.fadeOut,
				BGColor:    it.bgColor,
				FFmpegPath: flagFFmpeg,
			}
			err = splash.Build(ctx, target, opts, runner, func(e ffmpeg.ProgressEvent) {
				r.Progress(concatStageNormalize, e.OutTimeUS, it.duration, e.FPS, e.Speed)
			})
			totalDuration += it.duration
		} else {
			p := probes[i]
			opts := concat.NormalizeVideoOptions{
				VideoPath:  it.path,
				OutputPath: segPath,
				Duration:   p.Duration,
				HasAudio:   p.HasAudio,
				FadeIn:     it.fadeIn,
				FadeOut:    it.fadeOut,
				BGColor:    bgColorOrDefault(it.bgColor),
				FFmpegPath: flagFFmpeg,
			}
			err = concat.NormalizeVideo(ctx, target, opts, runner, func(e ffmpeg.ProgressEvent) {
				r.Progress(concatStageNormalize, e.OutTimeUS, p.Duration, e.FPS, e.Speed)
			})
			totalDuration += p.Duration
		}
		if err != nil {
			r.StageError(concatStageNormalize, fmt.Errorf("normalizing %q: %w", it.path, err))
			return err
		}
	}
	r.StageDone(concatStageNormalize, time.Since(t1))

	r.StageStart(concatStageJoin)
	t2 := time.Now()

	err = concat.RunDemuxer(ctx, segmentPaths, outputPath, flagOverwrite, runner, func(e ffmpeg.ProgressEvent) {
		r.Progress(concatStageJoin, e.OutTimeUS, totalDuration, e.FPS, e.Speed)
	})
	if err != nil {
		os.Remove(outputPath)
		r.StageError(concatStageJoin, err)
		return err
	}
	r.StageDone(concatStageJoin, time.Since(t2))

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
		{Label: "Items ", Value: fmt.Sprintf("%d", len(items))},
		{Label: "Output", Value: ui.FormatDuration(outDur)},
	}, outSize, outputPath)

	return nil
}

func bgColorOrDefault(c string) string {
	if c == "" {
		return flagConcatBGColor
	}
	return c
}

// resolveConcatTarget determines the output resolution/fps and picks a
// synthetic probe.Result standing in for the "target format" every item is
// normalized against. Audio format is always derived from the clips
// themselves (first item that actually has audio), never overridden.
func resolveConcatTarget(items []concatItem, probes []*probe.Result, override *concatConfigTarget) (*probe.Result, error) {
	target := &probe.Result{CodecName: "h264"}

	if override != nil && override.Width > 0 && override.Height > 0 {
		target.Width = override.Width
		target.Height = override.Height
	}
	if override != nil && override.FPS != "" {
		target.FPS = override.FPS
	}

	if target.Width == 0 || target.Height == 0 || target.FPS == "" {
		var firstVideo *probe.Result
		for i, it := range items {
			if !it.isImage {
				firstVideo = probes[i]
				break
			}
		}
		if firstVideo == nil {
			return nil, fmt.Errorf("cannot determine target resolution: no video items and no --width/--height (or config target) given")
		}
		if target.Width == 0 {
			target.Width = firstVideo.Width
		}
		if target.Height == 0 {
			target.Height = firstVideo.Height
		}
		if target.FPS == "" {
			target.FPS = firstVideo.FPS
		}
	}

	for _, p := range probes {
		if p != nil && p.HasAudio {
			target.HasAudio = true
			target.SampleRate = p.SampleRate
			target.Channels = p.Channels
			target.ChannelLayout = p.ChannelLayout
			target.SampleFmt = p.SampleFmt
			break
		}
	}

	return target, nil
}
