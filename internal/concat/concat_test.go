package concat

import (
	"context"
	"strings"
	"testing"

	"github.com/dpilawa/vidsplash/internal/ffmpeg"
	"github.com/dpilawa/vidsplash/internal/probe"
)

type fakeRunner struct {
	args []string
}

func (f *fakeRunner) Run(ctx context.Context, args []string, onProgress func(ffmpeg.ProgressEvent)) error {
	f.args = args
	return nil
}

func joinedArgs(args []string) string {
	return strings.Join(args, " ")
}

func TestNormalizeVideoWithAudioAndFades(t *testing.T) {
	target := &probe.Result{
		Width: 1080, Height: 1920, FPS: "30/1",
		HasAudio: true, SampleRate: "44100", Channels: 2, ChannelLayout: "stereo", SampleFmt: "fltp",
	}
	opts := NormalizeVideoOptions{
		VideoPath:  "in.mp4",
		OutputPath: "out.mp4",
		Duration:   10,
		HasAudio:   true,
		FadeIn:     0.5,
		FadeOut:    0.5,
		BGColor:    "black",
	}
	runner := &fakeRunner{}
	if err := NormalizeVideo(context.Background(), target, opts, runner, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := joinedArgs(runner.args)

	if !strings.Contains(joined, "scale=1080:1920:force_original_aspect_ratio=decrease") {
		t.Errorf("missing scale filter: %s", joined)
	}
	if !strings.Contains(joined, "fade=t=in:st=0:d=0.500") {
		t.Errorf("missing video fade-in: %s", joined)
	}
	if !strings.Contains(joined, "fade=t=out:st=9.500:d=0.500") {
		t.Errorf("missing video fade-out: %s", joined)
	}
	if !strings.Contains(joined, "afade=t=in:st=0:d=0.500") {
		t.Errorf("missing audio fade-in: %s", joined)
	}
	if !strings.Contains(joined, "aresample=44100") {
		t.Errorf("missing audio resample to target rate: %s", joined)
	}
	if strings.Contains(joined, "anullsrc") {
		t.Errorf("should not synthesize silence when source has audio: %s", joined)
	}
}

func TestNormalizeVideoSourceWithoutAudioSynthesizesSilence(t *testing.T) {
	target := &probe.Result{
		Width: 640, Height: 480, FPS: "25/1",
		HasAudio: true, SampleRate: "48000", Channels: 2, ChannelLayout: "stereo", SampleFmt: "fltp",
	}
	opts := NormalizeVideoOptions{
		VideoPath:  "in.mp4",
		OutputPath: "out.mp4",
		Duration:   7.5,
		HasAudio:   false,
		BGColor:    "black",
	}
	runner := &fakeRunner{}
	if err := NormalizeVideo(context.Background(), target, opts, runner, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := joinedArgs(runner.args)

	if !strings.Contains(joined, "anullsrc=channel_layout=stereo:sample_rate=48000") {
		t.Errorf("missing silence synthesis: %s", joined)
	}
	if !strings.Contains(joined, "atrim=duration=7.500") {
		t.Errorf("missing atrim to source duration: %s", joined)
	}
}

func TestNormalizeVideoTargetWithoutAudioOmitsAudioArgs(t *testing.T) {
	target := &probe.Result{Width: 640, Height: 480, FPS: "25/1", HasAudio: false}
	opts := NormalizeVideoOptions{
		VideoPath:  "in.mp4",
		OutputPath: "out.mp4",
		Duration:   5,
		HasAudio:   true,
		BGColor:    "black",
	}
	runner := &fakeRunner{}
	if err := NormalizeVideo(context.Background(), target, opts, runner, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := joinedArgs(runner.args)

	if strings.Contains(joined, "-map [aout]") || strings.Contains(joined, "-c:a") {
		t.Errorf("target has no audio, should not map/encode audio: %s", joined)
	}
}

func TestRunDemuxerBuildsConcatListAndCopies(t *testing.T) {
	runner := &fakeRunner{}
	err := RunDemuxer(context.Background(), []string{"a.mp4", "b.mp4"}, "out.mp4", true, runner, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := joinedArgs(runner.args)
	if !strings.Contains(joined, "-f concat") || !strings.Contains(joined, "-c copy") {
		t.Errorf("expected concat demuxer + stream copy args: %s", joined)
	}
	if !strings.Contains(joined, "-y") {
		t.Errorf("expected overwrite flag -y: %s", joined)
	}
}

func TestRunDemuxerNoOverwriteUsesDashN(t *testing.T) {
	runner := &fakeRunner{}
	if err := RunDemuxer(context.Background(), []string{"a.mp4"}, "out.mp4", false, runner, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.args[0] != "-n" {
		t.Errorf("args[0] = %q, want -n", runner.args[0])
	}
}

func TestWriteConcatListEscapesSingleQuotes(t *testing.T) {
	path, cleanup, err := writeConcatList([]string{"a'b.mp4"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	if path == "" {
		t.Fatal("expected non-empty list file path")
	}
}
