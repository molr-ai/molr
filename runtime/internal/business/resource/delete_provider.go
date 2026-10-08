package resource

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/persistence"
)

func (s *service) DeleteProvider(ctx context.Context, p core.Principal, req DeleteProviderRequest) error {
	payload := struct {
		ID      core.ID
		Version core.Version
	}{req.ID, req.Mutation.ExpectedVersion}
	// Collection-level owner authorization remains valid when replaying a delete
	// receipt after the resource has gone. The initial delete loads by owner + ID.
	_, err := mutateForTarget(ctx, s, p, "delete-provider", req.ID, req.Mutation, payload, true, true, core.Object{Kind: objectKind}, func(ctx context.Context) (struct{}, error) {
		result := struct{}{}
		r, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return result, err
		}
		if r.Kind != Model {
			return result, invalid("provider")
		}
		if r.Version != req.Mutation.ExpectedVersion {
			return result, conflict()
		}
		binding, err := s.repo.Binding(ctx, p.OwnerID, r.ID)
		if err != nil {
			return result, err
		}
		if s.options.CheckVerification == nil || s.options.OnDeleteProvider == nil {
			return result, core.NewError(core.Unavailable, "provider deletion unavailable")
		}
		removeKey := binding.CredentialID != ""
		if removeKey {
			bindings, err := persistence.Select[Binding](ctx, s.store, bindingBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("CredentialID", binding.CredentialID), {Field: "ResourceID", Op: "!=", Value: r.ID}}, Limit: 1})
			if err != nil {
				return result, err
			}
			for _, other := range bindings {
				if other.ResourceID != r.ID && other.CredentialID == binding.CredentialID {
					removeKey = false
					break
				}
			}
		}
		verified := false
		if removeKey {
			if s.options.Credentials == nil {
				return result, core.NewError(core.Unavailable, "credential storage unavailable")
			}
			key, err := s.options.Credentials.Get(ctx, p, binding.CredentialID)
			if err != nil && !persistence.Missing(err) {
				return result, err
			}
			if err == nil {
				if err := s.options.Credentials.Delete(ctx, p, credential.DeleteRequest{ID: key.ID, Mutation: core.Mutation{IdempotencyKey: req.Mutation.IdempotencyKey, ExpectedVersion: key.Version}, Verification: req.Verification}); err != nil {
					return result, err
				}
				verified = true
			}
		}
		if !verified {
			if err := s.options.CheckVerification(ctx, p, req.Verification, "credential.delete"); err != nil {
				return result, err
			}
		}
		if err := s.options.OnDeleteProvider(ctx, p, r, binding); err != nil {
			return result, err
		}
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := tx.Delete(ctx, bindingBucket, persistence.Key(r.OwnerID, r.ID)); err != nil {
				return err
			}
			return tx.Delete(ctx, bucket, persistence.Key(r.OwnerID, r.ID))
		})
		return result, err
	})
	return err
}
