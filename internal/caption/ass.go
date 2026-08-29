package caption

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultHighlight = "#5EEAD4"

type textSpan struct {
	Text      string
	Highlight bool
}

func isPopPreset(name string) bool {
	return name == "pop"
}

// parseSpans splits text on [[highlight]] markers. A Spec.Highlight value
// wraps the first case-insensitive match of that word if no markers exist.
func parseSpans(text, highlight string) []textSpan {
	var spans []textSpan
	rest := text
	for {
		start := strings.Index(rest, "[[")
		if start < 0 {
			if rest != "" {
				spans = append(spans, textSpan{Text: rest})
			}
			break
		}
		if start > 0 {
			spans = append(spans, textSpan{Text: rest[:start]})
		}
		rest = rest[start+2:]
		end := strings.Index(rest, "]]")
		if end < 0 {
			spans = append(spans, textSpan{Text: "[[" + rest})
			break
		}
		spans = append(spans, textSpan{Text: rest[:end], Highlight: true})
		rest = rest[end+2:]
	}
	if highlight != "" && !hasHighlight(spans) {
		spans = wrapFirstWord(spans, highlight)
	}
	return spans
}

func hasHighlight(spans []textSpan) bool {
	for _, s := range spans {
		if s.Highlight {
			return true
		}
	}
	return false
}

func wrapFirstWord(spans []textSpan, word string) []textSpan {
	needle := strings.ToLower(word)
	var out []textSpan
	done := false
	for _, s := range spans {
		if done || s.Highlight {
			out = append(out, s)
			continue
		}
		lower := strings.ToLower(s.Text)
		idx := strings.Index(lower, needle)
		if idx < 0 {
			out = append(out, s)
			continue
		}
		if idx > 0 {
			out = append(out, textSpan{Text: s.Text[:idx]})
		}
		out = append(out, textSpan{Text: s.Text[idx : idx+len(word)], Highlight: true})
		if idx+len(word) < len(s.Text) {
			out = append(out, textSpan{Text: s.Text[idx+len(word):]})
		}
		done = true
	}
	if !done {
		return spans
	}
	return out
}

func hexToASS(hex string) string {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		hex = strings.TrimPrefix(defaultHighlight, "#")
	}
	r := hex[0:2]
	g := hex[2:4]
	b := hex[4:6]
	return "&H00" + strings.ToUpper(b+g+r)
}

func assTimestamp(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	d := time.Duration(seconds * float64(time.Second))
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	cs := int(d.Milliseconds()/10) % 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, cs)
}

func assAlignment(position string) (align, marginV int) {
	switch position {
	case "top":
		return 8, 90
	case "upper":
		return 8, 280
	case "bottom":
		return 2, 90
	default:
		return 5, 0
	}
}

func escapeASS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "{", `\{`)
	s = strings.ReplaceAll(s, "}", `\}`)
	s = strings.ReplaceAll(s, "\n", `\N`)
	return s
}

func popDialogueText(s Spec) string {
	spans := parseSpans(s.Text, s.Highlight)
	hl := defaultHighlight
	if s.HighlightColor != "" {
		hl = s.HighlightColor
	}
	box := hexToASS(hl)
	var b strings.Builder
	if s.Fade > 0 {
		ms := int(s.Fade * 1000)
		fmt.Fprintf(&b, `{\fad(%d,%d)}`, ms, ms)
	}
	for _, sp := range spans {
		text := strings.ToUpper(sp.Text)
		if text == "" {
			continue
		}
		if sp.Highlight {
			// Opaque mint box + white text + black edge. BorderStyle 3 via
			// a named style reset is unreliable mid-line in libass, so the
			// box is drawn as a fat colored outline + matching shadow.
			fmt.Fprintf(&b, `{\bord7\shad0\3c%s\4c%s\1c&H00FFFFFF&}%s{\bord5\shad3\3c&H00000000&\4c&H00000000&\1c&H00FFFFFF&}`, box, box, escapeASS(" "+text+" "))
			continue
		}
		b.WriteString(escapeASS(text))
	}
	return b.String()
}

func popEvents(s Spec) [][3]string {
	// each event is start, end, text
	text := popDialogueText(s)
	if s.IsInterval() {
		count := s.Count
		if count <= 0 {
			count = 40
		}
		var events [][3]string
		for i := 0; i < count; i++ {
			start := s.Every * float64(i)
			end := start + s.Duration
			events = append(events, [3]string{assTimestamp(start), assTimestamp(end), text})
		}
		return events
	}
	return [][3]string{{assTimestamp(s.Start), assTimestamp(s.End), text}}
}

func writeASSFile(specs []Spec, width, height int) (string, error) {
	if width <= 0 {
		width = 1080
	}
	if height <= 0 {
		height = 1920
	}
	f, err := os.CreateTemp("", "vidsplash-caption-*.ass")
	if err != nil {
		return "", fmt.Errorf("creating ass file: %w", err)
	}
	defer f.Close()

	// Use the first pop spec for shared style (size/color/position/highlight).
	base := specs[0]
	preset := presets["pop"]
	fontSize := preset.FontSize
	if base.FontSize > 0 {
		fontSize = base.FontSize
	}
	pos := preset.Position
	if base.Position != "" {
		pos = base.Position
	}
	align, marginV := assAlignment(pos)
	hl := defaultHighlight
	if base.HighlightColor != "" {
		hl = base.HighlightColor
	}

	var b strings.Builder
	b.WriteString("[Script Info]\n")
	b.WriteString("ScriptType: v4.00+\n")
	b.WriteString("WrapStyle: 0\n")
	b.WriteString("ScaledBorderAndShadow: yes\n")
	b.WriteString(fmt.Sprintf("PlayResX: %d\nPlayResY: %d\n\n", width, height))
	b.WriteString("[V4+ Styles]\n")
	b.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	fmt.Fprintf(&b, "Style: Pop,Arial Black,%d,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,-1,0,0,0,100,100,0,0,1,5,3,%d,48,48,%d,1\n", fontSize, align, marginV)
	fmt.Fprintf(&b, "Style: PopHL,Arial Black,%d,&H00FFFFFF,&H000000FF,&H00000000,%s,-1,0,0,0,100,100,0,0,3,0,0,%d,48,48,%d,1\n\n", fontSize, hexToASS(hl), align, marginV)
	b.WriteString("[Events]\n")
	b.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

	for _, s := range specs {
		if s.FontSize > 0 || (s.Position != "" && s.Position != pos) || (s.HighlightColor != "" && s.HighlightColor != hl) {
			// per-caption overrides: still one style sheet; size/position follow first spec.
			_ = s
		}
		for _, ev := range popEvents(s) {
			fmt.Fprintf(&b, "Dialogue: 0,%s,%s,Pop,,0,0,0,,%s\n", ev[0], ev[1], ev[2])
		}
	}

	if _, err := f.WriteString(b.String()); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("writing ass file: %w", err)
	}
	return f.Name(), nil
}

func assFilter(assPath, fontFile string) string {
	opts := []string{"ass=" + escapeFilterValue(assPath)}
	if fontFile != "" {
		dir := filepath.Dir(fontFile)
		if dir != "" && dir != "." {
			opts[0] += ":fontsdir=" + escapeFilterValue(dir)
		}
	}
	return opts[0]
}
