package files_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/storage/files"
)

type pausedPublicationObjects struct {
	files.Objects
	entered, resume chan struct{}
}

func (o *pausedPublicationObjects) Publish(ctx context.Context, object files.StagedObject, publish func(func() error) error) error {
	close(o.entered)
	<-o.resume
	return o.Objects.Publish(ctx, object, publish)
}

func TestUploadPreparationDoesNotHoldDatabaseTransaction(t *testing.T) {
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
	paused := &pausedPublicationObjects{Objects: objects, entered: make(chan struct{}), resume: make(chan struct{})}
	service := files.NewService(db, paused, files.Options{Authorizer: fileAuthorizer{}})
	done := make(chan error, 1)
	go func() {
		_, err := service.Upload(t.Context(), core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}, upload("publication", []byte("verified bytes")))
		done <- err
	}()
	<-paused.entered
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	readErr := db.Read(ctx, func(context.Context, persistence.Tx) error { return nil })
	close(paused.resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if readErr != nil {
		t.Fatal("publication preparation blocked unrelated reads", readErr)
	}
}

func TestPublicationHoldsObjectLockAndDefersRenameUntilCommit(t *testing.T) {
	directory := t.TempDir()
	objects, err := files.NewObjects(directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer objects.Close()
	staged, err := objects.Put(t.Context(), "object", strings.NewReader("verified"), 8)
	if err != nil {
		t.Fatal(err)
	}
	var escaped func() error
	err = objects.Publish(t.Context(), staged, func(commit func() error) error {
		escaped = commit
		if body, err := objects.Open(t.Context(), staged.ID); err == nil {
			body.Close()
			t.Fatal("published before commit")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
		defer cancel()
		if err := objects.Remove(ctx, staged.ID); core.CodeOf(err) != core.DeadlineExceeded {
			t.Fatal("publication lost object lock", err)
		}
		if err := commit(); err != nil {
			return err
		}
		return commit()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := escaped(); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal("commit escaped lock lifetime", err)
	}
	if err := objects.Remove(t.Context(), staged.ID); err != nil {
		t.Fatal("lock was retained", err)
	}
}

func TestPublicationIntegrityAndCallbackFailurePrecedence(t *testing.T) {
	directory := t.TempDir()
	objects, err := files.NewObjects(directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer objects.Close()
	staged, err := objects.Put(t.Context(), "object", strings.NewReader("original"), 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "staging", "object"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	denied := core.NewError(core.Forbidden, "fixture authorization denied")
	if err := objects.Publish(t.Context(), staged, func(func() error) error { return denied }); !errors.Is(err, denied) {
		t.Fatal("integrity changed authorization precedence", err)
	}
	err = objects.Publish(t.Context(), staged, func(commit func() error) error {
		// Repair after preparation: commit must return the already observed mismatch.
		if err := os.WriteFile(filepath.Join(directory, "staging", "object"), []byte("original"), 0600); err != nil {
			return err
		}
		return commit()
	})
	if core.CodeOf(err) != core.Conflict {
		t.Fatal("verification did not precede metadata callback", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "objects", "object")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed verification renamed object", err)
	}
	if err := objects.Publish(t.Context(), staged, func(commit func() error) error { return commit() }); err != nil {
		t.Fatal(err)
	}
}
