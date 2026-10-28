package runtimeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/workspace"
)

// WorkspaceUpload writes one version-checked file. Unknown results must be inspected, never replayed automatically.
func (c *Client) WorkspaceUpload(ctx context.Context, id core.ID, path, version string, data []byte) (workspace.Entry, error) {
	var entry workspace.Entry
	if id.Validate() != nil || len(data) > 64<<20 {
		return entry, errors.New("工作区上传参数或文件大小无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := c.request(ctx, "PUT", "/api/assistants/"+string(id)+"/workspace/content?path="+url.QueryEscape(path)+"&version="+url.QueryEscape(version), nil)
	if err != nil {
		return entry, err
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.ContentLength = int64(len(data))
	if len(data) == 0 {
		req.Body = http.NoBody
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return entry, errors.New("上传结果未确认，请先刷新目标文件核对，勿直接重复上传")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return entry, responseError(resp)
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&entry); err != nil {
		return entry, errors.New("上传结果未确认，请先刷新目标文件核对")
	}
	return entry, nil
}

// WorkspaceDownload streams a selected version into a caller-owned temporary file.
func (c *Client) WorkspaceDownload(ctx context.Context, id core.ID, entry workspace.Entry, dst io.Writer) error {
	if id.Validate() != nil || entry.Size < 0 || entry.Version == "" {
		return errors.New("工作区文件信息无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	path := "/api/assistants/" + string(id) + "/workspace/content?path=" + url.QueryEscape(entry.Path) + "&version=" + url.QueryEscape(entry.Version)
	req, err := c.request(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("工作区下载失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return responseError(resp)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, entry.Size+1))
	if err != nil {
		return err
	}
	if n != entry.Size {
		return errors.New("文件大小已变化，请刷新后重新下载")
	}
	var after workspace.Entry
	if err = c.Do(ctx, "GET", "/api/assistants/"+string(id)+"/workspace?action=stat&path="+url.QueryEscape(entry.Path), nil, &after); err != nil {
		return err
	}
	if after.Version != entry.Version {
		return errors.New("文件下载期间已变化，请重新下载")
	}
	return nil
}
