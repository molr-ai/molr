package sandbox

import (
	"context"
	"testing"
)

func TestAutomaticPreparationWithoutPrivateDirectoryFailsClosed(t *testing.T) {
	r := New(Options{QuotaStore: quotaTestStore(t)})
	defer r.Close()
	if err := r.Prepare(context.Background()); err == nil {
		t.Fatal("automatic sandbox accepted missing private directory")
	}
	caps, err := r.Capabilities(context.Background())
	if err != nil || caps.Shell {
		t.Fatalf("failed bootstrap advertised shell: %+v %v", caps, err)
	}
}
