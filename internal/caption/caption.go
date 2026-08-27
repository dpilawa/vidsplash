package caption

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dpilawa/vidsplash/internal/ffmpeg"
	"github.com/dpilawa/vidsplash/internal/probe"
)

// Spec describes one caption. Exactly one of the two timing modes must be
// set: explicit (Start/End) or interval (Every/Duration, optionally Count).
type Spec struct {
	Text string

	// Explicit mode: shown once, from Start to End.
	Start float64
	End   float64

	// Interval mode: shown for Duration seconds, repeating every Every
	// seconds. Count bounds the number of repeats (0 = until video ends).
	Every    float64
	Duration float64
	Count    int

	Preset    string
	FontFile  string
	FontSize  int
	FontColor string
	Position  string // "top", "center", or "bottom"; "" uses the preset default
	Fade      float64
}

// IsInterval reports whether this spec uses interval (repeating) timing.
func (s Spec) IsInterval() bool { return s.Every > 0 }

type presetDefaults struct {
	FontSize  int
	FontColor string
	BoxColor  string
	BoxBorder int
	Position  string
}

const DefaultPreset = "caption-bar"

var presets = map[string]presetDefaults{
	"caption-bar":   {FontSize: 42, FontColor: "white", BoxColor: "black@0.5", BoxBorder: 20, Position: "bottom"},
	"centered-pill": {FontSize: 36, FontColor: "white", BoxColor: "black@0.55", BoxBorder: 24, Position: "center"},
	"top-banner":    {FontSize: 40, FontColor: "white", BoxColor: "black@0.6", BoxBorder: 16, Position: "top"},
}

// PresetNames returns the known preset names, for flag help / validation.
func PresetNames() []string {
	names := make([]string, 0, len(presets))
	for n := range presets {
		names = append(names, n)
	}
	return names
}

// Validate checks a spec's timing mode and preset are well-formed.
func (s Spec) Validate() error {
	if strings.TrimSpace(s.Text) == "" {
		return fmt.Errorf("caption text cannot be empty")
	}
	explicit := s.End > s.Start
	interval := s.Every > 0
	switch {
	case explicit && interval:
		return fmt.Errorf("caption %q: cannot mix start/end with every/duration", s.Text)
	case !explicit && !interval:
		return fmt.Errorf("caption %q: need either start/end or every/duration", s.Text)
	case interval && s.Duration <= 0:
		return fmt.Errorf("caption %q: every requires a positive duration", s.Text)
	case interval && s.Duration >= s.Every:
		return fmt.Errorf("caption %q: duration (%.2fs) must be less than every (%.2fs)", s.Text, s.Duration, s.Every)
	}
	if s.Preset != "" {
		if _, ok := presets[s.Preset]; !ok {
			return fmt.Errorf("caption %q: unknown preset %q", s.Text, s.Preset)
		}
	}
	if s.Position != "" && s.Position != "top" && s.Position != "center" && s.Position != "bottom" {
		return fmt.Errorf("caption %q: --position must be top, center, or bottom", s.Text)
	}
	return nil
}

// BuildFilter assembles the full -vf filter chain for all specs, writing
// each caption's text to a temp file (drawtext's textfile= option, which
// sidesteps filtergraph escaping entirely). Callers must call the returned
// cleanup func once ffmpeg has run.
func BuildFilter(specs []Spec, defaultFontFile string) (string, func(), error) {
	var parts []string
	var textFiles []string
	cleanup := func() {
		for _, f := range textFiles {
			os.Remove(f)
		}
	}

	for _, s := range specs {
		textFile, err := writeTextFile(s.Text)
		if err != nil {
			cleanup()
			return "", nil, err
		}
		textFiles = append(textFiles, textFile)

		part, err := drawtextFilter(s, textFile, defaultFontFile)
		if err != nil {
			cleanup()
			return "", nil, err
		}
		parts = append(parts, part)
	}

	return strings.Join(parts, ","), cleanup, nil
}

func writeTextFile(text string) (string, error) {
	f, err := os.CreateTemp("", "vidsplash-caption-*.txt")
	if err != nil {
		return "", fmt.Errorf("creating caption text file: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("writing caption text file: %w", err)
	}
	return f.Name(), nil
}

func drawtextFilter(s Spec, textFile, defaultFontFile string) (string, error) {
	preset := presets[DefaultPreset]
	if s.Preset != "" {
		preset = presets[s.Preset]
	}

	fontFile := s.FontFile
	if fontFile == "" {
		fontFile = defaultFontFile
	}
	if fontFile == "" {
		return "", fmt.Errorf("no font file available: pass --font-file")
	}
	if _, err := os.Stat(fontFile); err != nil {
		return "", fmt.Errorf("font file not found: %q", fontFile)
	}

	fontSize := preset.FontSize
	if s.FontSize > 0 {
		fontSize = s.FontSize
	}
	fontColor := preset.FontColor
	if s.FontColor != "" {
		fontColor = s.FontColor
	}
	position := preset.Position
	if s.Position != "" {
		position = s.Position
	}

	x, y := positionExpr(position)
	win := buildWindow(s)

	opts := []string{
		"fontfile=" + escapeFilterValue(fontFile),
		"textfile=" + escapeFilterValue(textFile),
		"expansion=none",
		fmt.Sprintf("fontsize=%d", fontSize),
		"fontcolor=" + fontColor,
		"box=1",
		"boxcolor=" + preset.BoxColor,
		fmt.Sprintf("boxborderw=%d", preset.BoxBorder),
		"x=" + x,
		"y=" + y,
		"enable=" + quoteExpr(win.enable),
	}
	if win.alpha != "" {
		opts = append(opts, "alpha="+quoteExpr(win.alpha))
	}

	return "drawtext=" + strings.Join(opts, ":"), nil
}

func positionExpr(position string) (x, y string) {
	x = "(w-tw)/2"
	switch position {
	case "top":
		y = "40"
	case "center":
		y = "(h-th)/2"
	default: // bottom
		y = "h-th-40"
	}
	return x, y
}

type window struct {
	enable string
	alpha  string
}

// buildWindow turns a Spec's timing into ffmpeg "enable"/"alpha" expressions
// over the filter's built-in time variable t. Interval mode uses mod(t,every)
// as the "local" time so the same expression repeats each period.
func buildWindow(s Spec) window {
	var localT string
	var enable string
	var duration float64

	if s.IsInterval() {
		duration = s.Duration
		localT = fmt.Sprintf("mod(t,%s)", num(s.Every))
		enable = fmt.Sprintf("lt(%s,%s)", localT, num(duration))
		if s.Count > 0 {
			bound := s.Every * float64(s.Count)
			enable = fmt.Sprintf("%s*lt(t,%s)", enable, num(bound))
		}
	} else {
		duration = s.End - s.Start
		localT = fmt.Sprintf("(t-%s)", num(s.Start))
		enable = fmt.Sprintf("between(t,%s,%s)", num(s.Start), num(s.End))
	}

	var alpha string
	if s.Fade > 0 && s.Fade*2 <= duration {
		alpha = fmt.Sprintf(
			"if(lt(%s,%s),%s/%s,if(lt(%s,%s),1,(%s-%s)/%s))",
			localT, num(s.Fade), localT, num(s.Fade),
			localT, num(duration-s.Fade),
			num(duration), localT, num(s.Fade),
		)
	}

	return window{enable: enable, alpha: alpha}
}

func num(f float64) string {
	return strconv.FormatFloat(f, 'f', 3, 64)
}

// escapeFilterValue escapes a value embedded in an ffmpeg filtergraph
// description (paths, mainly): backslashes and colons are the two
// characters that would otherwise be misparsed.
func escapeFilterValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `:`, `\:`)
	return v
}

// quoteExpr wraps an ffmpeg expression in single quotes so the commas
// inside function calls like between(t,a,b) aren't parsed as filter-chain
// separators.
func quoteExpr(expr string) string {
	return "'" + expr + "'"
}

// RenderOptions configures the final ffmpeg invocation.
type RenderOptions struct {
	InputPath  string
	OutputPath string
	Filter     string
	FFmpegPath string
	Overwrite  bool
}

// Render burns the caption filter chain into the video, copying audio
// through unchanged.
func Render(ctx context.Context, p *probe.Result, opts RenderOptions, runner ffmpeg.Runner, onProgress func(ffmpeg.ProgressEvent)) error {
	encoder := probe.EncoderName(p.CodecName)
	overwriteFlag := "-n"
	if opts.Overwrite {
		overwriteFlag = "-y"
	}
	args := []string{
		overwriteFlag,
		"-i", opts.InputPath,
		"-vf", opts.Filter,
		"-c:v", encoder,
		"-preset", "fast",
		"-pix_fmt", "yuv420p",
	}
	if p.HasAudio {
		args = append(args, "-c:a", "copy")
	}
	args = append(args, "-progress", "pipe:1", opts.OutputPath)
	return runner.Run(ctx, args, onProgress)
}
