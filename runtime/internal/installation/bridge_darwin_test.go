package installation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMacComponentWorkerLockHandoff(t *testing.T) {
	_, r := fixture(t)
	bin := filepath.Join(r.Paths.Program, "bin")
	if err := os.Mkdir(bin, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(r.Paths.Cache, "worker-started")
	// The shell stands in for setup after the parent's verified launch. Inherited
	// descriptor 3 must keep the instance lock held even after parent cancellation.
	script := "#!/bin/sh\nprintf started > '" + marker + "'\n/bin/sleep 1\n"
	if err := os.WriteFile(filepath.Join(bin, "molr-setup"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	lock, err := lockRecord(r)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err = startMaintenanceWorker(ctx, r, "fixture", nil, lock); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	cancel()
	lock.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err = os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker canceled with parent")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if second, err := lockRecord(r); err == nil {
		second.Close()
		t.Fatal("worker lost inherited lock")
	}
	for {
		if second, err := lockRecord(r); err == nil {
			second.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker retained lock after exit")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !runnerAvailable(t.Context(), false) || runnerAvailable(t.Context(), true) {
		t.Fatal("incorrect macOS maintenance scope")
	}
}
