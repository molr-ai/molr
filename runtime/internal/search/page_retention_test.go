package search

import (
	"context"
	"reflect"
	"testing"

	"molr.ai/runtime/internal/core"
)

func TestSearchPagesRetainOnlySelectedHits(t *testing.T) {
	db := fixtureDB(t)
	documents := candidateDocuments(103)
	service := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Documents: func(context.Context, core.ID) ([]Document, error) { return documents, nil }})
	all, err := service.Search(t.Context(), owner(), Query{Sources: []Source{Messages}, Page: core.PageRequest{Limit: 200}})
	if err != nil {
		t.Fatal(err)
	}
	if all.Counts[Messages] != len(documents) {
		t.Fatal(all.Counts)
	}
	query := Query{Sources: []Source{Messages}, Page: core.PageRequest{Limit: 20}}
	var hits []Hit
	for pages := 0; ; pages++ {
		if pages > 6 {
			t.Fatal("pagination did not finish")
		}
		result, err := service.Search(t.Context(), owner(), query)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Counts, all.Counts) {
			t.Fatal("page changed total counts")
		}
		if cap(result.Hits.Items) != len(result.Hits.Items) {
			t.Fatalf("page holds unused hit capacity: len=%d cap=%d", len(result.Hits.Items), cap(result.Hits.Items))
		}
		hits = append(hits, result.Hits.Items...)
		if result.Hits.NextCursor == "" {
			break
		}
		query.Page.Cursor = result.Hits.NextCursor
	}
	if !reflect.DeepEqual(hits, all.Hits.Items) {
		t.Fatal("paging changed ranking or lost hits")
	}
	empty, err := service.Search(t.Context(), owner(), Query{Text: "no matching text"})
	if err != nil || empty.Hits.Items == nil || len(empty.Hits.Items) != 0 || empty.Hits.NextCursor != "" {
		t.Fatalf("empty page: %+v %v", empty, err)
	}
}

func TestSearchPageOwnsItsItems(t *testing.T) {
	items := []string{"first", "second", "third"}
	page, err := paginate(items, "owner", "query", core.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	items[0] = "changed"
	if page.Items[0] != "first" {
		t.Fatal("page still aliases the full matches array")
	}
	page.Items = append(page.Items, "appended")
	if items[1] != "second" {
		t.Fatal("appending to a page overwrote another match")
	}
}
