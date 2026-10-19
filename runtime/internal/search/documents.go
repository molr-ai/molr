package search

import (
	"context"
	"molr.ai/runtime/internal/catalog"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func selectedSource(q Query, source Source) bool {
	if len(q.Sources) == 0 {
		return true
	}
	for _, selected := range q.Sources {
		if selected == source {
			return true
		}
	}
	return false
}

// Each batch releases its read transaction before decoding or visiting rows.
// Callers already in a transaction (index maintenance) retain their snapshot.
func walkRecords(ctx context.Context, store persistence.Store, bucket string, q persistence.Query, visit func(persistence.Record) error) error {
	q.Limit = 200
	for {
		if err := persistence.CheckContext(ctx); err != nil {
			return err
		}
		rows, err := readRecords(ctx, store, bucket, q)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := visit(row); err != nil {
				return err
			}
		}
		if len(rows) < q.Limit {
			return nil
		}
		q.After = []any{rows[len(rows)-1].Key}
	}
}

type sourceRow struct {
	ID, AssistantID   core.ID
	Name, Goal, State string
	Version           core.Version
	Sequence          uint64
	Content           struct {
		Text          string
		Card          *struct{ Title, Summary string }
		AttachmentIDs []core.ID
	}
}

func (s *service) walkDocuments(ctx context.Context, owner core.ID, query Query, visit func(Document) error) error {
	for _, source := range []struct {
		bucket string
		source Source
		kind   string
	}{
		{"business.assistants", Assistants, "assistant"},
		{"business.resources", Resources, "resource"},
		{"business.tasks", Tasks, "task"},
	} {
		if !selectedSource(query, source.source) {
			continue
		}
		q := persistence.Query{Prefix: string(owner) + "/"}
		if source.source == Resources {
			q.Prefix = string(catalog.Scope) + "/"
		}
		if query.AssistantID != "" {
			switch source.source {
			case Assistants:
				q.Where = []persistence.Filter{persistence.Eq("key", persistence.Key(owner, query.AssistantID))}
			case Tasks:
				q.Where = []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("AssistantID", query.AssistantID)}
			case Resources:
				// Resources have no assistant association in the catalog.
				continue
			}
		}
		if source.source == Assistants && !query.IncludeArchived {
			q.Any = [][]persistence.Filter{{{Field: "State", Op: "!=", Value: "dismissed"}}, {persistence.Eq("State", nil)}}
		}
		if err := walkRecords(ctx, s.store, source.bucket, q, func(record persistence.Record) error {
			row, err := persistence.Decode[sourceRow](record)
			if err != nil {
				return err
			}
			doc := Document{OwnerID: owner, Object: core.Object{Kind: source.kind, ID: row.ID}, Source: source.source, AssistantID: row.AssistantID, Version: row.Version}
			switch source.source {
			case Assistants:
				doc.Title, doc.AssistantID, doc.Archived = row.Name, row.ID, row.State == "dismissed"
			case Resources:
				doc.Title = row.Name
			case Tasks:
				doc.Title = row.Goal
			}
			return visit(doc)
		}); err != nil {
			return err
		}
	}
	if !selectedSource(query, Messages) {
		return nil
	}
	// Read each selected conversation once. Its account, assistant and archive
	// constraints are applied before any of its messages are loaded.
	q := persistence.Query{Prefix: string(owner) + "/", Where: []persistence.Filter{persistence.Eq("OwnerID", owner)}}
	if query.AssistantID != "" {
		q.Where = append(q.Where, persistence.Eq("PeerID", query.AssistantID))
	}
	if !query.IncludeArchived {
		q.Any = [][]persistence.Filter{{persistence.Eq("Archived", false)}, {persistence.Eq("Archived", nil)}}
	}
	return walkRecords(ctx, s.store, "im.conversations", q, func(record persistence.Record) error {
		chat, err := persistence.Decode[struct {
			ID, PeerID   core.ID
			Archived     bool
			LastSequence uint64
		}](record)
		if err != nil {
			return err
		}
		messages := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", owner), persistence.Eq("ConversationID", chat.ID), {Field: "Sequence", Op: "<=", Value: chat.LastSequence}}}
		return walkMessages(ctx, s.store, messages, func(record persistence.Record) error {
			row, err := persistence.Decode[sourceRow](record)
			if err != nil {
				return err
			}
			doc := Document{OwnerID: owner, Object: core.Object{Kind: "message", ID: row.ID}, Source: Messages, AssistantID: chat.PeerID, Archived: chat.Archived, Version: core.Version(row.Sequence), Text: row.Content.Text}
			if row.Content.Card != nil {
				doc.Title, doc.Text = row.Content.Card.Title, row.Content.Card.Summary
			}
			if len(row.Content.AttachmentIDs) > 0 {
				doc.FileID = row.Content.AttachmentIDs[0]
			}
			return visit(doc)
		})
	})
}

// Sequence is unique within one conversation. Its existing index handles
// traversal and ordering without repeatedly sorting the history by message ID.
func walkMessages(ctx context.Context, store persistence.Store, q persistence.Query, visit func(persistence.Record) error) error {
	q.OrderBy, q.Limit = "Sequence", 200
	filters := append([]persistence.Filter(nil), q.Where...)
	for {
		if err := persistence.CheckContext(ctx); err != nil {
			return err
		}
		rows, err := readRecords(ctx, store, "im.messages", q)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := visit(row); err != nil {
				return err
			}
		}
		if len(rows) < q.Limit {
			return nil
		}
		last, err := persistence.Decode[struct{ Sequence uint64 }](rows[len(rows)-1])
		if err != nil {
			return err
		}
		q.Where = append(append([]persistence.Filter(nil), filters...), persistence.Filter{Field: "Sequence", Op: ">", Value: last.Sequence})
	}
}

// Keep only SQL access inside the transaction; never pass its context to visit.
func readRecords(ctx context.Context, store persistence.Store, bucket string, q persistence.Query) ([]persistence.Record, error) {
	var rows []persistence.Record
	err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		rows, err = tx.Query(ctx, bucket, q)
		return err
	})
	return rows, err
}
