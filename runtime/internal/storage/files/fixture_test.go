package files_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
)

type fileAuthorizer struct{}

func (fileAuthorizer) Require(ctx context.Context, p core.Principal, request policy.CheckRequest) error {
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Kind != core.ActorOwner {
		return core.NewError(core.Forbidden, "fixture denies assistant")
	}
	return nil
}
func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		directory := t.TempDir()
		store, err := sqlitestore.Open(filepath.Join(directory, "data.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		objectDirectory := filepath.Join(directory, "files")
		objects, err := files.NewObjects(objectDirectory, store)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = objects.Close() })
		var quota atomic.Int64
		quota.Store(1 << 20)
		service := files.NewService(store, objects, files.Options{Authorizer: fileAuthorizer{}, MaxUploadBytes: 4096, DiskQuota: quota.Load})
		owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
		other := core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}
		return contractFixture{Service: service, Objects: objects, Owner: owner, Other: other, MaxUpload: 4096, SetQuota: quota.Store,
			Within: func(ctx context.Context, fn func(files.Repository) error) error {
				return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(files.NewRepository(tx.Store())) })
			},
			FailMetadataOnce: func() { store.FailNextCommit(errors.New("injected SQL commit failure")) }, FailObjectCommitOnce: func() { objects.FailNextCommit(errors.New("injected object commit failure")) },
			Observe: func() (int, int, int) {
				rows, err := persistence.Scan[files.File](context.Background(), store, files.Bucket)
				if err != nil {
					t.Fatal(err)
				}
				ready := 0
				for _, row := range rows {
					if row.State == files.Ready {
						ready++
					}
				}
				committed, err := os.ReadDir(filepath.Join(objectDirectory, "objects"))
				if err != nil {
					t.Fatal(err)
				}
				staged, err := os.ReadDir(filepath.Join(objectDirectory, "staging"))
				if err != nil {
					t.Fatal(err)
				}
				return ready, len(committed), len(staged)
			},
			SeedSymlink: func(id core.ID, value []byte) func() []byte {
				outside := filepath.Join(directory, "sentinel")
				if err := os.WriteFile(outside, value, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(objectDirectory, "objects", string(id))); err != nil {
					t.Fatal(err)
				}
				return func() []byte {
					data, err := os.ReadFile(outside)
					if err != nil {
						t.Fatal(err)
					}
					return data
				}
			},
			SeedUploadNameTarget: func(value []byte) (string, func() []byte) {
				outside := filepath.Join(directory, "upload-sentinel")
				if err := os.WriteFile(outside, value, 0600); err != nil {
					t.Fatal(err)
				}
				return "../../upload-sentinel", func() []byte {
					data, err := os.ReadFile(outside)
					if err != nil {
						t.Fatal(err)
					}
					return data
				}
			},
		}
	}
}
