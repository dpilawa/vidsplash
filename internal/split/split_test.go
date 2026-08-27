package split

import (
	"testing"
)

func TestParseTimestamp(t *testing.T) {
	cases := []struct {
		in      string
		want    float64
		wantErr bool
	}{
		{"5", 5, false},
		{"5.5", 5.5, false},
		{"1:30", 90, false},
		{"01:30", 90, false},
		{"1:00:00", 3600, false},
		{"1:02:03.5", 3723.5, false},
		{"", 0, true},
		{"abc", 0, true},
		{"1:2:3:4", 0, true},
		{"1:", 0, true},
	}
	for _, c := range cases {
		got, err := ParseTimestamp(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseTimestamp(%q): expected error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTimestamp(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseTimestamp(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSegmentsFromCutPoints(t *testing.T) {
	segs, err := SegmentsFromCutPoints([]float64{4, 2, 2}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Segment{{0, 2}, {2, 4}, {4, 10}}
	if len(segs) != len(want) {
		t.Fatalf("got %d segments, want %d: %+v", len(segs), len(want), segs)
	}
	for i := range want {
		if segs[i] != want[i] {
			t.Errorf("segment %d = %+v, want %+v", i, segs[i], want[i])
		}
	}
}

func TestSegmentsFromCutPointsOutOfRange(t *testing.T) {
	cases := [][]float64{
		{0},
		{10},
		{-1},
		{11},
	}
	for _, cuts := range cases {
		if _, err := SegmentsFromCutPoints(cuts, 10); err == nil {
			t.Errorf("cuts %v: expected error", cuts)
		}
	}
}

func TestSegmentsFromCutPointsEmpty(t *testing.T) {
	segs, err := SegmentsFromCutPoints(nil, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(segs) != 1 || segs[0] != (Segment{0, 10}) {
		t.Fatalf("got %+v, want single [0,10] segment", segs)
	}
}
