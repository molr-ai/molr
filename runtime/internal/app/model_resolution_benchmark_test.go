package app

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/modelcatalog"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
)

func BenchmarkModelResolutionProviders(b *testing.B) {
	for _, count := range []int{10, 100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "models.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			repo := resource.NewRepository(db)
			var target core.ID
			err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
				for i := 0; i < count; i++ {
					id := core.ID(fmt.Sprintf("provider-%06d", i))
					r := resource.Resource{ID: id, OwnerID: "owner", Kind: resource.Model, Provider: "openai-compatible", State: resource.Available, Version: 1}
					binding := resource.Binding{ResourceID: id, Endpoint: "https://example.invalid/v1", Models: []resource.ModelConfig{{Name: "first", ContextLimit: 32768}, {Name: "second", ContextLimit: 65536}}}
					if err := repo.Insert(ctx, r, binding); err != nil {
						return err
					}
					target = modelcatalog.Entries(r, binding)[1].ID
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			adapter := adapters{store: db}
			p := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				provider, err := adapter.provider(b.Context(), p, target)
				if err != nil || provider.Model.ID != target || provider.Model.Name != "second" {
					b.Fatal(provider, err)
				}
			}
		})
	}
}
