package model

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

const maxProviderStreamBytes = 8 << 20

// sseFrames only handles data framing and bounded raw evidence. Protocol readers
// decide when to stop and how to interpret frames and transport failures.
// The capture belongs to this reader and is never pooled or reused after return.
type sseFrames struct {
	scanner  *bufio.Scanner
	captured bytes.Buffer
}

func newSSEFrames(reader io.Reader) *sseFrames {
	frames := &sseFrames{}
	frames.scanner = bufio.NewScanner(io.TeeReader(io.LimitReader(reader, maxProviderStreamBytes+1), &frames.captured))
	frames.scanner.Buffer(make([]byte, 4096), maxProviderStreamBytes+1)
	return frames
}

func (frames *sseFrames) next() (string, bool) {
	var data []string
	for frames.scanner.Scan() {
		if frames.captured.Len() > maxProviderStreamBytes {
			return "", false
		}
		line := frames.scanner.Text()
		if line == "" {
			if len(data) > 0 {
				return strings.Join(data, "\n"), true
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	// Preserve a final frame without a trailing blank line, but never process
	// partial data after a read failure or an exceeded capture limit.
	if frames.failed() || len(data) == 0 {
		return "", false
	}
	return strings.Join(data, "\n"), true
}

func (frames *sseFrames) failed() bool {
	return frames.scanner.Err() != nil || frames.captured.Len() > maxProviderStreamBytes
}
