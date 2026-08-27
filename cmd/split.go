package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dpilawa/vidsplash/internal/ffmpeg"
	"github.com/dpilawa/vidsplash/internal/probe"
	"github.com/dpilawa/vidsplash/internal/split"
	"github.com/dpilawa/vidsplash/internal/ui"
)

var (
	flagSplitAt       string
	flagSplitSegments string
	flagSplitOutdir   string
	flagSplitPattern  string
	flagSplitReencode bool
)

var splitCmd = &cobra.Command{
	Use:   "split INPUT_VIDEO",
	Short: "Split a video into multiple files at given timestamps",
	Args:  cobra.ExactArgs(1),
	RunE:  runSplit,
}

const (
	splitStageProbe = 0
	splitStageWrite = 1
)

var splitStageLabels = []string{"Probing video", "Writing segments"}

func init() {
	splitCmd.Flags().StringVar(&flagSplitAt, "at", "", "Comma-separated cut points, e.g. 00:30,01:15,90 (splits into N+1 segments)")
	splitCmd.Flags().StringVar(&flagSplitSegments, "segments", "", "Comma-separated start-end ranges, e.g. 0:00-0:30,0:30-end (end resolves to the video's duration)")
	splitCmd.Flags().StringVar(&flagSplitOutdir, "outdir", ".", "Directory to write segments into (created if missing)")
	splitCmd.Flags().StringVar(&flagSplitPattern, "pattern", "part-%03d.mp4", "printf-style filename pattern for segments (1-indexed)")
	splitCmd.Flags().BoolVar(&flagSplitReencode, "reencode", false, "Re-encode for frame-accurate cuts (default: fast stream-copy snapped to keyframes)")
}

func runSplit(cmd *cobra.Command, args []string) error {
	inputPath := args[0]

	if flagSplitAt == "" && flagSplitSegments == "" {
		return fmt.Errorf("one of --at or --segments is required")
	}
	if flagSplitAt != "" && flagSplitSegments != "" {
		return fmt.Errorf("--at and --segments are mutually exclusive")
	}
	if err := checkInputs(inputPath); err != nil {
		return err
	}
	if err := checkBinaries(); err != nil {
		return err
	}

	ctx, cancel := newSignalContext()
	defer cancel()

	return runStages(ctx, "vidsplash split", splitStageLabels, func(ctx context.Context, r Reporter) error {
		return splitPipeline(ctx, r, inputPath)
	})
}

func splitPipeline(ctx context.Context, r Reporter, inputPath string) error {
	runner := &ffmpeg.ExecRunner{FFmpegPath: flagFFmpeg}

	r.StageStart(splitStageProbe)
	t0 := time.Now()
	p, err := probe.Run(ctx, flagFFprobe, inputPath)
	if err != nil {
		r.StageError(splitStageProbe, err)
		return err
	}

	var segments []split.Segment
	if flagSplitAt != "" {
		var cutPoints []float64
		for _, raw := range strings.Split(flagSplitAt, ",") {
			ts, err := split.ParseTimestamp(raw)
			if err != nil {
				r.StageError(splitStageProbe, err)
				return err
			}
			cutPoints = append(cutPoints, ts)
		}
		segments, err = split.SegmentsFromCutPoints(cutPoints, p.Duration)
		if err != nil {
			r.StageError(splitStageProbe, err)
			return err
		}
	} else {
		segments, err = parseSegmentRanges(flagSplitSegments, p.Duration)
		if err != nil {
			r.StageError(splitStageProbe, err)
			return err
		}
	}
	r.StageDone(splitStageProbe, time.Since(t0))

	if err := os.MkdirAll(flagSplitOutdir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	outputPaths := make([]string, len(segments))
	for i := range segments {
		outputPaths[i] = filepath.Join(flagSplitOutdir, fmt.Sprintf(flagSplitPattern, i+1))
	}
	if !flagOverwrite {
		for _, out := range outputPaths {
			if _, err := os.Stat(out); err == nil {
				return fmt.Errorf("output file already exists: %q (use --overwrite to replace)", out)
			}
		}
	}

	r.StageStart(splitStageWrite)
	t1 := time.Now()

	for i, seg := range segments {
		opts := split.Options{
			InputPath:  inputPath,
			OutputPath: outputPaths[i],
			Start:      seg.Start,
			End:        seg.End,
			Reencode:   flagSplitReencode,
			FFmpegPath: flagFFmpeg,
		}
		segDuration := seg.End - seg.Start
		err := split.Extract(ctx, p, opts, runner, func(e ffmpeg.ProgressEvent) {
			r.Progress(splitStageWrite, e.OutTimeUS, segDuration, e.FPS, e.Speed)
		})
		if err != nil {
			r.StageError(splitStageWrite, fmt.Errorf("writing %q: %w", outputPaths[i], err))
			return err
		}
	}
	r.StageDone(splitStageWrite, time.Since(t1))

	var totalSize int64
	for _, out := range outputPaths {
		if info, err := os.Stat(out); err == nil {
			totalSize += info.Size()
		}
	}

	r.Summary([]ui.StatItem{
		{Label: "Segments", Value: fmt.Sprintf("%d", len(segments))},
		{Label: "Source  ", Value: ui.FormatDuration(p.Duration)},
	}, totalSize, flagSplitOutdir)

	return nil
}

func parseSegmentRanges(raw string, totalDuration float64) ([]split.Segment, error) {
	var segments []split.Segment
	for _, rangeStr := range strings.Split(raw, ",") {
		rangeStr = strings.TrimSpace(rangeStr)
		parts := strings.SplitN(rangeStr, "-", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid range %q: expected start-end", rangeStr)
		}
		start, err := split.ParseTimestamp(parts[0])
		if err != nil {
			return nil, err
		}
		var end float64
		if strings.EqualFold(strings.TrimSpace(parts[1]), "end") {
			end = totalDuration
		} else {
			end, err = split.ParseTimestamp(parts[1])
			if err != nil {
				return nil, err
			}
		}
		if end <= start {
			return nil, fmt.Errorf("range %q has end <= start", rangeStr)
		}
		if start < 0 || end > totalDuration {
			return nil, fmt.Errorf("range %q is out of bounds (0, %.3f)", rangeStr, totalDuration)
		}
		segments = append(segments, split.Segment{Start: start, End: end})
	}
	return segments, nil
}
