package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dpilawa/vidsplash/internal/caption"
	"github.com/dpilawa/vidsplash/internal/ffmpeg"
	"github.com/dpilawa/vidsplash/internal/probe"
	"github.com/dpilawa/vidsplash/internal/ui"
)

var (
	flagCaptionOutput   string
	flagCaptionConfig   string
	flagCaptionText     string
	flagCaptionStart    float64
	flagCaptionEnd      float64
	flagCaptionEvery    float64
	flagCaptionDuration float64
	flagCaptionCount    int
	flagCaptionPreset   string
	flagCaptionFade     float64
	flagCaptionFontSize int
	flagCaptionFontCol  string
	flagCaptionFontFile string
	flagCaptionPosition string
	flagCaptionHL       string
	flagCaptionHLColor  string
	flagCaptionNoAudio  bool
)

var captionCmd = &cobra.Command{
	Use:   "caption INPUT_VIDEO",
	Short: "Add Snapchat/Instagram-style text overlays to a video",
	Long: "Add Snapchat/Instagram-style text overlays to a video.\n\n" +
		"Use --text with --start/--end (one caption) or --every/--duration (repeating),\n" +
		"or use --config for multiple captions with per-item styling.",
	Args: cobra.ExactArgs(1),
	RunE: runCaption,
}

const (
	captionStageProbe  = 0
	captionStageRender = 1
)

var captionStageLabels = []string{"Probing video", "Rendering captions"}

func init() {
	captionCmd.Flags().StringVarP(&flagCaptionOutput, "output", "o", "", "Output video path (required)")
	captionCmd.Flags().StringVarP(&flagCaptionConfig, "config", "c", "", "JSON config file describing multiple captions (alternative to --text)")
	captionCmd.Flags().StringVar(&flagCaptionText, "text", "", "Caption text (simple single-caption mode)")
	captionCmd.Flags().Float64Var(&flagCaptionStart, "start", 0, "Caption start time in seconds (explicit mode)")
	captionCmd.Flags().Float64Var(&flagCaptionEnd, "end", 0, "Caption end time in seconds (explicit mode)")
	captionCmd.Flags().Float64Var(&flagCaptionEvery, "every", 0, "Repeat interval in seconds (interval mode)")
	captionCmd.Flags().Float64Var(&flagCaptionDuration, "duration", 0, "On-screen duration per occurrence, seconds (interval mode)")
	captionCmd.Flags().IntVar(&flagCaptionCount, "count", 0, "Number of repeats for interval mode (0 = until the video ends)")
	captionCmd.Flags().StringVar(&flagCaptionPreset, "preset", caption.DefaultPreset, "Style preset: caption-bar, centered-pill, top-banner, hook, or pop")
	captionCmd.Flags().Float64Var(&flagCaptionFade, "fade", 0.3, "Fade in/out duration in seconds, 0 = no fade")
	captionCmd.Flags().IntVar(&flagCaptionFontSize, "font-size", 0, "Font size override (0 = preset default)")
	captionCmd.Flags().StringVar(&flagCaptionFontCol, "font-color", "", "Font color override (any ffmpeg color string)")
	captionCmd.Flags().StringVar(&flagCaptionFontFile, "font-file", "", "Path to a .ttf/.otf font file (defaults to a system font)")
	captionCmd.Flags().StringVar(&flagCaptionPosition, "position", "", "top, upper, center, or bottom (empty = preset default)")
	captionCmd.Flags().StringVar(&flagCaptionHL, "highlight", "", "Word to emphasize with a color box (pop preset; or wrap [[word]] in --text)")
	captionCmd.Flags().StringVar(&flagCaptionHLColor, "highlight-color", "", "Highlight box color as #RRGGBB (pop preset)")
	captionCmd.Flags().BoolVar(&flagCaptionNoAudio, "no-audio", false, "Strip audio from the output")
}

type captionConfigFile struct {
	Captions []captionConfigItem `json:"captions"`
}

type captionConfigItem struct {
	Text      string  `json:"text"`
	Start     float64 `json:"start,omitempty"`
	End       float64 `json:"end,omitempty"`
	Every     float64 `json:"every,omitempty"`
	Duration  float64 `json:"duration,omitempty"`
	Count     int     `json:"count,omitempty"`
	Preset    string  `json:"preset,omitempty"`
	Fade      float64 `json:"fade,omitempty"`
	FontSize  int     `json:"font_size,omitempty"`
	FontColor string  `json:"font_color,omitempty"`
	FontFile       string  `json:"font_file,omitempty"`
	Position       string  `json:"position,omitempty"`
	Highlight      string  `json:"highlight,omitempty"`
	HighlightColor string  `json:"highlight_color,omitempty"`
}

var defaultFontCandidates = []string{
	`C:\Windows\Fonts\ariblk.ttf`,
	`C:\Windows\Fonts\arialbd.ttf`,
	`C:\Windows\Fonts\arial.ttf`,
	"/System/Library/Fonts/Supplemental/Arial.ttf",
	"/System/Library/Fonts/Helvetica.ttc",
}

func resolveDefaultFontFile() string {
	for _, path := range defaultFontCandidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func runCaption(cmd *cobra.Command, args []string) error {
	inputPath := args[0]

	if flagCaptionOutput == "" {
		return fmt.Errorf("--output is required")
	}
	if flagCaptionConfig != "" && flagCaptionText != "" {
		return fmt.Errorf("cannot combine --config with --text")
	}

	var specs []caption.Spec
	if flagCaptionConfig != "" {
		data, err := os.ReadFile(flagCaptionConfig)
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
		var cfg captionConfigFile
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
		if len(cfg.Captions) == 0 {
			return fmt.Errorf("config has no captions")
		}
		for _, c := range cfg.Captions {
			specs = append(specs, captionConfigItem2Spec(c))
		}
	} else {
		if flagCaptionText == "" {
			return fmt.Errorf("--text is required (or use --config)")
		}
		specs = append(specs, caption.Spec{
			Text:      flagCaptionText,
			Start:     flagCaptionStart,
			End:       flagCaptionEnd,
			Every:     flagCaptionEvery,
			Duration:  flagCaptionDuration,
			Count:     flagCaptionCount,
			Preset:    flagCaptionPreset,
			Fade:      flagCaptionFade,
			FontSize:  flagCaptionFontSize,
			FontColor: flagCaptionFontCol,
			FontFile:       flagCaptionFontFile,
			Position:       flagCaptionPosition,
			Highlight:      flagCaptionHL,
			HighlightColor: flagCaptionHLColor,
		})
	}

	for _, s := range specs {
		if err := s.Validate(); err != nil {
			return err
		}
	}

	if err := checkInputs(inputPath); err != nil {
		return err
	}
	if err := checkOutput(flagCaptionOutput); err != nil {
		return err
	}
	if err := checkBinaries(); err != nil {
		return err
	}

	ctx, cancel := newSignalContext()
	defer cancel()

	return runStages(ctx, "vidsplash caption", captionStageLabels, func(ctx context.Context, r Reporter) error {
		return captionPipeline(ctx, r, inputPath, specs)
	})
}

func captionConfigItem2Spec(c captionConfigItem) caption.Spec {
	return caption.Spec{
		Text:      c.Text,
		Start:     c.Start,
		End:       c.End,
		Every:     c.Every,
		Duration:  c.Duration,
		Count:     c.Count,
		Preset:    c.Preset,
		Fade:      c.Fade,
		FontSize:  c.FontSize,
		FontColor: c.FontColor,
		FontFile:       c.FontFile,
		Position:       c.Position,
		Highlight:      c.Highlight,
		HighlightColor: c.HighlightColor,
	}
}

func captionPipeline(ctx context.Context, r Reporter, inputPath string, specs []caption.Spec) error {
	runner := &ffmpeg.ExecRunner{FFmpegPath: flagFFmpeg}

	r.StageStart(captionStageProbe)
	t0 := time.Now()
	p, err := probe.Run(ctx, flagFFprobe, inputPath)
	if err != nil {
		r.StageError(captionStageProbe, err)
		return err
	}
	r.StageDone(captionStageProbe, time.Since(t0))

	defaultFont := resolveDefaultFontFile()
	filter, cleanup, err := caption.BuildFilterFor(specs, defaultFont, p.Width, p.Height)
	if err != nil {
		return err
	}
	defer cleanup()

	r.StageStart(captionStageRender)
	t1 := time.Now()

	opts := caption.RenderOptions{
		InputPath:  inputPath,
		OutputPath: flagCaptionOutput,
		Filter:     filter,
		FFmpegPath: flagFFmpeg,
		Overwrite:  flagOverwrite,
		NoAudio:    flagCaptionNoAudio,
	}
	err = caption.Render(ctx, p, opts, runner, func(e ffmpeg.ProgressEvent) {
		r.Progress(captionStageRender, e.OutTimeUS, p.Duration, e.FPS, e.Speed)
	})
	if err != nil {
		os.Remove(flagCaptionOutput)
		r.StageError(captionStageRender, err)
		return err
	}
	r.StageDone(captionStageRender, time.Since(t1))

	outInfo, _ := os.Stat(flagCaptionOutput)
	var outSize int64
	if outInfo != nil {
		outSize = outInfo.Size()
	}

	r.Summary([]ui.StatItem{
		{Label: "Captions", Value: fmt.Sprintf("%d", len(specs))},
		{Label: "Output  ", Value: ui.FormatDuration(p.Duration)},
	}, outSize, flagCaptionOutput)

	return nil
}
