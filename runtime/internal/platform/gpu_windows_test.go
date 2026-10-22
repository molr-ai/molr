package platform

import (
	"context"
	"testing"
)

func TestWindowsNVIDIAParsing(t *testing.T) {
	const id = "GPU-00000000-0000-0000-0000-000000000001"
	const row = id + ", Example GPU, 16376, 25, 4096\r\n"
	devices := parseWindowsNVIDIA([]byte(row))
	if len(devices) != 1 {
		t.Fatalf("inventory: %+v", devices)
	}
	g := devices[0]
	if g.MemoryBytes != 16376<<20 || g.UtilizationPercent == nil || *g.UtilizationPercent != 25 || g.MemoryUsedBytes == nil || *g.MemoryUsedBytes != 4096<<20 || g.Available || g.Reason != "gpu_platform_unsupported" {
		t.Fatalf("telemetry or authorization: %+v", g)
	}
	for _, metrics := range []string{"N/A, N/A, N/A", "-1, NaN, -1", "9223372036854775807, Inf, 9223372036854775807"} {
		devices = parseWindowsNVIDIA([]byte(id + ", Example GPU, " + metrics))
		if len(devices) != 1 || devices[0].MemoryBytes != 0 || devices[0].UtilizationPercent != nil || devices[0].MemoryUsedBytes != nil {
			t.Fatalf("unsupported counters became known: %+v", devices)
		}
	}
	g = parseWindowsNVIDIA([]byte(id + ", Example GPU, 16, 0, 17"))[0]
	if g.UtilizationPercent == nil || *g.UtilizationPercent != 0 || g.MemoryUsedBytes != nil {
		t.Fatal("zero usage or impossible memory mishandled")
	}
	for _, invalid := range []string{row + row, "bad, GPU, 1, 1, 1", id + ", , 1, 1, 1", id + ", GPU, 1, 1", "\"unterminated"} {
		if len(parseWindowsNVIDIA([]byte(invalid))) != 0 {
			t.Fatalf("accepted malformed inventory: %q", invalid)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if len(windowsNVIDIASnapshot(ctx)) != 0 {
		t.Fatal("canceled probe returned devices")
	}
}
