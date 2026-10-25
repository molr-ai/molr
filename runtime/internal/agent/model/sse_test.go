package model

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestSSEFramingAndRawCapture(t *testing.T) {
	body := ": heartbeat\r\n\r\nevent: ignored\r\ndata: first\r\ndata:  second\r\n\r\ndata:\n\n: ignored\n\ndata: final"
	frames := newSSEFrames(iotest.OneByteReader(strings.NewReader(body)))
	for _, want := range []string{"first\n second", "", "final"} {
		got, ok := frames.next()
		if !ok || got != want {
			t.Fatalf("frame = %q, %v; want %q", got, ok, want)
		}
	}
	if _, ok := frames.next(); ok || frames.failed() || frames.captured.String() != body {
		t.Fatal("lost framing or raw evidence")
	}
}

func TestSSECaptureLimitAndReadFailure(t *testing.T) {
	for _, extra := range []int{0, 1, 2} {
		// Both a long line and many short lines must obey the total capture limit.
		for _, repeated := range []string{"x", ":\n"} {
			size := maxProviderStreamBytes + extra
			body := strings.Repeat(repeated, size/len(repeated)) + strings.Repeat("x", size%len(repeated))
			frames := newSSEFrames(strings.NewReader(body))
			for {
				if _, ok := frames.next(); !ok {
					break
				}
			}
			want := min(len(body), maxProviderStreamBytes+1)
			if frames.failed() != (extra > 0) || frames.captured.Len() != want || !bytes.Equal(frames.captured.Bytes(), []byte(body[:want])) {
				t.Fatalf("capture size=%d, failed=%v; input=%d", frames.captured.Len(), frames.failed(), size)
			}
		}
	}
	body := "data: incomplete"
	frames := newSSEFrames(io.MultiReader(strings.NewReader(body), iotest.ErrReader(io.ErrUnexpectedEOF)))
	if _, ok := frames.next(); ok || !frames.failed() || frames.captured.String() != body {
		t.Fatal("accepted partial frame after transport failure")
	}
}

func TestProviderStreamFramingCompletionAndEvidence(t *testing.T) {
	chat := "data: {\"id\":\"s\",\n" + `data: "choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}` + "\n\ndata: [DONE]\n\n"
	responses := `data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n\n"
	for _, tc := range []struct {
		protocol Protocol
		body     string
		stops    bool
	}{
		{OpenAIChat, chat, true}, {AnthropicMessages, anthropicToolStream(), true}, {OpenAIResponses, responses, false},
	} {
		t.Run(string(tc.protocol), func(t *testing.T) {
			for _, body := range []string{tc.body, strings.TrimRight(tc.body, "\n"), strings.ReplaceAll(tc.body, "\n", "\r\n")} {
				raw, completion, _, err := readProviderStream(tc.protocol, iotest.OneByteReader(strings.NewReader(body)), time.Now(), time.Now)
				if err != nil || string(raw) != body || len(completion) == 0 {
					t.Fatalf("framing: %v", err)
				}
				// The returned capture survives subsequent independent reads.
				_, _, _, _ = readProviderStream(tc.protocol, strings.NewReader("data: broken\n\n"), time.Now(), time.Now)
				if string(raw) != body {
					t.Fatal("returned evidence was reused")
				}
			}
			tail := "data: {}\n\n"
			raw, _, _, err := readProviderStream(tc.protocol, iotest.OneByteReader(strings.NewReader(tc.body+tail)), time.Now(), time.Now)
			if (err == nil) != tc.stops {
				t.Fatalf("completion policy changed: %v", err)
			}
			want := tc.body
			if !tc.stops {
				want += tail
			}
			if string(raw) != want {
				t.Fatal("incorrect raw evidence at completion")
			}
			broken := "data: broken\n\n"
			raw, completion, timing, err := readProviderStream(tc.protocol, strings.NewReader(broken), time.Now(), time.Now)
			if err == nil || completion != nil || timing == nil || string(raw) != broken {
				t.Fatal("lost failure evidence")
			}
			oversized := strings.Repeat(":\n", maxProviderStreamBytes/2) + tc.body
			raw, completion, _, err = readProviderStream(tc.protocol, strings.NewReader(oversized), time.Now(), time.Now)
			if err == nil || completion != nil || len(raw) != maxProviderStreamBytes+1 {
				t.Fatal("accepted oversized stream or lost bounded evidence")
			}
		})
	}
}
