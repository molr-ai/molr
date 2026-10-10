package search

import (
	"context"
	"fmt"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

type candidateAuthorizer struct{ checks int }

func (a *candidateAuthorizer) Require(ctx context.Context, p core.Principal, q policy.CheckRequest) error {
	if err := (fixtureAuthorizer{}).Require(ctx, p, q); err != nil {
		return err
	}
	if q.Target.Kind == "message" {
		a.checks++
		if q.Target.ID == "message-0500" {
			return core.NewError(core.Forbidden, "synthetic denied match")
		}
	}
	return nil
}

func candidateDocuments(count int) []Document {
	docs := make([]Document, count)
	for i := range docs {
		text := "unrelated historical entry"
		if i%500 == 0 {
			text = "budget 预算"
		}
		docs[i] = Document{OwnerID: "owner", Source: Messages, Object: core.Object{Kind: "message", ID: core.ID(fmt.Sprintf("message-%04d", i))}, Text: text}
	}
	return docs
}

func TestSearchAuthorizesOnlyMatchingCandidates(t *testing.T) {
	db := fixtureDB(t)
	authorizer := &candidateAuthorizer{}
	documents := candidateDocuments(1000)
	service := NewService(db, Options{Authorizer: authorizer, Documents: func(context.Context, core.ID) ([]Document, error) { return documents, nil }})
	result, err := service.Search(t.Context(), owner(), Query{Text: "  预算  ", Sources: []Source{Messages}})
	testkit.RequireNoError(t, err)
	if len(result.Hits.Items) != 1 || result.Hits.Items[0].Object.ID != "message-0000" || result.Counts[Messages] != 1 {
		t.Fatalf("denied match leaked or allowed match lost: %+v", result)
	}
	if authorizer.checks != 2 {
		t.Fatalf("1000 documents with two text matches required %d object authorization checks, want 2", authorizer.checks)
	}
	authorizer.checks = 0
	result, err = service.Search(t.Context(), owner(), Query{Text: " ", Sources: []Source{Messages}})
	testkit.RequireNoError(t, err)
	if authorizer.checks != 1000 || result.Counts[Messages] != 999 {
		t.Fatalf("empty search must authorize every candidate: checks=%d result=%+v", authorizer.checks, result)
	}
}

type candidatePolicy func(context.Context, core.Principal, policy.CheckRequest) error

func (f candidatePolicy) Require(ctx context.Context, p core.Principal, q policy.CheckRequest) error {
	return f(ctx, p, q)
}

func TestSearchCandidateAuthorizationErrors(t *testing.T) {
	for _, code := range []core.Code{core.Forbidden, core.NotFound, core.Unavailable, core.Unauthenticated} {
		t.Run(string(code), func(t *testing.T) {
			db := fixtureDB(t)
			documents := candidateDocuments(1)
			checks := 0
			service := NewService(db, Options{Documents: func(context.Context, core.ID) ([]Document, error) { return documents, nil }, Authorizer: candidatePolicy(func(_ context.Context, _ core.Principal, q policy.CheckRequest) error {
				if q.Target.Kind == "message" {
					checks++
					return core.NewError(code, "synthetic authorization failure")
				}
				return nil
			})})
			result, err := service.Search(t.Context(), owner(), Query{Text: "budget"})
			if code == core.Forbidden || code == core.NotFound {
				testkit.RequireNoError(t, err)
				if len(result.Hits.Items) != 0 || result.Counts[Messages] != 0 {
					t.Fatal("unauthorized match leaked", result)
				}
			} else {
				testkit.RequireCode(t, err, code)
			}
			if checks != 1 {
				t.Fatal("matching object not checked", checks)
			}
			checks = 0
			_, err = service.Search(t.Context(), owner(), Query{Text: "absent"})
			testkit.RequireNoError(t, err)
			if checks != 0 {
				t.Fatal("nonmatching object checked", checks)
			}
		})
	}
}

func TestSearchCandidateEntryAuthorizationAndCancellation(t *testing.T) {
	db := fixtureDB(t)
	visited := false
	service := NewService(db, Options{Documents: func(context.Context, core.ID) ([]Document, error) { visited = true; return candidateDocuments(10), nil }, Authorizer: candidatePolicy(func(context.Context, core.Principal, policy.CheckRequest) error {
		return core.NewError(core.Unauthenticated, "synthetic expired session")
	})})
	_, err := service.Search(t.Context(), owner(), Query{Text: "absent"})
	testkit.RequireCode(t, err, core.Unauthenticated)
	if visited {
		t.Fatal("unauthenticated search read documents")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	service = NewService(db, Options{Authorizer: fixtureAuthorizer{}, Documents: func(context.Context, core.ID) ([]Document, error) { cancel(); return candidateDocuments(10), nil }})
	_, err = service.Search(ctx, owner(), Query{Text: "absent"})
	testkit.RequireCode(t, err, core.Canceled)
}
