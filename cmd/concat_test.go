package cmd

import (
	"testing"

	"github.com/dpilawa/vidsplash/internal/probe"
)

func TestIsImagePath(t *testing.T) {
	cases := map[string]bool{
		"a.png":  true,
		"a.JPG":  true,
		"a.jpeg": true,
		"a.webp": true,
		"a.mp4":  false,
		"a.mov":  false,
		"a":      false,
	}
	for path, want := range cases {
		if got := isImagePath(path); got != want {
			t.Errorf("isImagePath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestBgColorOrDefault(t *testing.T) {
	old := flagConcatBGColor
	flagConcatBGColor = "blue"
	defer func() { flagConcatBGColor = old }()

	if got := bgColorOrDefault(""); got != "blue" {
		t.Errorf("bgColorOrDefault(\"\") = %q, want blue", got)
	}
	if got := bgColorOrDefault("red"); got != "red" {
		t.Errorf("bgColorOrDefault(\"red\") = %q, want red", got)
	}
}

func TestResolveConfigItem(t *testing.T) {
	old := flagConcatBGColor
	flagConcatBGColor = "black"
	defer func() { flagConcatBGColor = old }()

	if _, err := resolveConfigItem(concatConfigItem{Video: "a.mp4", Image: "b.png"}); err == nil {
		t.Error("expected error when both video and image are set")
	}
	if _, err := resolveConfigItem(concatConfigItem{}); err == nil {
		t.Error("expected error when neither video nor image is set")
	}
	if _, err := resolveConfigItem(concatConfigItem{Image: "b.png"}); err == nil {
		t.Error("expected error when image item has no duration")
	}

	item, err := resolveConfigItem(concatConfigItem{Video: "a.mp4", FadeIn: 0.5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.isImage || item.path != "a.mp4" || item.fadeIn != 0.5 {
		t.Errorf("unexpected video item: %+v", item)
	}

	item, err = resolveConfigItem(concatConfigItem{Image: "b.png", Duration: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !item.isImage || item.path != "b.png" || item.duration != 3 || item.bgColor != "black" {
		t.Errorf("unexpected image item (expected default bg color): %+v", item)
	}
}

func TestResolveConcatTargetFromFirstVideo(t *testing.T) {
	items := []concatItem{
		{isImage: true, path: "img.png"},
		{isImage: false, path: "a.mp4"},
	}
	probes := []*probe.Result{
		nil,
		{Width: 640, Height: 480, FPS: "24/1", HasAudio: true, SampleRate: "44100", Channels: 2, ChannelLayout: "stereo", SampleFmt: "fltp"},
	}
	target, err := resolveConcatTarget(items, probes, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Width != 640 || target.Height != 480 || target.FPS != "24/1" {
		t.Errorf("unexpected target dims: %+v", target)
	}
	if !target.HasAudio || target.SampleRate != "44100" {
		t.Errorf("expected target to inherit audio params from the video item: %+v", target)
	}
}

func TestResolveConcatTargetOverride(t *testing.T) {
	items := []concatItem{{isImage: false, path: "a.mp4"}}
	probes := []*probe.Result{
		{Width: 640, Height: 480, FPS: "24/1"},
	}
	override := &concatConfigTarget{Width: 1080, Height: 1920, FPS: "30/1"}
	target, err := resolveConcatTarget(items, probes, override)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Width != 1080 || target.Height != 1920 || target.FPS != "30/1" {
		t.Errorf("override not applied: %+v", target)
	}
}

func TestResolveConcatTargetNoVideoNoOverrideErrors(t *testing.T) {
	items := []concatItem{{isImage: true, path: "img.png"}}
	probes := []*probe.Result{nil}
	if _, err := resolveConcatTarget(items, probes, nil); err == nil {
		t.Error("expected error when there are no video items and no override")
	}
}
