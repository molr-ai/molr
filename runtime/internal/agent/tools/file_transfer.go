package tools

import (
	"bytes"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/core"
)

type FileTransferRequest struct {
	Source          FileTarget `json:"source"`
	Destination     FileTarget `json:"destination"`
	SourceVersion   string     `json:"source_version,omitempty"`
	ExpectedVersion string     `json:"expected_version,omitempty"`
	Overwrite       bool       `json:"overwrite"`
}

func parseFileTransfer(raw json.RawMessage) (FileTransferRequest, error) {
	var r FileTransferRequest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF {
		return r, core.Invalid("arguments", "invalid transfer")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if _, ok := fields["overwrite"]; !ok {
		return r, core.Invalid("overwrite", "required")
	}
	for _, t := range []FileTarget{r.Source, r.Destination} {
		if t.Path == "" || len(t.Path) > 4096 || t.FullAccess || (t.SandboxID != "" && t.SandboxID.Validate() != nil) || (t.ResourceID != "" && t.ResourceID.Validate() != nil) {
			return r, core.Invalid("target", "bounded authorized file target required; Full Access transfers are not supported")
		}
	}
	local := func(id core.ID) bool { return id == "" || id == "local" }
	if local(r.Source.SandboxID) == local(r.Destination.SandboxID) {
		return r, core.Invalid("target", "transfer requires one local and one explicit remote endpoint")
	}
	if r.Overwrite && r.ExpectedVersion == "" {
		return r, core.Invalid("expected_version", "required when replacing a file")
	}
	return r, nil
}
func fileTransferDefinition() Definition {
	target := `{"type":"object","properties":{"sandbox_id":{"type":"string"},"resource_id":{"type":"string"},"path":{"type":"string","maxLength":4096}},"required":["path"],"additionalProperties":false}`
	return Definition{Name: "file_transfer", Description: "Transfer one binary or text file (at most 20 MiB) between the local computer and an explicit remote computer. Both endpoints are authorized independently. Remote endpoints require sandbox_id and an authorized directory resource_id. No remote workspace, remote-to-remote copy, directory sync or automatic overwrite. Parent directories must exist. overwrite is required; replacing a file also requires expected_version. Optional source_version checks the source read. Returns destination version and SHA256; an uncertain operation must be reconciled before retrying.", InputSchema: schema(`"source":`+target+`,"destination":`+target+`,"overwrite":{"type":"boolean"},"source_version":{"type":"string"},"expected_version":{"type":"string"}`, `"source","destination","overwrite"`), RequiredActions: []string{"read", "write"}, SideEffect: true}
}
