package concat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dpilawa/vidsplash/internal/ffmpeg"
	"github.com/dpilawa/vidsplash/internal/probe"
)

// Options configures the concat operation.
type Options struct {
	SplashStartPath string // splash used at the start (prepend / both)
	SplashEndPath   string // splash used at the end (append / both); may equal SplashStartPath
	VideoPath       string
	OutputPath      string
	Position        string // "prepend", "append", or "both"
	VideoFadeIn     float64
	VideoFadeOut    float64
	VideoDuration   float64 // needed to compute fade-out start time
	NoAudioFade     bool
	FFmpegPath      string
	Overwrite       bool
}

// Run concatenates the splash segment with the source video.
func Run(ctx context.Context, p *probe.Result, opts Options, runner ffmpeg.Runner, onProgress func(ffmpeg.ProgressEvent)) error {
	encoder := probe.EncoderName(p.CodecName)

	overwriteFlag := "-n"
	if opts.Overwrite {
		overwriteFlag = "-y"
	}

	// Build optional per-stream fade filters for the video input.
	// videoIdx is the ffmpeg input index of the main video (0 or 1 depending on position).
	videoFadeV, videoFadeA := buildVideoFades(opts, p.HasAudio && !opts.NoAudioFade)

	var inputs []string
	var filterComplex string
	var mapArgs []string

	switch opts.Position {
	case "prepend":
		// input 0 = splash, input 1 = video
		inputs = []string{"-i", opts.SplashStartPath, "-i", opts.VideoPath}
		filterComplex = buildFilterComplex(videoFadeV, videoFadeA, 1, 2, p.HasAudio)
		mapArgs = mapArgsFor(p.HasAudio)
	case "append":
		// input 0 = video, input 1 = splash
		inputs = []string{"-i", opts.VideoPath, "-i", opts.SplashEndPath}
		filterComplex = buildFilterComplex(videoFadeV, videoFadeA, 0, 2, p.HasAudio)
		mapArgs = mapArgsFor(p.HasAudio)
	case "both":
		// input 0 = start splash, input 1 = video, input 2 = end splash
		inputs = []string{"-i", opts.SplashStartPath, "-i", opts.VideoPath, "-i", opts.SplashEndPath}
		filterComplex = buildFilterComplex(videoFadeV, videoFadeA, 1, 3, p.HasAudio)
		mapArgs = mapArgsFor(p.HasAudio)
	}

	args := []string{overwriteFlag}
	args = append(args, inputs...)
	args = append(args, "-filter_complex", filterComplex)
	args = append(args, mapArgs...)
	args = append(args,
		"-c:v", encoder,
		"-preset", "fast",
		"-pix_fmt", "yuv420p",
	)
	if p.HasAudio {
		args = append(args,
			"-c:a", "aac",
			"-ar", p.SampleRate,
			"-ac", strconv.Itoa(p.Channels),
		)
	}
	args = append(args,
		"-progress", fmt.Sprintf("pipe:1"),
		opts.OutputPath,
	)

	return runner.Run(ctx, args, onProgress)
}

// buildVideoFades returns the video and audio fade filter chains for the main video stream.
// Returns empty strings when no fades are requested.
func buildVideoFades(opts Options, hasAudio bool) (vFade, aFade string) {
	if opts.VideoFadeIn <= 0 && opts.VideoFadeOut <= 0 {
		return "", ""
	}
	fadeOutStart := opts.VideoDuration - opts.VideoFadeOut

	var vParts []string
	var aParts []string
	if opts.VideoFadeIn > 0 {
		vParts = append(vParts, fmt.Sprintf("fade=t=in:st=0:d=%.3f", opts.VideoFadeIn))
		if hasAudio {
			aParts = append(aParts, fmt.Sprintf("afade=t=in:st=0:d=%.3f", opts.VideoFadeIn))
		}
	}
	if opts.VideoFadeOut > 0 && fadeOutStart > 0 {
		vParts = append(vParts, fmt.Sprintf("fade=t=out:st=%.3f:d=%.3f", fadeOutStart, opts.VideoFadeOut))
		if hasAudio {
			aParts = append(aParts, fmt.Sprintf("afade=t=out:st=%.3f:d=%.3f", fadeOutStart, opts.VideoFadeOut))
		}
	}

	for i, p := range vParts {
		if i == 0 {
			vFade = p
		} else {
			vFade += "," + p
		}
	}
	for i, p := range aParts {
		if i == 0 {
			aFade = p
		} else {
			aFade += "," + p
		}
	}
	return
}

// buildFilterComplex assembles the -filter_complex string.
// videoInputIdx is the 0-based index of the main video among the ffmpeg inputs.
func buildFilterComplex(videoFadeV, videoFadeA string, videoInputIdx int, n int, hasAudio bool) string {
	// Label each input stream, applying fades to the video input if configured.
	var parts []string
	vLabels := make([]string, n)
	aLabels := make([]string, n)

	for i := 0; i < n; i++ {
		vIn := fmt.Sprintf("[%d:v]", i)
		aIn := fmt.Sprintf("[%d:a]", i)
		vOut := fmt.Sprintf("[v%d]", i)
		aOut := fmt.Sprintf("[a%d]", i)

		if i == videoInputIdx && videoFadeV != "" {
			parts = append(parts, fmt.Sprintf("%s%s%s", vIn, videoFadeV, vOut))
		} else {
			parts = append(parts, fmt.Sprintf("%snull%s", vIn, vOut))
		}
		vLabels[i] = vOut

		if hasAudio {
			if i == videoInputIdx && videoFadeA != "" {
				parts = append(parts, fmt.Sprintf("%s%s%s", aIn, videoFadeA, aOut))
			} else {
				parts = append(parts, fmt.Sprintf("%sanull%s", aIn, aOut))
			}
			aLabels[i] = aOut
		}
	}

	// Build concat input list
	var concatInputs string
	for i := 0; i < n; i++ {
		concatInputs += vLabels[i]
		if hasAudio {
			concatInputs += aLabels[i]
		}
	}

	aCount := 0
	if hasAudio {
		aCount = 1
	}
	if hasAudio {
		parts = append(parts, fmt.Sprintf("%sconcat=n=%d:v=1:a=1[vout][aout]", concatInputs, n))
	} else {
		parts = append(parts, fmt.Sprintf("%sconcat=n=%d:v=1:a=0[vout]", concatInputs, n))
	}
	_ = aCount

	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ";"
		}
		result += p
	}
	return result
}

func mapArgsFor(hasAudio bool) []string {
	if hasAudio {
		return []string{"-map", "[vout]", "-map", "[aout]"}
	}
	return []string{"-map", "[vout]"}
}

// NormalizeVideoOptions configures re-encoding one source clip to a common
// target format so it can be losslessly joined with other normalized
// segments (see RunDemuxer).
type NormalizeVideoOptions struct {
	VideoPath  string
	OutputPath string
	Duration   float64 // the source clip's own duration, for fade-out timing
	HasAudio   bool    // whether the source clip itself carries an audio stream
	FadeIn     float64
	FadeOut    float64
	BGColor    string
	FFmpegPath string
}

// NormalizeVideo re-encodes a video clip to target's resolution, frame rate,
// and audio format. Every clip normalized against the same target can then
// be joined via RunDemuxer with a lossless stream copy.
func NormalizeVideo(ctx context.Context, target *probe.Result, opts NormalizeVideoOptions, runner ffmpeg.Runner, onProgress func(ffmpeg.ProgressEvent)) error {
	vf := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,"+
			"pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=%s,"+
			"setsar=1,fps=%s",
		target.Width, target.Height,
		target.Width, target.Height,
		opts.BGColor,
		target.FPS,
	)
	if opts.FadeIn > 0 {
		vf += fmt.Sprintf(",fade=t=in:st=0:d=%.3f", opts.FadeIn)
	}
	if fadeOutStart := opts.Duration - opts.FadeOut; opts.FadeOut > 0 && fadeOutStart > 0 {
		vf += fmt.Sprintf(",fade=t=out:st=%.3f:d=%.3f", fadeOutStart, opts.FadeOut)
	}

	filterComplex := fmt.Sprintf("[0:v]%s[vout]", vf)
	mapArgs := []string{"-map", "[vout]"}
	args := []string{"-y", "-i", opts.VideoPath}

	if target.HasAudio {
		if opts.HasAudio {
			af := fmt.Sprintf("aresample=%s,aformat=channel_layouts=%s:sample_fmts=%s", target.SampleRate, target.ChannelLayout, target.SampleFmt)
			if opts.FadeIn > 0 {
				af += fmt.Sprintf(",afade=t=in:st=0:d=%.3f", opts.FadeIn)
			}
			if fadeOutStart := opts.Duration - opts.FadeOut; opts.FadeOut > 0 && fadeOutStart > 0 {
				af += fmt.Sprintf(",afade=t=out:st=%.3f:d=%.3f", fadeOutStart, opts.FadeOut)
			}
			filterComplex += fmt.Sprintf(";[0:a]%s[aout]", af)
		} else {
			// Source has no audio: synthesize silence trimmed to the clip's duration.
			args = append(args, "-f", "lavfi", "-i", fmt.Sprintf("anullsrc=channel_layout=%s:sample_rate=%s", target.ChannelLayout, target.SampleRate))
			filterComplex += fmt.Sprintf(";[1:a]atrim=duration=%.3f[aout]", opts.Duration)
		}
		mapArgs = append(mapArgs, "-map", "[aout]")
	}

	args = append(args, "-filter_complex", filterComplex)
	args = append(args, mapArgs...)
	args = append(args,
		"-c:v", "libx264",
		"-preset", "fast",
		"-pix_fmt", "yuv420p",
		"-fps_mode", "cfr",
	)
	if target.HasAudio {
		args = append(args,
			"-c:a", "aac",
			"-ar", target.SampleRate,
			"-ac", strconv.Itoa(target.Channels),
		)
	}
	args = append(args, "-progress", "pipe:1", opts.OutputPath)

	return runner.Run(ctx, args, onProgress)
}

// RunDemuxer joins pre-normalized segments (matching codec, resolution, and
// audio format) via the ffmpeg concat demuxer using a lossless stream copy.
func RunDemuxer(ctx context.Context, segmentPaths []string, outputPath string, overwrite bool, runner ffmpeg.Runner, onProgress func(ffmpeg.ProgressEvent)) error {
	listFile, cleanup, err := writeConcatList(segmentPaths)
	if err != nil {
		return err
	}
	defer cleanup()

	overwriteFlag := "-n"
	if overwrite {
		overwriteFlag = "-y"
	}

	args := []string{
		overwriteFlag,
		"-f", "concat",
		"-safe", "0",
		"-i", listFile,
		"-c", "copy",
		"-progress", "pipe:1",
		outputPath,
	}
	return runner.Run(ctx, args, onProgress)
}

func writeConcatList(paths []string) (string, func(), error) {
	f, err := os.CreateTemp("", "vidsplash-concat-list-*.txt")
	if err != nil {
		return "", nil, fmt.Errorf("creating concat list: %w", err)
	}
	defer f.Close()

	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			os.Remove(f.Name())
			return "", nil, fmt.Errorf("resolving path %q: %w", p, err)
		}
		escaped := strings.ReplaceAll(abs, "'", "'\\''")
		if _, err := fmt.Fprintf(f, "file '%s'\n", escaped); err != nil {
			os.Remove(f.Name())
			return "", nil, fmt.Errorf("writing concat list: %w", err)
		}
	}

	path := f.Name()
	return path, func() { os.Remove(path) }, nil
}
