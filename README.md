# vidsplash

Add a splash screen image to the beginning, end, or both ends of a video file.

## Requirements

- Go 1.22+
- ffmpeg (`brew install ffmpeg`)

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
vidsplash [flags] INPUT_VIDEO SPLASH_IMAGE OUTPUT_VIDEO
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `-p, --position` | `prepend` | `prepend`, `append`, or `both` |
| `-d, --duration` | `3.0` | Splash duration in seconds |
| `--fade-outer` | `0.5` | Fade on the edge facing away from the video (0 = off) |
| `--fade-inner` | `0.5` | Fade on the edge facing towards the video (0 = off) |
| `-b, --bg-color` | `black` | Background color behind the image (any ffmpeg color string) |
| `--video-fade-in` | `0` | Fade-in the main video (seconds, 0 = off) |
| `--video-fade-out` | `0` | Fade-out the main video (seconds, 0 = off) |
| `--overwrite` | | Overwrite output if it exists |
| `-v, --verbose` | | Print raw ffmpeg output instead of TUI |

### Examples

```bash
# Prepend a 3s splash with default fades
vidsplash input.mp4 logo.png output.mp4

# Append with no inner fade (hard cut to video)
vidsplash --position append --fade-inner 0 input.mp4 logo.png output.mp4

# Wrap both ends, 5s each, no fades at all
vidsplash --position both --duration 5 --fade-outer 0 --fade-inner 0 input.mp4 logo.png output.mp4

# Dark background, fade the video in too
vidsplash --bg-color "#1a1a2e" --video-fade-in 0.5 input.mp4 logo.png output.mp4
```

The splash image is centered on a solid background and scaled to match the video resolution (letterboxed/pillarboxed as needed).
