// Package voicecodec decodes Weixin SILK in a bounded, capability-free WASM guest.
package voicecodec

import (
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
	"time"

	"github.com/tetratelabs/wazero"
)

//go:embed silk.wasm
var module []byte

// WAV converts at most two minutes of mono SILK to PCM WAV. No guest filesystem,
// environment, network, stdin or stdout is exposed. A fresh instance owns memory.
func WAV(ctx context.Context, data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > 4<<20 {
		return nil, errors.New("invalid voice size")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(512).WithCloseOnContextDone(true))
	defer runtime.Close(context.Background())
	guest, err := runtime.InstantiateWithConfig(ctx, module, wazero.NewModuleConfig().WithStartFunctions("_initialize"))
	if err != nil {
		return nil, errors.New("voice decoder unavailable")
	}
	const outputLimit = 24000 * 2 * 120
	malloc := guest.ExportedFunction("malloc")
	in, err := malloc.Call(ctx, uint64(len(data)))
	if err != nil || len(in) != 1 || in[0] == 0 {
		return nil, errors.New("voice allocation failed")
	}
	out, err := malloc.Call(ctx, outputLimit)
	if err != nil || len(out) != 1 || out[0] == 0 {
		return nil, errors.New("voice allocation failed")
	}
	if !guest.Memory().Write(uint32(in[0]), data) {
		return nil, errors.New("voice input exceeds memory")
	}
	result, err := guest.ExportedFunction("decode_silk").Call(ctx, in[0], uint64(len(data)), out[0], outputLimit)
	if err != nil || len(result) != 1 || int32(result[0]) <= 0 || result[0] > outputLimit {
		return nil, errors.New("invalid or oversized SILK voice")
	}
	pcm, ok := guest.Memory().Read(uint32(out[0]), uint32(result[0]))
	if !ok {
		return nil, errors.New("invalid voice output")
	}
	wav := make([]byte, 44+len(pcm))
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 24000)
	binary.LittleEndian.PutUint32(wav[28:], 48000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(pcm)))
	copy(wav[44:], pcm)
	return wav, nil
}
