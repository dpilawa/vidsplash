package splash

import (
	"context"
	"fmt"
	"strconv"

	"github.com/dpilawa/vidsplash/internal/ffmpeg"
	"github.com/dpilawa/vidsplash/internal/probe"
)

// Options configures the splash segment.
type Options struct {
	ImagePath   string
	OutputPath  string
	Duration    float64
	FadeIn      float64
	FadeOut     float64
	BGColor     string
	FFmpegPath  string
}

// Build generates the splash video segment at opts.OutputPath.
func Build(ctx context.Context, p *probe.Result, opts Options, runner ffmpeg.Runner, onProgress func(ffmpeg.ProgressEvent)) error {
	// Base filters: scale, pad, normalize SAR
	vf := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,"+
			"pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=%s,"+
			"setsar=1",
		p.Width, p.Height,
		p.Width, p.Height,
		opts.BGColor,
	)
	// Append fade filters only when duration > 0
	if opts.FadeIn > 0 {
		vf += fmt.Sprintf(",fade=t=in:st=0:d=%.3f", opts.FadeIn)
	}
	if opts.FadeOut > 0 {
		fadeOutStart := opts.Duration - opts.FadeOut
		vf += fmt.Sprintf(",fade=t=out:st=%.3f:d=%.3f", fadeOutStart, opts.FadeOut)
	}

	videoFilter := fmt.Sprintf("[0:v]%s[vout]", vf)

	var filterComplex string
	var mapArgs []string

	if p.HasAudio {
		audioFilter := fmt.Sprintf(
			"anullsrc=channel_layout=%s:sample_rate=%s,"+
				"aformat=sample_fmts=%s,"+
				"atrim=duration=%.3f"+
				"[aout]",
			p.ChannelLayout, p.SampleRate, p.SampleFmt, opts.Duration,
		)
		filterComplex = videoFilter + ";" + audioFilter
		mapArgs = []string{"-map", "[vout]", "-map", "[aout]"}
	} else {
		filterComplex = videoFilter
		mapArgs = []string{"-map", "[vout]"}
	}

	encoder := probe.EncoderName(p.CodecName)

	args := buildArgs(opts, p, filterComplex, mapArgs, encoder)
	args = append(args,
		"-progress", "pipe:1",
		opts.OutputPath,
	)

	return runner.Run(ctx, args, onProgress)
}

func buildArgs(opts Options, p *probe.Result, filterComplex string, mapArgs []string, encoder string) []string {
	args := []string{
		"-y",
		"-loop", "1",
		"-framerate", p.FPS,
		"-i", opts.ImagePath,
	}

	if p.HasAudio {
		// second input: lavfi silent audio source
		args = append(args, "-f", "lavfi", "-i",
			fmt.Sprintf("anullsrc=channel_layout=%s:sample_rate=%s", p.ChannelLayout, p.SampleRate),
		)
	}

	args = append(args,
		"-filter_complex", filterComplex,
	)
	args = append(args, mapArgs...)
	args = append(args,
		"-t", fmt.Sprintf("%.3f", opts.Duration),
		"-c:v", encoder,
		"-preset", "fast",
		"-pix_fmt", "yuv420p",
		"-vsync", "cfr",
	)
	if p.HasAudio {
		args = append(args,
			"-c:a", "aac",
			"-ar", p.SampleRate,
			"-ac", strconv.Itoa(p.Channels),
		)
	}
	return args
}
