package concat

import (
	"context"
	"fmt"
	"strconv"

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
