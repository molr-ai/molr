package tools

import (
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"testing"
)

func TestFileSearchArgumentsAndSemanticDefaults(t *testing.T) {
	args, err := fileArguments(Call{Name: "file_search", Arguments: json.RawMessage(`{"path":".agents/memory","pattern":"沟通","glob":["**/*.md"]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if args.fileSearch.Path != "/work/.agents/memory" || !args.fileSearch.SearchOptions().CaseSensitive {
		t.Fatal("default search target/options", args.fileSearch)
	}
	for _, raw := range []string{`{"path":"/work","pattern":"[","regex":true}`, `{"path":"/work","mode":"content"}`, `{"path":"/work/../global","pattern":"x"}`, `{"path":"/work","pattern":"x","context_lines":6}`} {
		_, err = fileArguments(Call{Name: "file_search", Arguments: json.RawMessage(raw)})
		if core.CodeOf(err) != core.InvalidArgument {
			t.Fatal(raw, err)
		}
	}
	_, err = fileArguments(Call{Name: "file_search", Arguments: json.RawMessage(`{"path":"/work","mode":"files","glob":["**/沟通*.md"]}`)})
	if err != nil {
		t.Fatal(err)
	}
}
