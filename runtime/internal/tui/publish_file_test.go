package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishFilePreservesDestinationOnFailure(t *testing.T) {
	for _, scenario := range []string{"cancelled", "cancel_during_write", "write_error", "created_during_write", "changed_during_write"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "result.txt")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var expected os.FileInfo
			want := ""
			if scenario == "changed_during_write" {
				if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
				var err error
				expected, err = os.Stat(target)
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "cancelled" {
				cancel()
			}
			calls := 0
			err := publishFile(ctx, target, expected, func(w io.Writer) error {
				calls++
				if _, err := io.WriteString(w, "downloaded"); err != nil {
					return err
				}
				switch scenario {
				case "cancel_during_write":
					cancel()
				case "write_error":
					return errors.New("transfer failed")
				case "created_during_write", "changed_during_write":
					want = "concurrent edit"
					return os.WriteFile(target, []byte(want), 0600)
				}
				return nil
			})
			if err == nil {
				t.Fatal("publication unexpectedly succeeded")
			}
			if scenario == "cancelled" && calls != 0 {
				t.Fatal("cancelled operation invoked writer")
			}
			if (scenario == "cancelled" || scenario == "cancel_during_write") && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			data, readErr := os.ReadFile(target)
			if want == "" {
				if !errors.Is(readErr, os.ErrNotExist) {
					t.Fatalf("partial target exists: %q, %v", data, readErr)
				}
			} else if readErr != nil || string(data) != want {
				t.Fatalf("destination changed: %q, %v", data, readErr)
			}
			leftovers, err := filepath.Glob(filepath.Join(dir, ".molr-download-*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary files leaked: %v, %v", leftovers, err)
			}
		})
	}
}

func TestPublishFileCreatesAndReplacesConfirmedFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "result.txt")
	var expected os.FileInfo
	for _, content := range []string{"first", "replacement"} {
		if err := publishFile(t.Context(), target, expected, func(w io.Writer) error {
			_, err := io.WriteString(w, content)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(target)
		if err != nil || string(data) != content {
			t.Fatalf("unexpected file: %q, %v", data, err)
		}
		expected, err = os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
	}
}
