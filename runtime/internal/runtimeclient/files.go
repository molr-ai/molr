package runtimeclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"molr.ai/runtime/internal/storage/files"
)

type transferReader struct {
	reader   io.Reader
	done     int64
	progress func(int64)
}

func (r *transferReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.done += int64(n)
	if r.progress != nil {
		r.progress(r.done)
	}
	return n, err
}

// Upload sends an immutable file snapshot with the caller's retry key.
func (c *Client) Upload(ctx context.Context, name string, data []byte, key string, progress func(int64)) (files.File, error) {
	var file files.File
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := c.request(ctx, "POST", "/api/files", nil)
	if err != nil {
		return file, err
	}
	req.Body = io.NopCloser(&transferReader{reader: bytes.NewReader(data), progress: progress})
	req.ContentLength = int64(len(data))
	if len(data) == 0 {
		req.Body = http.NoBody
	}
	req.Header.Set("Content-Type", http.DetectContentType(data))
	req.Header.Set("X-File-Name", url.PathEscape(name))
	req.Header.Set("X-File-Name-Encoding", "percent")
	req.Header.Set("Idempotency-Key", key)
	resp, err := c.http.Do(req)
	if err != nil {
		return file, errors.New("附件上传未确认，请重试")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return file, responseError(resp)
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&file); err != nil {
		return file, errors.New("Runtime 返回了无效附件信息")
	}
	sum := sha256.Sum256(data)
	if file.ID == "" || file.Size != int64(len(data)) || file.State != files.Ready || !strings.EqualFold(file.SHA256, hex.EncodeToString(sum[:])) {
		return files.File{}, errors.New("附件上传校验失败，请重试")
	}
	return file, nil
}

// Download streams authenticated bytes and verifies the advertised size and hash.
// The caller owns a temporary destination and publishes it only after success.
func (c *Client) Download(ctx context.Context, file files.File, dst io.Writer, progress func(int64)) error {
	if file.ID.Validate() != nil || file.Size < 0 || len(file.SHA256) != 64 {
		return errors.New("附件信息无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := c.request(ctx, "GET", "/api/files/"+url.PathEscape(string(file.ID))+"/content", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("附件下载失败，请重试")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError(resp)
	}
	hash := sha256.New()
	r := &transferReader{reader: io.LimitReader(resp.Body, file.Size+1), progress: progress}
	n, err := io.Copy(io.MultiWriter(dst, hash), r)
	if err != nil {
		return err
	}
	if n != file.Size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), file.SHA256) {
		return errors.New("附件不完整或校验失败，请重试")
	}
	return nil
}
