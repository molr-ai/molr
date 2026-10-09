package files_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/storage/files"
)

type pausedUploadReader struct {
	started chan struct{}
	release chan struct{}
	read    bool
}

func (r *pausedUploadReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	close(r.started)
	<-r.release
	return copy(p, "slow"), nil
}
func TestSlowUploadDoesNotBlockUnrelatedCommitOrDatabase(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "files.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	objects, err := files.NewObjects(filepath.Join(t.TempDir(), "objects"), db)
	if err != nil {
		t.Fatal(err)
	}
	defer objects.Close()
	staged, err := objects.Put(t.Context(), "ready", strings.NewReader("ready"), 5)
	if err != nil {
		t.Fatal(err)
	}
	slow := &pausedUploadReader{started: make(chan struct{}), release: make(chan struct{})}
	uploaded := make(chan error, 1)
	go func() { _, err := objects.Put(t.Context(), "slow", slow, 4); uploaded <- err }()
	<-slow.started
	entered, committed := make(chan struct{}), make(chan error, 1)
	go func() {
		committed <- db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			close(entered)
			return objects.Commit(ctx, staged)
		})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	readErr := db.Read(ctx, func(context.Context, persistence.Tx) error { return nil })
	var commitErr error
	completed := false
	select {
	case commitErr = <-committed:
		completed = true
	case <-ctx.Done():
	}
	// Release the stream before reporting failures, so cleanup never strands a writer.
	close(slow.release)
	if err := <-uploaded; err != nil {
		t.Fatal(err)
	}
	if !completed {
		commitErr = <-committed
	}
	if commitErr != nil {
		t.Fatal(commitErr)
	}
	if readErr != nil || !completed {
		t.Fatalf("slow upload blocked unrelated publication/database: commit_completed=%v read_error=%v", completed, readErr)
	}
	reader, err := objects.Open(t.Context(), staged.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil || string(content) != "ready" {
		t.Fatal(string(content), err)
	}
}

func TestObjectWaitersCancelWithoutChangingActiveUpload(t *testing.T) {
	objects, err := files.NewObjects(filepath.Join(t.TempDir(), "objects"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer objects.Close()
	slow := &pausedUploadReader{started: make(chan struct{}), release: make(chan struct{})}
	uploaded := make(chan error, 1)
	go func() { _, err := objects.Put(t.Context(), "shared", slow, 4); uploaded <- err }()
	<-slow.started
	sum := sha256.Sum256([]byte("slow"))
	staged := files.StagedObject{ID: "shared", Size: 4, SHA256: hex.EncodeToString(sum[:])}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	results := make(chan error, 3)
	go func() { _, err := objects.Put(ctx, "shared", strings.NewReader("bad"), 3); results <- err }()
	go func() { results <- objects.Commit(ctx, staged) }()
	go func() { results <- objects.Remove(ctx, "shared") }()
	var waiting []error
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
collect:
	for len(waiting) < 3 {
		select {
		case err := <-results:
			waiting = append(waiting, err)
		case <-timeout.C:
			break collect
		}
	}
	completed := len(waiting)
	close(slow.release)
	if err := <-uploaded; err != nil {
		t.Fatal(err)
	}
	for len(waiting) < 3 {
		waiting = append(waiting, <-results)
	}
	if completed != 3 {
		t.Fatal("canceled waiters remained blocked until the upload ended")
	}
	for _, err := range waiting {
		if core.CodeOf(err) != core.DeadlineExceeded {
			t.Fatal("waiter changed active object", err)
		}
	}
	if err := objects.Commit(t.Context(), staged); err != nil {
		t.Fatal(err)
	}
	reader, err := objects.Open(t.Context(), "shared")
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(content) != "slow" {
		t.Fatal(string(content), err)
	}
	if err := objects.Remove(t.Context(), "shared"); err != nil {
		t.Fatal(err)
	}
	if _, err := objects.Put(t.Context(), "shared", strings.NewReader("new"), 3); err != nil {
		t.Fatal("canceled waiter retained stale lock", err)
	}
}

func TestSameObjectOperationsWaitForStreamingToFinish(t *testing.T) {
	for _, operation := range []string{"put", "commit", "remove"} {
		t.Run(operation, func(t *testing.T) {
			objects, err := files.NewObjects(filepath.Join(t.TempDir(), "objects"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer objects.Close()
			slow := &pausedUploadReader{started: make(chan struct{}), release: make(chan struct{})}
			uploaded := make(chan error, 1)
			go func() { _, err := objects.Put(t.Context(), "shared", slow, 4); uploaded <- err }()
			<-slow.started
			sum := sha256.Sum256([]byte("slow"))
			staged := files.StagedObject{ID: "shared", Size: 4, SHA256: hex.EncodeToString(sum[:])}
			done := make(chan error, 1)
			go func() {
				switch operation {
				case "put":
					_, err := objects.Put(t.Context(), "shared", strings.NewReader("replacement"), 11)
					done <- err
				case "commit":
					done <- objects.Commit(t.Context(), staged)
				case "remove":
					done <- objects.Remove(t.Context(), "shared")
				}
			}()
			var result error
			early := false
			select {
			case result = <-done:
				early = true
			case <-time.After(100 * time.Millisecond):
			}
			close(slow.release)
			if err := <-uploaded; err != nil {
				t.Fatal(err)
			}
			if !early {
				result = <-done
			}
			if early {
				t.Fatal("operation raced incomplete staging", result)
			}
			if operation == "put" {
				if core.CodeOf(result) != core.Conflict {
					t.Fatal(result)
				}
				if err := objects.Commit(t.Context(), staged); err != nil {
					t.Fatal(err)
				}
			} else if result != nil {
				t.Fatal(result)
			}
		})
	}
}

func TestConcurrentUploadsPreserveQuotaAndIdempotency(t *testing.T) {
	for _, sameKey := range []bool{true, false} {
		t.Run(fmt.Sprint(sameKey), func(t *testing.T) {
			f := fixture(t)
			f.SetQuota(10)
			type result struct {
				file files.File
				err  error
			}
			results := make(chan result, 8)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for i := 0; i < 8; i++ {
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					key := fmt.Sprint(i)
					if sameKey {
						key = "shared"
					}
					file, err := f.Service.Upload(t.Context(), f.Owner, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: key}, Name: "parallel.txt", Size: 6, Body: strings.NewReader("upload")})
					results <- result{file, err}
				}()
			}
			close(start)
			workers.Wait()
			close(results)
			successes := 0
			var id core.ID
			for r := range results {
				if r.err != nil {
					if sameKey || core.CodeOf(r.err) != core.FailedPrecondition {
						t.Fatal(r.err)
					}
					continue
				}
				successes++
				if id != "" && id != r.file.ID {
					t.Fatal("idempotency published duplicate files")
				}
				id = r.file.ID
			}
			expected := 1
			if sameKey {
				expected = 8
			}
			if successes != expected {
				t.Fatal("wrong quota/idempotency result", successes)
			}
			ready, committed, staged := f.Observe()
			if ready != 1 || committed != 1 || staged != 0 {
				t.Fatal("failed/repeated uploads left objects", ready, committed, staged)
			}
		})
	}
}
