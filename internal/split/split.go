package split

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dpilawa/vidsplash/internal/ffmpeg"
	"github.com/dpilawa/vidsplash/internal/probe"
)

// Segment is a [Start, End) range of the source video, in seconds.
type Segment struct {
	Start float64
	End   float64
}

// ParseTimestamp parses "SS", "SS.sss", "MM:SS", or "HH:MM:SS(.sss)" into seconds.
func ParseTimestamp(s string) (float64, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if s == "" || len(parts) > 3 {
		return 0, fmt.Errorf("invalid timestamp %q", s)
	}
	var secs float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid timestamp %q", s)
		}
		secs = secs*60 + v
	}
	return secs, nil
}

// SegmentsFromCutPoints turns a list of interior cut points into contiguous
// segments spanning [0, totalDuration]. Points are deduped and sorted;
// any point outside (0, totalDuration) is an error.
func SegmentsFromCutPoints(cutPoints []float64, totalDuration float64) ([]Segment, error) {
	sorted := append([]float64(nil), cutPoints...)
	sort.Float64s(sorted)

	bounds := []float64{0}
	prev := -1.0
	for _, c := range sorted {
		if c <= 0 || c >= totalDuration {
			return nil, fmt.Errorf("cut point %.3fs is out of range (0, %.3f)", c, totalDuration)
		}
		if c == prev {
			continue
		}
		bounds = append(bounds, c)
		prev = c
	}
	bounds = append(bounds, totalDuration)

	segments := make([]Segment, 0, len(bounds)-1)
	for i := 0; i < len(bounds)-1; i++ {
		segments = append(segments, Segment{Start: bounds[i], End: bounds[i+1]})
	}
	return segments, nil
}

// Options configures extracting one segment.
type Options struct {
	InputPath  string
	OutputPath string
	Start      float64
	End        float64
	Reencode   bool
	FFmpegPath string
}

// Extract cuts one [Start, End) segment out of the input video.
// By default it stream-copies (fast, snapped to the nearest keyframe);
// Reencode re-encodes for frame-accurate cuts.
func Extract(ctx context.Context, p *probe.Result, opts Options, runner ffmpeg.Runner, onProgress func(ffmpeg.ProgressEvent)) error {
	duration := opts.End - opts.Start

	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", opts.Start),
		"-i", opts.InputPath,
		"-t", fmt.Sprintf("%.3f", duration),
	}

	if opts.Reencode {
		encoder := probe.EncoderName(p.CodecName)
		args = append(args, "-c:v", encoder, "-preset", "fast", "-pix_fmt", "yuv420p")
		if p.HasAudio {
			args = append(args, "-c:a", "aac", "-ar", p.SampleRate, "-ac", strconv.Itoa(p.Channels))
		}
	} else {
		args = append(args, "-c", "copy", "-avoid_negative_ts", "make_zero")
	}

	args = append(args, "-progress", "pipe:1", opts.OutputPath)
	return runner.Run(ctx, args, onProgress)
}
