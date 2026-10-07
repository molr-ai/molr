package app

import (
	"context"
	"encoding/json"
	"io"
	"path"
	"strconv"
	"strings"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
)

type conversationAttachments struct {
	files         files.Service
	workspace     *resourceFileTools
	conversations conversation.Service
	maxBytes      int64
}

func (a *conversationAttachments) Get(ctx context.Context, p core.Principal, id core.ID) (files.File, error) {
	return a.files.Get(ctx, p, id)
}
func (a *conversationAttachments) Upload(ctx context.Context, e core.Execution, r tools.Attachment, index int) (core.ID, error) {
	if targetSandbox(r.SandboxID) != "local" {
		return "", core.Invalid("sandbox_id", "download remote attachments to the local workspace first")
	}

	root, name, err := a.workspace.resolve(ctx, e, tools.FileTarget{SandboxID: r.SandboxID, Path: r.Path}, policy.Read)
	if err != nil {
		return "", err
	}
	f, err := openResourceFile(root, name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return "", core.NewError(core.Unavailable, "file metadata unavailable")
	}
	filename := r.Name
	if filename == "" {
		filename = path.Base(r.Path)
	}
	uploaded, err := a.files.Upload(ctx, e.Principal, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: string(e.OperationID) + "/attachment/" + strconv.Itoa(index)}, Name: filename, MediaType: r.MediaType, Size: stat.Size(), Body: f})
	return uploaded.ID, err
}
func (a *conversationAttachments) Import(ctx context.Context, e core.Execution, r tools.ConversationRequest) (json.RawMessage, error) {
	if targetSandbox(r.SandboxID) != "local" {
		return nil, core.Invalid("sandbox_id", "save attachments locally then use file_transfer")
	}
	// Access must come from this exact conversation, not merely another accessible file.
	cursor := ""
	found := false
	for !found {
		page, err := a.conversations.Messages(ctx, e.Principal, conversation.MessageQuery{ConversationID: r.ConversationID, Descending: true, Page: core.PageRequest{Limit: 200, Cursor: cursor}})
		if err != nil {
			return nil, err
		}
		for _, m := range page.Items {
			for _, id := range m.Content.AttachmentIDs {
				if id == r.FileID {
					found = true
				}
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if !found {
		return nil, core.NewError(core.NotFound, "attachment not found in this conversation")
	}
	download, err := a.files.Open(ctx, e.Principal, r.FileID)
	if err != nil {
		return nil, err
	}
	defer download.Body.Close()
	limit := a.maxBytes
	if limit <= 0 {
		limit = 20 << 20
	}
	if download.File.Size > limit {
		return nil, core.Invalid("attachment", "attachment exceeds import limit")
	}
	data, err := io.ReadAll(io.LimitReader(download.Body, limit+1))
	if err != nil {
		return nil, core.NewError(core.Unavailable, "attachment read failed")
	}
	if int64(len(data)) > limit {
		return nil, core.Invalid("attachment", "attachment exceeds import limit")
	}
	target := tools.FileTarget{SandboxID: r.SandboxID, Path: r.Path}
	if a.workspace.router != nil {
		out, remote, err := a.workspace.router.file(ctx, e, "upload", target, sandboxUpload{Target: target, Data: data}, "")
		if remote {
			if err != nil {
				return nil, err
			}
			var blob sandboxBlob
			if err = json.Unmarshal(out, &blob); err != nil {
				return nil, err
			}
			canonical := r.Path
			if !strings.HasPrefix(canonical, "/") {
				canonical = "/work/" + canonical
			}
			return json.Marshal(map[string]any{"File": download.File, "Path": canonical, "Version": blob.Version})
		}
	}

	root, name, err := a.workspace.resolve(ctx, e, target, policy.Write)
	if err != nil {
		return nil, err
	}
	unlock, err := a.workspace.lock(ctx, root+"/"+name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	version, err := replaceResourceFile(ctx, root, name, string(data), false, "", func() error { _, _, err := a.workspace.resolve(ctx, e, target, policy.Write); return err })
	if err != nil {
		return nil, err
	}
	canonical := r.Path
	if !strings.HasPrefix(canonical, "/") {
		canonical = "/work/" + canonical
	}
	return json.Marshal(map[string]any{"File": download.File, "Path": canonical, "Version": version})
}
