package caption

import (
	"os"
	"strings"
	"testing"
)

func TestSpecValidate(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		wantErr bool
	}{
		{"explicit ok", Spec{Text: "hi", Start: 1, End: 4}, false},
		{"interval ok", Spec{Text: "hi", Every: 10, Duration: 3}, false},
		{"empty text", Spec{Text: "", Start: 1, End: 4}, true},
		{"no timing", Spec{Text: "hi"}, true},
		{"mixed timing", Spec{Text: "hi", Start: 1, End: 4, Every: 10}, true},
		{"interval no duration", Spec{Text: "hi", Every: 10}, true},
		{"interval duration too long", Spec{Text: "hi", Every: 10, Duration: 10}, true},
		{"unknown preset", Spec{Text: "hi", Start: 1, End: 4, Preset: "nope"}, true},
		{"known preset", Spec{Text: "hi", Start: 1, End: 4, Preset: "top-banner"}, false},
		{"bad position", Spec{Text: "hi", Start: 1, End: 4, Position: "middle"}, true},
		{"good position", Spec{Text: "hi", Start: 1, End: 4, Position: "center"}, false},
	}
	for _, c := range cases {
		err := c.spec.Validate()
		if c.wantErr && err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
		if !c.wantErr && err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
		}
	}
}

func TestPositionExpr(t *testing.T) {
	cases := []struct {
		position string
		wantY    string
	}{
		{"top", "40"},
		{"center", "(h-th)/2"},
		{"bottom", "h-th-40"},
		{"", "h-th-40"},
	}
	for _, c := range cases {
		x, y := positionExpr(c.position)
		if x != "(w-tw)/2" {
			t.Errorf("positionExpr(%q): x = %q, want (w-tw)/2", c.position, x)
		}
		if y != c.wantY {
			t.Errorf("positionExpr(%q): y = %q, want %q", c.position, y, c.wantY)
		}
	}
}

func TestBuildWindowExplicit(t *testing.T) {
	win := buildWindow(Spec{Start: 2, End: 5})
	if win.enable != "between(t,2.000,5.000)" {
		t.Errorf("enable = %q, want between(t,2.000,5.000)", win.enable)
	}
	if win.alpha != "" {
		t.Errorf("alpha = %q, want empty (no fade requested)", win.alpha)
	}
}

func TestBuildWindowExplicitWithFade(t *testing.T) {
	win := buildWindow(Spec{Start: 2, End: 5, Fade: 0.5})
	if win.enable != "between(t,2.000,5.000)" {
		t.Errorf("enable = %q, want between(t,2.000,5.000)", win.enable)
	}
	if win.alpha == "" {
		t.Errorf("alpha empty, want a fade expression")
	}
	if !strings.Contains(win.alpha, "(t-2.000)") {
		t.Errorf("alpha = %q, want it to reference (t-2.000)", win.alpha)
	}
}

func TestBuildWindowInterval(t *testing.T) {
	win := buildWindow(Spec{Every: 10, Duration: 3})
	want := "lt(mod(t,10.000),3.000)"
	if win.enable != want {
		t.Errorf("enable = %q, want %q", win.enable, want)
	}
}

func TestBuildWindowIntervalWithCount(t *testing.T) {
	win := buildWindow(Spec{Every: 10, Duration: 3, Count: 4})
	if !strings.Contains(win.enable, "lt(t,40.000)") {
		t.Errorf("enable = %q, want it bounded by lt(t,40.000)", win.enable)
	}
}

func TestEscapeFilterValue(t *testing.T) {
	got := escapeFilterValue(`C:\fonts\a:b.ttf`)
	want := `C\:\\fonts\\a\:b.ttf`
	if got != want {
		t.Errorf("escapeFilterValue = %q, want %q", got, want)
	}
}

func TestBuildFilterWritesAndCleansUpTextFiles(t *testing.T) {
	fontFile := writeTempFontFile(t)

	specs := []Spec{
		{Text: "hello", Start: 1, End: 3},
		{Text: "world", Every: 5, Duration: 2},
	}
	filter, cleanup, err := BuildFilter(specs, fontFile)
	if err != nil {
		t.Fatalf("BuildFilter: unexpected error: %v", err)
	}

	parts := strings.Split(filter, ",drawtext=")
	if len(parts) != 2 {
		t.Fatalf("expected 2 drawtext filters joined by comma, got filter: %q", filter)
	}

	var textFiles []string
	for _, part := range strings.Split(filter, ":") {
		if strings.HasPrefix(part, "textfile=") {
			textFiles = append(textFiles, strings.TrimPrefix(part, "textfile="))
		}
	}
	if len(textFiles) != 2 {
		t.Fatalf("expected 2 textfile= entries in filter, got %v (filter=%q)", textFiles, filter)
	}
	for _, tf := range textFiles {
		if _, err := os.Stat(tf); err != nil {
			t.Errorf("expected text file %q to exist before cleanup: %v", tf, err)
		}
	}

	cleanup()

	for _, tf := range textFiles {
		if _, err := os.Stat(tf); err == nil {
			t.Errorf("expected text file %q to be removed after cleanup", tf)
		}
	}
}

func TestBuildFilterMissingFont(t *testing.T) {
	specs := []Spec{{Text: "hi", Start: 1, End: 3}}
	_, _, err := BuildFilter(specs, "")
	if err == nil {
		t.Fatal("expected error when no font file is available")
	}
}

func writeTempFontFile(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "vidsplash-test-font-*.ttf")
	if err != nil {
		t.Fatalf("creating temp font file: %v", err)
	}
	defer f.Close()
	name := f.Name()
	t.Cleanup(func() { os.Remove(name) })
	return name
}
