package ffmpeg

import (
	"strconv"
	"strings"
)

// ProgressEvent holds parsed values from one ffmpeg -progress block.
type ProgressEvent struct {
	OutTimeUS int64
	FPS       float64
	Speed     string
	Done      bool
}

// ParseProgressBlock parses a slice of "key=value" lines from ffmpeg -progress output.
func ParseProgressBlock(lines []string) ProgressEvent {
	var e ProgressEvent
	for _, line := range lines {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "out_time_us":
			e.OutTimeUS, _ = strconv.ParseInt(v, 10, 64)
		case "fps":
			e.FPS, _ = strconv.ParseFloat(v, 64)
		case "speed":
			e.Speed = v
		case "progress":
			e.Done = v == "end"
		}
	}
	return e
}
