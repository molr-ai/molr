package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/storage/files"
	"molr.ai/runtime/tests/testkit"
)

// Bind the real file service, object store and SQL transaction adapter. The
// primary owner/session is active. A second trusted test identity has no access
// to the first owner's objects (not a second public account-registration flow).
// FailMetadataOnce/FailObjectCommitOnce inject dependency failures, not SUT return
// values. These hooks fail before their write can commit, so Upload can confirm
// abort; no committed resumable upload intent remains. They do not represent an
// ambiguous crash after commit, for which recovery may instead roll forward.
// Quotas are configured at construction. Observe counts actual persisted state.
// SeedSymlink creates a real filesystem link targeting an outside file.
// SeedUploadNameTarget creates a real sentinel outside object storage and returns
// a traversal-shaped upload name that would address it if used as a path. Its
// reader must read that actual file; it cannot return a cached expected value.
type contractFixture struct {
	Service              files.Service
	Objects              files.Objects
	Within               func(context.Context, func(files.Repository) error) error
	Owner                core.Principal
	Other                core.Principal
	MaxUpload            int64
	SetQuota             func(int64)
	FailMetadataOnce     func()
	FailObjectCommitOnce func()
	Observe              func() (readyMetadata, committedObjects, stagedObjects int)
	SeedSymlink          func(core.ID, []byte) func() []byte
	SeedUploadNameTarget func([]byte) (name string, readOutside func() []byte)
}

var newContractFixture func(*testing.T) contractFixture

func fixture(t *testing.T) contractFixture {
	t.Helper()
	return testkit.RequireFactory(t, "storage/files", newContractFixture)
}
func upload(key string, data []byte) files.UploadRequest {
	return files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: key}, Name: "report.txt", MediaType: "text/plain", Size: int64(len(data)), Body: bytes.NewReader(data)}
}
func denied(t *testing.T, err error) {
	t.Helper()
	if c := core.CodeOf(err); c != core.NotFound && c != core.Forbidden {
		t.Fatalf("expected access denial, got %v", err)
	}
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func TestTDDFilesUploadGetOpenDelete(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	body := []byte("actual file content")
	created, err := f.Service.Upload(ctx, f.Owner, upload("upload", body))
	testkit.RequireNoError(t, err)
	if created.ID == "" || created.OwnerID != f.Owner.OwnerID || created.State != files.Ready || created.Size != int64(len(body)) || created.SHA256 != digest(body) {
		t.Fatalf("incorrect published metadata: %+v", created)
	}
	fetched, err := f.Service.Get(ctx, f.Owner, created.ID)
	testkit.RequireNoError(t, err)
	if fetched.ID != created.ID || fetched.SHA256 != created.SHA256 {
		t.Fatal("metadata not persisted")
	}
	download, err := f.Service.Open(ctx, f.Owner, created.ID)
	testkit.RequireNoError(t, err)
	bytes, readErr := io.ReadAll(download.Body)
	closeErr := download.Body.Close()
	testkit.RequireNoError(t, readErr)
	testkit.RequireNoError(t, closeErr)
	if !strings.EqualFold(download.File.MediaType, "text/plain") || string(bytes) != string(body) {
		t.Fatal("wrong downloaded content")
	}
	testkit.RequireNoError(t, f.Service.Delete(ctx, f.Owner, created.ID))
	_, err = f.Service.Get(ctx, f.Owner, created.ID)
	testkit.RequireCode(t, err, core.NotFound)
	_, err = f.Service.Open(ctx, f.Owner, created.ID)
	testkit.RequireCode(t, err, core.NotFound)
}

func TestTDDFilesIdempotencyBindsPayloadAndOwner(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	body := []byte("same")
	first, err := f.Service.Upload(ctx, f.Owner, upload("same-key", body))
	testkit.RequireNoError(t, err)
	replay, err := f.Service.Upload(ctx, f.Owner, upload("same-key", body))
	testkit.RequireNoError(t, err)
	if replay.ID != first.ID {
		t.Fatal("duplicate upload created a second object")
	}
	_, err = f.Service.Upload(ctx, f.Owner, upload("same-key", []byte("different")))
	testkit.RequireCode(t, err, core.Conflict)
	metadata, objects, _ := f.Observe()
	if metadata != 1 || objects != 1 {
		t.Fatalf("duplicate or orphan publication: %d/%d", metadata, objects)
	}
}

func TestTDDFilesAccessDeniedBeforeReadingContent(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	file, err := f.Service.Upload(ctx, f.Owner, upload("private", []byte("private")))
	testkit.RequireNoError(t, err)
	_, err = f.Service.Get(ctx, f.Other, file.ID)
	denied(t, err)
	_, err = f.Service.Open(ctx, f.Other, file.ID)
	denied(t, err)
	denied(t, f.Service.Delete(ctx, f.Other, file.ID))
	_, err = f.Service.Open(ctx, core.Principal{}, file.ID)
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, err = f.Service.Get(ctx, f.Owner, file.ID)
	testkit.RequireNoError(t, err)
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestTDDFilesValidationAndCapacityFailuresPublishNothing(t *testing.T) {
	cases := []string{"oversized", "quota", "negative-size", "reader-failure", "metadata-failure", "object-commit-failure"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			f := fixture(t)
			ctx := testkit.Context(t)
			request := upload("bad-upload", []byte("content"))
			switch name {
			case "oversized":
				request.Size = f.MaxUpload + 1
				request.Body = io.LimitReader(strings.NewReader(strings.Repeat("x", 4096)), request.Size)
			case "quota":
				f.SetQuota(0)
			case "negative-size":
				request.Size = -1
			case "reader-failure":
				request.Body = failingReader{errors.New("read failed")}
			case "metadata-failure":
				f.FailMetadataOnce()
			case "object-commit-failure":
				f.FailObjectCommitOnce()
			}
			_, err := f.Service.Upload(ctx, f.Owner, request)
			if err == nil {
				t.Fatalf("%s published successfully", name)
			}
			ready, _, _ := f.Observe()
			if ready != 0 {
				t.Fatal("failed upload published readable metadata")
			}
			testkit.RequireNoError(t, f.Objects.Recover(ctx))
			ready, objects, staged := f.Observe()
			if ready != 0 || objects != 0 || staged != 0 {
				t.Fatalf("unrecovered failure: %d/%d/%d", ready, objects, staged)
			}
		})
	}
}

func TestTDDFilesDisplayNameCannotEscapeStorage(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	sentinel := []byte("outside-storage-sentinel")
	name, readOutside := f.SeedUploadNameTarget(sentinel)
	if !strings.ContainsAny(name, `/\`) {
		t.Fatal("fixture must provide a path-shaped upload name")
	}
	if !bytes.Equal(readOutside(), sentinel) {
		t.Fatal("outside sentinel was not initialized")
	}
	request := upload("unsafe-name", []byte("must-not-overwrite-sentinel"))
	request.Name = name
	created, err := f.Service.Upload(ctx, f.Owner, request)
	if !bytes.Equal(readOutside(), sentinel) {
		t.Fatal("upload name escaped object storage")
	}
	if err != nil {
		ready, _, _ := f.Observe()
		if ready != 0 {
			t.Fatal("rejected filename published metadata")
		}
		return
	}
	// A safe basename is also valid; rejecting the original display name is not
	// the only way to enforce object-storage boundaries.
	testkit.RequireNoError(t, created.ID.Validate())
	if created.State != files.Ready || created.Name == "" || created.Name == "." || created.Name == ".." || filepath.Base(created.Name) != created.Name || strings.ContainsAny(created.Name, `/\`) {
		t.Fatalf("unsafe name was not normalized: %+v", created)
	}
	download, err := f.Service.Open(ctx, f.Owner, created.ID)
	testkit.RequireNoError(t, err)
	got, readErr := io.ReadAll(download.Body)
	closeErr := download.Body.Close()
	testkit.RequireNoError(t, readErr)
	testkit.RequireNoError(t, closeErr)
	if string(got) != "must-not-overwrite-sentinel" {
		t.Fatal("safe upload returned outside file content")
	}
	testkit.RequireNoError(t, f.Service.Delete(ctx, f.Owner, created.ID))
	if !bytes.Equal(readOutside(), sentinel) {
		t.Fatal("deleting normalized upload changed outside sentinel")
	}
}

func TestTDDFilesActualBytesCannotExceedClaimedLimit(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	request := upload("dishonest-size", []byte("longer-than-declared"))
	request.Size = 1
	if _, err := f.Service.Upload(ctx, f.Owner, request); err == nil {
		t.Fatal("actual byte count was not enforced")
	}
	ready, _, _ := f.Observe()
	if ready != 0 {
		t.Fatal("over-limit file published")
	}
}

func TestTDDFilesObjectsStageCommitOpenRemove(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	data := []byte("stored bytes")
	staged, err := f.Objects.Put(ctx, "object", bytes.NewReader(data), int64(len(data)))
	testkit.RequireNoError(t, err)
	if staged.ID != "object" || staged.Size != int64(len(data)) || staged.SHA256 != digest(data) {
		t.Fatal("invalid staging result", staged)
	}
	if body, err := f.Objects.Open(ctx, staged.ID); err == nil {
		_ = body.Close()
		t.Fatal("uncommitted object became visible")
	}
	testkit.RequireNoError(t, f.Objects.Commit(ctx, staged))
	testkit.RequireNoError(t, f.Objects.Commit(ctx, staged))
	reader, err := f.Objects.Open(ctx, staged.ID)
	testkit.RequireNoError(t, err)
	got, err := io.ReadAll(reader)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, reader.Close())
	if !bytes.Equal(got, data) {
		t.Fatal("committed content changed")
	}
	testkit.RequireNoError(t, f.Objects.Remove(ctx, staged.ID))
	_, err = f.Objects.Open(ctx, staged.ID)
	testkit.RequireCode(t, err, core.NotFound)
}

func TestTDDFilesObjectsRejectPathTraversalAndSymlinkEscape(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	for _, id := range []core.ID{"../secret", "/etc/passwd", "a/b", "a\\b"} {
		if reader, err := f.Objects.Open(ctx, id); err == nil {
			_ = reader.Close()
			t.Fatal("opened path as object ID", id)
		}
		if _, err := f.Objects.Put(ctx, id, strings.NewReader("overwrite"), 9); err == nil {
			t.Fatal("stored path as object ID", id)
		}
		if err := f.Objects.Remove(ctx, id); err == nil {
			t.Fatal("removed path as object ID", id)
		}
	}
	outside := []byte("outside-store-secret")
	readOutside := f.SeedSymlink("link", outside)
	if reader, err := f.Objects.Open(ctx, "link"); err == nil {
		_ = reader.Close()
		t.Fatal("followed object symlink outside storage")
	}
	_, _ = f.Objects.Put(ctx, "link", strings.NewReader("overwritten"), 11)
	_ = f.Objects.Remove(ctx, "link")
	if !bytes.Equal(readOutside(), outside) {
		t.Fatal("symlink traversal modified outside file")
	}
}

func TestTDDFilesObjectsRecoverIsRepeatable(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	_, err := f.Objects.Put(ctx, "orphan", strings.NewReader("orphan"), 6)
	testkit.RequireNoError(t, err)
	for range 2 {
		testkit.RequireNoError(t, f.Objects.Recover(ctx))
	}
	ready, committed, staged := f.Observe()
	if ready != 0 || committed != 0 || staged != 0 {
		t.Fatalf("orphan survived recovery: %d/%d/%d", ready, committed, staged)
	}
}

func TestTDDFilesRepositoryOwnershipAndStates(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	record := files.File{ID: "row", OwnerID: f.Owner.OwnerID, Name: "file.txt", MediaType: "text/plain", Size: 3, SHA256: digest([]byte("abc")), State: files.Staged, CreatedAt: time.Now()}
	testkit.RequireNoError(t, f.Within(ctx, func(repo files.Repository) error { return repo.Insert(ctx, record) }))
	testkit.RequireNoError(t, f.Within(ctx, func(repo files.Repository) error {
		got, err := repo.Get(ctx, record.OwnerID, record.ID)
		if err != nil {
			return err
		}
		if got.State != files.Staged || got.OwnerID != record.OwnerID {
			t.Fatal("wrong stored metadata")
		}
		_, err = repo.Get(ctx, f.Other.OwnerID, record.ID)
		testkit.RequireCode(t, err, core.NotFound)
		testkit.RequireCode(t, repo.SetState(ctx, f.Other.OwnerID, record.ID, files.Ready), core.NotFound)
		testkit.RequireCode(t, repo.Delete(ctx, f.Other.OwnerID, record.ID), core.NotFound)
		return repo.SetState(ctx, record.OwnerID, record.ID, files.Ready)
	}))
	testkit.RequireNoError(t, f.Within(ctx, func(repo files.Repository) error {
		got, err := repo.Get(ctx, record.OwnerID, record.ID)
		if err != nil {
			return err
		}
		if got.State != files.Ready {
			t.Fatal("state change lost")
		}
		return repo.Delete(ctx, record.OwnerID, record.ID)
	}))
	testkit.RequireNoError(t, f.Within(ctx, func(repo files.Repository) error {
		_, err := repo.Get(ctx, record.OwnerID, record.ID)
		testkit.RequireCode(t, err, core.NotFound)
		return nil
	}))
}

func TestTDDFilesCanceledRequestLeavesNoPublishedFile(t *testing.T) {
	f := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.Service.Upload(ctx, f.Owner, upload("canceled", []byte("data")))
	testkit.RequireCode(t, err, core.Canceled)
	_, err = f.Objects.Put(ctx, "canceled", strings.NewReader("data"), 4)
	testkit.RequireCode(t, err, core.Canceled)
	ready, _, _ := f.Observe()
	if ready != 0 {
		t.Fatal("canceled upload published metadata")
	}
}
