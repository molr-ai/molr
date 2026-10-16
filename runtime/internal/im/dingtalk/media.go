package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/storage/files"
)

type mediaRef struct{ Code, Name string }

func parseIncoming(m Incoming) (string, []mediaRef) {
	if m.MsgType == "text" {
		return m.Text.Content, nil
	}
	var content struct {
		DownloadCode        string `json:"downloadCode"`
		PictureDownloadCode string `json:"pictureDownloadCode"`
		FileName            string `json:"fileName"`
		Recognition         string `json:"recognition"`
		RichText            []struct {
			Type                string `json:"type"`
			Text                string `json:"text"`
			DownloadCode        string `json:"downloadCode"`
			PictureDownloadCode string `json:"pictureDownloadCode"`
		} `json:"richText"`
	}
	if json.Unmarshal(m.Content, &content) != nil {
		return "", nil
	}
	if m.MsgType == "audio" && content.Recognition != "" {
		return content.Recognition, nil
	}
	if m.MsgType == "richText" {
		var text strings.Builder
		var refs []mediaRef
		for _, part := range content.RichText {
			if part.Text != "" {
				text.WriteString(part.Text)
			}
			code := part.DownloadCode
			if code == "" {
				code = part.PictureDownloadCode
			}
			if code != "" && len(refs) < 20 {
				refs = append(refs, mediaRef{code, "image.jpg"})
			}
		}
		return text.String(), refs
	}
	switch m.MsgType {
	case "picture", "file", "audio", "video":
		code := content.DownloadCode
		if code == "" {
			code = content.PictureDownloadCode
		}
		name := content.FileName
		if name == "" {
			name = map[string]string{"picture": "image.jpg", "audio": "voice.amr", "video": "video.mp4", "file": "attachment"}[m.MsgType]
		}
		if code != "" {
			return "", []mediaRef{{code, name}}
		}
	}
	return "", nil
}
func (s *Service) receiveMedia(ctx context.Context, b Binding, key string, refs []mediaRef) ([]core.ID, string) {
	var ids []core.ID
	warning := ""
	if len(refs) == 0 {
		return nil, ""
	}
	secret, err := s.open(b)
	if err != nil {
		return nil, "[附件未读取 / Attachment could not be read.]"
	}
	for i, ref := range refs {
		if s.options.Files == nil {
			warning = "[附件未读取 / Attachment could not be read.]"
			continue
		}
		if s.AuthorizeChannel(ctx, b.OwnerID, b.ConversationID, b.ID) != nil {
			return ids, "[连接已失效，附件未读取 / Connection revoked; attachment not read.]"
		}
		raw, kind, err := s.options.Client.Download(ctx, Credentials{b.ClientID, secret}, ref.Code, s.options.MaxMediaBytes)
		if err == nil {
			name := path.Base(strings.ReplaceAll(ref.Name, "\\", "/"))
			if name == "." || name == "/" || len(name) > 255 {
				name = "attachment"
			}
			f, e := s.options.Files.UploadChannel(ctx, b.OwnerID, b.ConversationID, b.ID, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "dingtalk-" + hash(key+"/"+strconv.Itoa(i))}, Name: name, MediaType: kind, Size: int64(len(raw)), Body: bytes.NewReader(raw)})
			err = e
			if err == nil {
				ids = append(ids, f.ID)
			}
		}
		if err != nil {
			warning = "[部分附件接收失败，失败的内容未读取。 / Some attachments could not be received; their content was not read.]"
		}
	}
	return ids, warning
}
func (h *HTTPClient) Download(ctx context.Context, c Credentials, code string, limit int64) ([]byte, string, error) {
	if len(code) == 0 || len(code) > 8192 {
		return nil, "", &apiError{Reason: "attachment_failed"}
	}
	access, err := h.access(ctx, c)
	if err != nil {
		return nil, "", err
	}
	var out struct {
		DownloadURL string `json:"downloadUrl"`
	}
	if err = h.request(ctx, "/v1.0/robot/messageFiles/download", access, map[string]string{"downloadCode": code, "robotCode": c.ClientID}, &out); err != nil {
		return nil, "", err
	}
	u, err := url.Parse(out.DownloadURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") || !(strings.HasSuffix(strings.ToLower(u.Hostname()), ".dingtalk.com") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".aliyuncs.com")) {
		return nil, "", &apiError{Reason: "attachment_failed"}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", out.DownloadURL, nil)
	if err != nil {
		return nil, "", err
	}
	client := h.http("https://" + u.Host)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength > limit {
		return nil, "", &apiError{Reason: "attachment_failed"}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, "", &apiError{Reason: "attachment_failed"}
	}
	kind, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if kind == "" {
		kind = http.DetectContentType(data)
	}
	return data, kind, nil
}
func (h *HTTPClient) Upload(ctx context.Context, c Credentials, name string, data []byte) (string, error) {
	access, err := h.access(ctx, c)
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("media", name)
	if err != nil {
		return "", err
	}
	if _, err = part.Write(data); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	const origin = "https://oapi.dingtalk.com"
	req, err := http.NewRequestWithContext(ctx, "POST", origin+"/media/upload?type=file&access_token="+url.QueryEscape(access), &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := h.http(origin)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return "", &apiError{Reason: "attachment_failed"}
	}
	defer resp.Body.Close()
	var out struct {
		Errcode int    `json:"errcode"`
		MediaID string `json:"media_id"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&out) != nil || out.Errcode != 0 || out.MediaID == "" {
		return "", &apiError{Reason: "attachment_failed"}
	}
	return out.MediaID, nil
}
func (s *Service) sendMedia(ctx context.Context, b Binding, c Credentials, id core.ID) error {
	failed := &apiError{Reason: "attachment_failed"}
	if s.options.Files == nil {
		return failed
	}
	download, err := s.options.Files.OpenChannel(ctx, b.OwnerID, b.ConversationID, b.ID, id)
	if err != nil {
		return failed
	}
	defer download.Body.Close()
	if download.File.Size > s.options.MaxMediaBytes {
		return failed
	}
	data, err := io.ReadAll(io.LimitReader(download.Body, s.options.MaxMediaBytes+1))
	if err != nil || int64(len(data)) > s.options.MaxMediaBytes {
		return failed
	}
	media, err := s.options.Client.Upload(ctx, c, download.File.Name, data)
	if err != nil {
		return failed
	}
	ext := strings.TrimPrefix(path.Ext(download.File.Name), ".")
	if ext == "" {
		ext = "file"
	}
	// Deliver images as downloadable files; no public Molr file URLs are exposed.
	return s.options.Client.Send(ctx, c, b.UserID, "sampleFile", map[string]any{"mediaId": media, "fileName": download.File.Name, "fileType": ext})
}
