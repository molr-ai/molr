package voicecodec

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
)

func TestWAVMatchesReferenceDecoderAndHasNoHostImports(t *testing.T) {
	data, err := os.ReadFile("testdata/tone.silk")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/pcm.sha256")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range [][]byte{data, data[1:]} {
		wav, err := WAV(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || binary.LittleEndian.Uint32(wav[24:]) != 24000 {
			t.Fatal("invalid WAV header")
		}
		sum := sha256.Sum256(wav[44:])
		if hex.EncodeToString(sum[:]) != strings.TrimSpace(string(expected)) {
			t.Fatal("PCM differs from SILK SDK reference decoder")
		}
	}
	runtime := wazero.NewRuntime(t.Context())
	defer runtime.Close(t.Context())
	compiled, err := runtime.CompileModule(t.Context(), module)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.ImportedFunctions()) != 0 || len(compiled.ImportedMemories()) != 0 {
		t.Fatal("codec requires host capabilities")
	}
}
func TestWAVRejectsMalformedOversizedAndCanceledInput(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("not silk"), []byte("#!SILK_V3\x7f\x7fshort"), make([]byte, (4<<20)+1)} {
		if _, err := WAV(t.Context(), data); err == nil {
			t.Fatal("invalid voice accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := WAV(ctx, []byte("#!SILK_V3\x01\x00a")); err == nil {
		t.Fatal("canceled decode accepted")
	}
}
