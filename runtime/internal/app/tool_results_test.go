package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

func TestLargeToolResultIsSavedAndReadableInChunks(t *testing.T) {
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { c.Sandbox.Enabled = false })
	fs := &resourceFileTools{adapters: f.ad}
	content, _ := json.Marshal(map[string]any{"Text": strings.Repeat("原始长结果 test\n", 20000), "Exit": map[string]int{"Code": 1}})
	result, err := fs.boundResult(t.Context(), f.execution, tools.Result{OperationID: f.execution.OperationID, Content: content})
	need(t, err)
	if len(result.Content) > toolInlineLimit {
		t.Fatal("inline result too large")
	}
	var envelope struct {
		FullResultPath string                     `json:"full_result_path"`
		Details        map[string]json.RawMessage `json:"details"`
	}
	need(t, json.Unmarshal(result.Content, &envelope))
	if !strings.HasPrefix(envelope.FullResultPath, "/work/.local/state/molr/tool-result-") || envelope.Details["Exit"] == nil {
		t.Fatal("result metadata missing")
	}
	var collected bytes.Buffer
	offset := int64(0)
	for {
		data, err := fs.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: tools.FileTarget{Path: envelope.FullResultPath}, ByteOffset: &offset, MaxBytes: 4096})
		need(t, err)
		var part struct {
			Text           string
			NextByteOffset int64
			Truncated      bool
		}
		need(t, json.Unmarshal(data, &part))
		collected.WriteString(part.Text)
		if !part.Truncated {
			break
		}
		if part.NextByteOffset <= offset {
			t.Fatal("paging did not advance")
		}
		offset = part.NextByteOffset
	}
	if !bytes.Equal(collected.Bytes(), content) {
		t.Fatal("saved result differs from original")
	}
	replay, err := fs.boundResult(t.Context(), f.execution, tools.Result{Content: content})
	need(t, err)
	if !bytes.Equal(replay.Content, result.Content) {
		t.Fatal("replay created a different envelope")
	}
}
func TestToolResultRefusesLinkedOutput(t *testing.T) {
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { c.Sandbox.Enabled = false })
	fs := &resourceFileTools{adapters: f.ad}
	content, _ := json.Marshal(strings.Repeat("test", 10000))
	result, err := fs.boundResult(t.Context(), f.execution, tools.Result{Content: content})
	need(t, err)
	var saved struct {
		Path string `json:"full_result_path"`
	}
	need(t, json.Unmarshal(result.Content, &saved))
	root := platform.AssistantWorkspace(f.ad.dataDir, f.ad.storageConfig.WorkRoot, f.owner.OwnerID, f.execution.AssistantID)
	name := root + "/" + strings.TrimPrefix(saved.Path, "/work/")
	need(t, os.Remove(name))
	target := t.TempDir() + "/private"
	need(t, os.WriteFile(target, []byte("keep"), 0600))
	need(t, os.Symlink(target, name))
	_, err = fs.boundResult(t.Context(), f.execution, tools.Result{Content: content})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("linked output accepted", err)
	}
	data, err := os.ReadFile(target)
	need(t, err)
	if string(data) != "keep" {
		t.Fatal("external file modified")
	}
}
func TestShellArchiveQuotaReportsPartialOutput(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "output")
	need(t, err)
	sink := &shellOutput{file: file, budget: &shellOutputBudget{left: 8}}
	n, err := sink.Write([]byte("abcdefghijkl"))
	if n != 8 || err == nil || !sink.budget.failed {
		t.Fatal("quota not reported", n, err)
	}
	sink.close()
	sink.close()
	saved, err := os.Open(file.Name())
	need(t, err)
	defer saved.Close()
	data, err := io.ReadAll(saved)
	need(t, err)
	if string(data) != "abcdefgh" {
		t.Fatal("partial output not preserved")
	}
}

func TestToolEnvelopeBoundsEscapedJSON(t *testing.T) {
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { c.Sandbox.Enabled = false })
	fs := &resourceFileTools{adapters: f.ad}
	content := json.RawMessage(`{"Path":"` + strings.Repeat("<", 500) + `","Text":"` + strings.Repeat("<", 30000) + `"}`)
	result, err := fs.boundResult(t.Context(), f.execution, tools.Result{Content: content})
	need(t, err)
	if len(result.Content) > toolInlineLimit || !json.Valid(result.Content) {
		t.Fatal("escaped envelope exceeded limit")
	}
}

func TestShellArchiveCanBeReadFromStateDirectory(t *testing.T) {
	root := t.TempDir()
	e := core.Execution{Principal: core.Principal{OwnerID: "owner"}, OperationID: "capture"}
	stdout, stderr, outPath, errPath, err := createShellTaskOutputs(root, 1<<20, e)
	need(t, err)
	_, err = stdout.Write([]byte("saved stdout"))
	need(t, err)
	_, err = stderr.Write([]byte("saved stderr"))
	need(t, err)
	stdout.close()
	stderr.close()
	for _, stream := range []string{"stdout", "stderr"} {
		raw, err := readShellOutput(root, outPath, errPath, tools.ShellTaskRequest{Stream: stream})
		need(t, err)
		var result struct{ Output, Path string }
		need(t, json.Unmarshal(raw, &result))
		if result.Output != "saved "+stream || !strings.HasPrefix(result.Path, "/work/.local/state/molr/") {
			t.Fatalf("wrong archive: %s", raw)
		}
	}
	entries, err := os.ReadDir(root)
	need(t, err)
	if len(entries) != 1 || entries[0].Name() != ".local" {
		t.Fatal("shell polluted workspace root")
	}
}
