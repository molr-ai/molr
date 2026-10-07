package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/textfiles"
)

// Receipts contain only operation digests and file references/hashes. A pending
// receipt is durable before the file rename; recovery inspects that exact hash.
// Replaying an old operation never restores content edited or removed later.
type receipt struct {
	Sources       []core.Object
	Digest        string
	Ref           Ref
	Version       string
	Planned, Done bool
}
type mutationPlan struct {
	record *receipt
	save   func() error
}
type planKey struct{}

func planMutation(ctx context.Context, m Memory) error {
	p, _ := ctx.Value(planKey{}).(*mutationPlan)
	if p == nil {
		return nil
	}
	p.record.Sources = m.Sources
	p.record.Ref = m.Ref
	p.record.Version = m.Version
	p.record.Planned = true
	return p.save()
}
func mutate[T any](ctx context.Context, s *service, p core.Principal, action string, ref Ref, m Mutation, payload any, update, ownerOnly bool, fn func(context.Context) (T, error)) (T, error) {
	var out T
	if err := m.Validate(update); err != nil {
		return out, err
	}
	if err := s.authorize(ctx, p, core.Object{Kind: objectKind}, policy.Write, ownerOnly); err != nil {
		return out, err
	}
	if s.repo == nil || s.repo.options.JournalRoot == "" {
		return out, core.NewError(core.Unavailable, "memory file storage unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	refBytes, _ := json.Marshal(ref)
	root := filepath.Join(s.repo.options.JournalRoot, hash([]byte(p.OwnerID)))
	if err := os.MkdirAll(root, 0700); err != nil {
		return out, err
	}
	name := hash([]byte(string(p.SubjectID)+"\x00"+action+"\x00"+string(refBytes)+"\x00"+m.IdempotencyKey)) + ".json"
	data, err := json.Marshal(payload)
	if err != nil {
		return out, err
	}
	r := receipt{Digest: hash(data)}
	stat := ""
	old, version, _, err := readRaw(root, name, 80<<10)
	if err == nil {
		var prior receipt
		if json.Unmarshal(old, &prior) != nil {
			return out, core.NewError(core.Internal, "invalid memory receipt")
		}
		if prior.Digest != r.Digest {
			return out, conflict()
		}
		r = prior
		stat = version
		if r.Planned {
			current, e := s.Get(ctx, p, r.Ref)
			if e == nil {
				if e = s.access(ctx, p, current.Ref, true, false); e != nil {
					return out, e
				}
				if current.Version == r.Version {
					if result, ok := any(current).(T); ok {
						return result, nil
					}
				}
			} else if core.CodeOf(e) != core.NotFound {
				return out, e
			} else if (action == "delete" || action == "forget") && r.Version == "" {
				if action == "forget" {
					return out, s.scrub(ctx, p, Memory{Ref: r.Ref, OwnerID: p.OwnerID, Sources: r.Sources})
				}
				return out, nil
			} else if action == "create" {
				return out, conflict()
			}
			if r.Done {
				return out, conflict()
			}
		}
	} else if core.CodeOf(err) != core.NotFound {
		return out, err
	}
	save := func() error {
		b, e := json.Marshal(r)
		if e != nil {
			return e
		}
		stat, e = textfiles.Replace(ctx, root, name, string(b), stat != "", stat, func() error { return ctx.Err() })
		return e
	}
	ctx = context.WithValue(ctx, planKey{}, &mutationPlan{record: &r, save: save})
	out, err = fn(ctx)
	if err != nil {
		return out, err
	}
	r.Done = true
	if err = save(); err != nil {
		return out, err
	}
	return out, nil
}
