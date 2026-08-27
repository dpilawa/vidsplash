package cmd

import (
	"testing"

	"github.com/dpilawa/vidsplash/internal/split"
)

func TestParseSegmentRanges(t *testing.T) {
	segs, err := parseSegmentRanges("0:00-0:30,0:30-end", 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []split.Segment{{Start: 0, End: 30}, {Start: 30, End: 60}}
	if len(segs) != len(want) {
		t.Fatalf("got %d segments, want %d: %+v", len(segs), len(want), segs)
	}
	for i := range want {
		if segs[i] != want[i] {
			t.Errorf("segment %d = %+v, want %+v", i, segs[i], want[i])
		}
	}
}

func TestParseSegmentRangesErrors(t *testing.T) {
	cases := []string{
		"not-a-range-either",
		"10-5",
		"0:00-1:30-2:00",
		"-1-10",
		"0-100",
	}
	for _, raw := range cases {
		if _, err := parseSegmentRanges(raw, 60); err == nil {
			t.Errorf("parseSegmentRanges(%q): expected error", raw)
		}
	}
}

func TestParseSegmentRangesCaseInsensitiveEnd(t *testing.T) {
	segs, err := parseSegmentRanges("0:00-END", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(segs) != 1 || segs[0].End != 42 {
		t.Fatalf("got %+v, want end resolved to total duration 42", segs)
	}
}
