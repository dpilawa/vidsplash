package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes ffmpeg commands and delivers progress events.
type Runner interface {
	Run(ctx context.Context, args []string, onProgress func(ProgressEvent)) error
}

// ExecRunner runs real ffmpeg subprocesses.
type ExecRunner struct {
	FFmpegPath string
}

func (r *ExecRunner) Run(ctx context.Context, args []string, onProgress func(ProgressEvent)) error {
	cmd := exec.CommandContext(ctx, r.FFmpegPath, args...)

	progressPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting ffmpeg: %w", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(progressPipe)
		var block []string
		for scanner.Scan() {
			line := scanner.Text()
			block = append(block, line)
			if strings.HasPrefix(line, "progress=") {
				if onProgress != nil {
					onProgress(ParseProgressBlock(block))
				}
				block = block[:0]
			}
		}
	}()

	<-done
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg: %w\n%s", err, stderrBuf.String())
	}
	return nil
}
