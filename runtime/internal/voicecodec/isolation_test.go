package voicecodec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWAVConcurrentDecodesOwnTheirOutput(t *testing.T) {
	data, err := os.ReadFile("testdata/tone.silk")
	if err != nil {
		t.Fatal(err)
	}
	reference, err := os.ReadFile("testdata/pcm.sha256")
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.Clone(data)
	const workers = 8
	outputs := make([][]byte, workers)
	errs := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			<-start
			for round := 0; round < 3; round++ {
				wav, err := WAV(t.Context(), data)
				if err != nil {
					errs <- err
					return
				}
				if len(wav) < 44 {
					errs <- fmt.Errorf("short WAV: %d", len(wav))
					return
				}
				sum := sha256.Sum256(wav[44:])
				if hex.EncodeToString(sum[:]) != strings.TrimSpace(string(reference)) {
					errs <- fmt.Errorf("worker %d round %d: PCM mismatch", i, round)
					return
				}
				outputs[i] = wav
			}
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if !bytes.Equal(input, data) {
		t.Fatal("decoder modified caller input")
	}
	preserved := bytes.Clone(outputs[1])
	clear(outputs[0])
	if !bytes.Equal(outputs[1], preserved) {
		t.Fatal("decodes share returned storage")
	}
	later, err := WAV(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(later, preserved) {
		t.Fatal("modified earlier output changed later decode")
	}
}

func TestWAVFailedCallsDoNotPoisonLaterDecodes(t *testing.T) {
	data, err := os.ReadFile("testdata/tone.silk")
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	expired, stop := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer stop()
	var previous []byte
	for _, tc := range []struct {
		name string
		ctx  context.Context
		data []byte
	}{{"canceled", canceled, data}, {"expired", expired, data}, {"malformed", t.Context(), []byte("not silk")}} {
		t.Run(tc.name, func(t *testing.T) {
			if wav, err := WAV(tc.ctx, tc.data); err == nil || wav != nil {
				t.Fatal("failed call returned audio", err)
			}
			wav, err := WAV(t.Context(), data)
			if err != nil {
				t.Fatal("valid decode after failure", err)
			}
			if previous != nil && !bytes.Equal(previous, wav) {
				t.Fatal("failure changed later output")
			}
			previous = wav
		})
	}
}
