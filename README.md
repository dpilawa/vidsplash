# vidsplash

A small video-editing toolkit built on ffmpeg: add splash screens, combine clips, split videos, and add Snapchat/Instagram-style caption overlays.

## Requirements

- Go 1.22+
- ffmpeg with `drawtext` support (needed for `caption`; requires a build with libfreetype)

Homebrew's default `ffmpeg` formula excludes libfreetype/libass, so `caption` will fail with `No such filter: 'drawtext'`. Use the fuller build instead:

```bash
brew uninstall ffmpeg   # if you have the homebrew/core build installed
brew install homebrew-ffmpeg/ffmpeg/ffmpeg
```

`splash`, `concat`, and `split` work with any ffmpeg build.

## Build

```bash
go build -o vidsplash .
```

## Install

```bash
go install .
```

Installs to `$(go env GOPATH)/bin`. Add that to your PATH if needed:

```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zprofile
```

## Usage

```
vidsplash <command> [flags]
```

Commands: `splash`, `concat`, `split`, `caption`.

### Persistent flags (all commands)

| Flag | Default | Description |
|---|---|---|
| `--overwrite` | | Overwrite output if it exists |
| `-v, --verbose` | | Print raw ffmpeg output instead of TUI |
| `--ffmpeg` | `ffmpeg` | Path to ffmpeg binary |
| `--ffprobe` | `ffprobe` | Path to ffprobe binary |

---

## `vidsplash splash`

Add a splash screen image to the beginning, end, or both ends of a video.

```
vidsplash splash [flags] INPUT_VIDEO SPLASH_IMAGE OUTPUT_VIDEO
```

| Flag | Default | Description |
|---|---|---|
| `-p, --position` | `prepend` | `prepend`, `append`, or `both` |
| `-d, --duration` | `3.0` | Splash duration in seconds |
| `--fade-outer` | `0.5` | Fade on the edge facing away from the video (0 = off) |
| `--fade-inner` | `0.5` | Fade on the edge facing towards the video (0 = off) |
| `-b, --bg-color` | `black` | Background color behind the image (any ffmpeg color string) |
| `--video-fade-in` | `0` | Fade-in the main video (seconds, 0 = off) |
| `--video-fade-out` | `0` | Fade-out the main video (seconds, 0 = off) |
| `--no-audio-fade` | | Disable audio fade in/out on the main video |

```bash
# Prepend a 3s splash with default fades
vidsplash splash input.mp4 logo.png output.mp4

# Append with no inner fade (hard cut to video)
vidsplash splash --position append --fade-inner 0 input.mp4 logo.png output.mp4

# Wrap both ends, 5s each, no fades at all
vidsplash splash --position both --duration 5 --fade-outer 0 --fade-inner 0 input.mp4 logo.png output.mp4
```

The splash image is centered on a solid background and scaled to match the video resolution (letterboxed/pillarboxed as needed).

---

## `vidsplash concat`

Combine multiple videos and images into one output. Every item is normalized to a common resolution/fps/audio format, then losslessly joined.

```
vidsplash concat [flags] FILE...
vidsplash concat --config concat.json -o output.mp4
```

| Flag | Default | Description |
|---|---|---|
| `-o, --output` | | Output video path (required) |
| `-c, --config` | | JSON config file (alternative to positional args) |
| `--image-duration` | `3.0` | Duration for image items given positionally (seconds) |
| `-b, --bg-color` | `black` | Default background/pad color |
| `--width`, `--height` | | Target resolution (defaults to the first video item's) |
| `--fps` | | Target frame rate, e.g. `30/1` (defaults to the first video item's) |
| `--no-audio` | | Strip audio from the output |

```bash
# Simple: videos and images positionally, images use --image-duration
vidsplash concat clip1.mp4 logo.png clip2.mp4 -o out.mp4

# Per-item control via JSON config
vidsplash concat --config concat.json -o out.mp4
```

`concat.json`:

```json
{
  "target": { "width": 1080, "height": 1920, "fps": "30/1" },
  "items": [
    { "video": "clip1.mp4", "fade_in": 0.3 },
    { "image": "logo.png", "duration": 3, "bg_color": "black", "fade_in": 0.5, "fade_out": 0.5 },
    { "video": "clip2.mp4" }
  ]
}
```

`target` is optional (defaults to the first video item's resolution/fps). Audio format is always derived from the clips themselves, not configurable. `image` items require `duration`; `video` items ignore it.

---

## `vidsplash split`

Split one video into multiple files at given timestamps.

```
vidsplash split [flags] INPUT_VIDEO
```

| Flag | Default | Description |
|---|---|---|
| `--at` | | Comma-separated cut points, e.g. `00:30,01:15,90` (splits into N+1 segments) |
| `--segments` | | Comma-separated `start-end` ranges, e.g. `0:00-0:30,0:30-end` |
| `--outdir` | `.` | Directory to write segments into (created if missing) |
| `--pattern` | `part-%03d.mp4` | printf-style filename pattern (1-indexed) |
| `--reencode` | | Re-encode for frame-accurate cuts (default: fast stream-copy snapped to keyframes) |

Timestamps accept `SS`, `SS.sss`, `MM:SS`, or `HH:MM:SS(.sss)`; `end`/`END` in `--segments` resolves to the video's total duration.

```bash
# Fast, keyframe-snapped cuts
vidsplash split input.mp4 --at 00:30,01:15 --outdir parts

# Frame-accurate cuts
vidsplash split input.mp4 --segments 0:00-0:30,0:30-end --reencode --outdir parts
```

---

## `vidsplash caption`

Add Snapchat/Instagram-style text overlays at specific timestamps or repeating intervals, using ffmpeg's `drawtext` filter with named style presets.

```
vidsplash caption INPUT_VIDEO -o OUTPUT_VIDEO [flags]
vidsplash caption INPUT_VIDEO -o OUTPUT_VIDEO --config captions.json
```

| Flag | Default | Description |
|---|---|---|
| `-o, --output` | | Output video path (required) |
| `-c, --config` | | JSON config file describing multiple captions |
| `--text` | | Caption text (simple single-caption mode) |
| `--start`, `--end` | | Explicit timing mode (seconds) |
| `--every`, `--duration` | | Interval mode: repeat every N seconds, shown for `--duration` seconds |
| `--count` | `0` | Number of repeats for interval mode (0 = until the video ends) |
| `--preset` | `caption-bar` | `caption-bar`, `centered-pill`, `top-banner`, `hook`, or `pop` |
| `--fade` | `0.3` | Fade in/out duration in seconds, 0 = no fade |
| `--font-size`, `--font-color`, `--font-file` | | Style overrides |
| `--position` | | `top`, `upper`, `center`, or `bottom` (overrides the preset default) |
| `--highlight` | | Word to put in a color box (`pop` preset). Or wrap `[[word]]` in the text |
| `--highlight-color` | `#5EEAD4` | Highlight box color as `#RRGGBB` |
| `--no-audio` | | Strip audio from the output |

Exactly one timing mode is required per caption: explicit (`start`/`end`) or interval (`every`/`duration`, optionally `count`).

```bash
# One caption, explicit window
vidsplash caption input.mp4 -o output.mp4 --text "Swipe up!" --start 1 --end 4 --preset caption-bar

# Repeating caption every 10s for 3s, 4 times
vidsplash caption input.mp4 -o output.mp4 --text "New drop" --preset top-banner --every 10 --duration 3 --count 4
```

`captions.json`:

```json
{
  "captions": [
    { "text": "Swipe up!", "start": 2.0, "end": 5.0, "preset": "caption-bar", "fade": 0.3 },
    { "text": "New drop", "preset": "top-banner", "every": 10, "duration": 3, "count": 4 }
  ]
}
```

Presets:

- `caption-bar` — bottom, white text on a semi-transparent black bar
- `centered-pill` — centered, padded box
- `top-banner` — top, full-width bar
- `hook` — large boxed text in the upper third
- `pop` — bold all-caps white text with a black outline and optional `[[highlight]]` box

Without `--font-file`, a system font is used (Windows: Arial Black; macOS: Arial/Helvetica). If none is found, pass `--font-file` explicitly.
