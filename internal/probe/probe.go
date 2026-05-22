package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Result holds the video metadata we care about.
type Result struct {
	Width         int
	Height        int
	FPS           string // raw fraction string e.g. "30000/1001"
	CodecName     string // decoder name e.g. "h264"
	HasAudio      bool
	SampleRate    string
	Channels      int
	ChannelLayout string
	SampleFmt     string
	Duration      float64
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType     string `json:"codec_type"`
		CodecName     string `json:"codec_name"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		RFrameRate    string `json:"r_frame_rate"`
		SampleRate    string `json:"sample_rate"`
		Channels      int    `json:"channels"`
		ChannelLayout string `json:"channel_layout"`
		SampleFmt     string `json:"sample_fmt"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// Run probes the given video file and returns its metadata.
func Run(ctx context.Context, ffprobePath, videoPath string) (*Result, error) {
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		videoPath,
	}
	cmd := exec.CommandContext(ctx, ffprobePath, args...)
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe: %w\n%s", err, errBuf.String())
	}

	var raw ffprobeOutput
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		return nil, fmt.Errorf("parsing ffprobe output: %w", err)
	}

	r := &Result{}

	dur, _ := strconv.ParseFloat(strings.TrimSpace(raw.Format.Duration), 64)
	r.Duration = dur

	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			r.Width = s.Width
			r.Height = s.Height
			r.FPS = s.RFrameRate
			r.CodecName = s.CodecName
		case "audio":
			r.HasAudio = true
			r.SampleRate = s.SampleRate
			r.Channels = s.Channels
			r.ChannelLayout = s.ChannelLayout
			r.SampleFmt = s.SampleFmt
		}
	}

	if r.Width == 0 || r.Height == 0 {
		return nil, fmt.Errorf("no video stream found in %q", videoPath)
	}
	if r.FPS == "" {
		r.FPS = "30/1"
	}
	if r.SampleRate == "" {
		r.SampleRate = "44100"
	}
	if r.Channels == 0 {
		r.Channels = 2
	}
	if r.ChannelLayout == "" {
		r.ChannelLayout = "stereo"
	}
	if r.SampleFmt == "" {
		r.SampleFmt = "fltp"
	}

	return r, nil
}

// EncoderName maps an ffprobe codec name to the corresponding encoder.
func EncoderName(codec string) string {
	encoders := map[string]string{
		"h264":  "libx264",
		"hevc":  "libx265",
		"vp9":   "libvpx-vp9",
		"vp8":   "libvpx",
		"av1":   "libaom-av1",
		"mpeg4": "mpeg4",
		"mpeg2video": "mpeg2video",
	}
	if enc, ok := encoders[codec]; ok {
		return enc
	}
	return "libx264"
}
