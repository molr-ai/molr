package files_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/storage/files"
)

func TestDeletionNotificationCommitsWithMetadata(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	db, err := sqlitestore.Open(filepath.Join(root, "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	objects, err := files.NewObjects(filepath.Join(root, "files"), db)
	if err != nil {
		t.Fatal(err)
	}
	defer objects.Close()
	events := realtime.NewStore(db)
	owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	service := files.NewService(db, objects, files.Options{Authorizer: fileAuthorizer{}, Deleted: func(ctx context.Context, file files.File) error {
		return events.Append(ctx, realtime.Visible{ID: "deleted", OwnerID: file.OwnerID, Topic: "file.deleted", Object: core.Object{Kind: "file", ID: file.ID}, CreatedAt: time.Now().UTC()})
	}})
	file, err := service.Upload(ctx, owner, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "upload"}, Name: "test.txt", Size: 4, Body: strings.NewReader("test")})
	if err != nil {
		t.Fatal(err)
	}
	watch, err := events.Watch(owner.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	db.FailNextCommit(errors.New("rollback"))
	if err := service.Delete(ctx, owner, file.ID); err == nil {
		t.Fatal("expected commit failure")
	}
	download, err := service.Open(ctx, owner, file.ID)
	if err != nil {
		t.Fatalf("rollback lost file: %v", err)
	}
	download.Body.Close()
	page, err := events.Replay(ctx, owner.OwnerID, realtime.Query{})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("rollback published event: %+v %v", page, err)
	}
	select {
	case <-watch.Changes():
		t.Fatal("rollback woke observer")
	default:
	}
	if err := service.Delete(ctx, owner, file.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-watch.Changes():
	default:
		t.Fatal("committed deletion did not wake observer")
	}
	page, err = events.Replay(ctx, owner.OwnerID, realtime.Query{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Object.ID != file.ID || page.Items[0].Topic != "file.deleted" {
		t.Fatalf("missing deletion event: %+v %v", page, err)
	}
	if _, err := service.Get(ctx, owner, file.ID); core.CodeOf(err) != core.NotFound {
		t.Fatalf("deleted metadata accessible: %v", err)
	}
	if err := service.Delete(ctx, owner, file.ID); err == nil {
		t.Fatal("duplicate deletion should fail")
	}
	page, err = events.Replay(ctx, owner.OwnerID, realtime.Query{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("duplicate event: %+v %v", page, err)
	}
}
