package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"strings"
	"unicode"
)

func state(s State) State {
	if s == "" {
		return Active
	}
	return s
}
func validateMetadata(context string) error {
	if len(context) > 1024 {
		return invalid("context")
	}
	return nil
}
func digest(s string) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.Join(strings.Fields(s), ""))))
	return hex.EncodeToString(h[:])
}

type Tombstone struct {
	OwnerID, AssistantID core.ID
	Hash                 string
	Sources              []core.Object
}

func scrubHistory(ctx context.Context, store persistence.Store, m Memory) error {
	if len(m.Sources) == 0 {
		return nil
	}
	return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		// Source membership is indexed; erased content never requires loading unrelated dreams.
		matching := map[string]persistence.Record{}
		for _, source := range m.Sources {
			for _, field := range []string{"MemorySources", "Sources.Source"} {
				rows, err := tx.Query(ctx, "business.dream_runs", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", m.OwnerID), {Field: field, Op: "has", Value: source.Kind + "/" + string(source.ID)}}})
				if err != nil {
					return err
				}
				for _, row := range rows {
					matching[row.Key] = row
				}
			}
		}

		for _, row := range matching {
			var r struct {
				OwnerID       core.ID
				MemorySources []core.Object
				Sources       []struct{ Source core.Object }
			}
			if json.Unmarshal(row.Data, &r) != nil {
				return core.NewError(core.Internal, "invalid dream record")
			}
			if r.OwnerID != m.OwnerID {
				continue
			}
			affected := false
			for _, a := range r.MemorySources {
				for _, b := range m.Sources {
					if a == b {
						affected = true
					}
				}
			}
			for _, src := range r.Sources {
				for _, ref := range m.Sources {
					if src.Source == ref {
						affected = true
					}
				}
			}
			if !affected {
				continue
			}
			var data map[string]json.RawMessage
			if err := json.Unmarshal(row.Data, &data); err != nil {
				return err
			}
			data["Summary"] = json.RawMessage(`""`)
			data["Output"] = json.RawMessage(`""`)
			data["Decisions"] = json.RawMessage(`[]`)
			var sources []map[string]json.RawMessage
			if err := json.Unmarshal(data["Sources"], &sources); err != nil {
				return err
			}
			for _, src := range sources {
				src["Text"] = json.RawMessage(`""`)
			}
			data["Sources"], _ = json.Marshal(sources)
			if err := tx.Put(ctx, "business.dream_runs", row.Key, data); err != nil {
				return err
			}
		}
		return nil
	})
}

// Terms supports Latin words and Han bigrams without an external embedding service.
func Terms(text string) map[string]bool {
	out := map[string]bool{}
	var word []rune
	var han rune
	flush := func() {
		if len(word) > 1 {
			out[string(word)] = true
		}
		word = nil
	}
	for _, r := range strings.ToLower(text) {
		if unicode.Is(unicode.Han, r) {
			flush()
			if han != 0 {
				out[string([]rune{han, r})] = true
			}
			han = r
			continue
		}
		han = 0
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}
func Score(query, text string) int {
	q := Terms(query)
	t := Terms(text)
	n := 0
	for term := range q {
		if t[term] {
			n++
		}
	}
	return n
}
