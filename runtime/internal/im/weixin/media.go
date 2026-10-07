package weixin

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5" // Tencent upload protocol requires the plaintext MD5.
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/voicecodec"
)

const cdnBase = "https://novac2c.cdn.weixin.qq.com/c2c"
const mediaLimit int64 = 100 << 20

type CDNMedia struct {
	Query       string `json:"encrypt_query_param,omitempty"`
	Key         string `json:"aes_key,omitempty"`
	EncryptType int    `json:"encrypt_type,omitempty"`
	URL         string `json:"full_url,omitempty"`
}
type MediaItem struct {
	Media   CDNMedia `json:"media"`
	AESKey  string   `json:"aeskey,omitempty"`
	Name    string   `json:"file_name,omitempty"`
	Length  string   `json:"len,omitempty"`
	MidSize int      `json:"mid_size,omitempty"`
}
type MediaFile struct {
	Name, Type string
	Data       []byte
}
type MediaClient interface {
	Download(context.Context, Item, int64) (MediaFile, error)
	SendMedia(context.Context, string, string, string, string, string, MediaFile) (string, error)
}
type cdnError struct{ retry bool }

func (e *cdnError) Error() string { return "Weixin CDN request failed" }

type mediaError struct{}

func (*mediaError) Error() string { return "attachment could not be prepared for sending" }

func decodeKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(key) == 32 {
		key, err = hex.DecodeString(string(key))
	}
	if err != nil || len(key) != aes.BlockSize {
		return nil, errors.New("invalid media key")
	}
	return key, nil
}

// AES-128-ECB with PKCS#7 is required by the Weixin wire protocol.
func cryptMedia(data, key []byte, encrypt bool) ([]byte, error) {
	if len(key) != aes.BlockSize {
		return nil, errors.New("invalid media key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if encrypt {
		padding := aes.BlockSize - len(data)%aes.BlockSize
		data = append(append([]byte(nil), data...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	} else if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("invalid media ciphertext")
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		if encrypt {
			block.Encrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
		} else {
			block.Decrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
		}
	}
	if !encrypt {
		n := int(out[len(out)-1])
		if n < 1 || n > aes.BlockSize || !bytes.Equal(out[len(out)-n:], bytes.Repeat([]byte{byte(n)}, n)) {
			return nil, errors.New("invalid media padding")
		}
		out = out[:len(out)-n]
	}
	return out, nil
}
func (h HTTPClient) cdn(ctx context.Context, raw, method string, data []byte, limit int64) ([]byte, http.Header, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 65536 || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".cdn.weixin.qq.com") {
		return nil, nil, errors.New("invalid Weixin CDN URL")
	}
	origin := "https://" + u.Host
	client := platform.PublicHTTPClient(origin)
	client.Timeout = 60 * time.Second
	if h.client != nil {
		client = h.client(origin)
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, method, raw, bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	// Bot authorization is never sent to the CDN. The public client also forbids redirects.
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, &cdnError{retry: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, &cdnError{retry: resp.StatusCode >= 500 || resp.StatusCode == 408 || resp.StatusCode == 429}
	}
	if resp.ContentLength > limit {
		return nil, nil, errors.New("Weixin attachment too large")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, nil, &cdnError{retry: err != nil}
	}
	return body, resp.Header, nil
}
func safeMediaName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" || len(name) > 240 {
		return "attachment.bin"
	}
	return name
}
func (h HTTPClient) Download(ctx context.Context, item Item, limit int64) (MediaFile, error) {
	if limit <= 0 || limit > mediaLimit {
		limit = mediaLimit
	}
	ref := item.File
	switch item.Type {
	case 2:
		ref = item.Image
	case 3:
		ref = MediaItem{Media: item.Voice.Media}
	case 4:
	case 5:
		ref = item.Video
	default:
		return MediaFile{}, errors.New("unsupported media")
	}
	raw := ref.Media.URL
	if raw == "" && ref.Media.Query != "" {
		raw = cdnBase + "/download?encrypted_query_param=" + url.QueryEscape(ref.Media.Query)
	}
	var key []byte
	var err error
	if item.Type == 2 && ref.AESKey != "" {
		key, err = hex.DecodeString(ref.AESKey)
	} else if ref.Media.Key != "" {
		key, err = decodeKey(ref.Media.Key)
	} else if item.Type != 2 {
		err = errors.New("missing media key")
	}
	if err != nil || (len(key) != 0 && len(key) != aes.BlockSize) || (ref.Media.EncryptType != 0 && ref.Media.EncryptType != 1) {
		return MediaFile{}, errors.New("invalid media encryption")
	}
	data, _, err := h.cdn(ctx, raw, http.MethodGet, nil, limit+aes.BlockSize)
	if err != nil {
		return MediaFile{}, err
	}
	if len(key) > 0 {
		data, err = cryptMedia(data, key, false)
	}
	if err != nil {
		return MediaFile{}, err
	}
	if int64(len(data)) > limit {
		return MediaFile{}, errors.New("Weixin attachment too large")
	}
	name := safeMediaName(ref.Name)
	mediaType := http.DetectContentType(data)
	if item.Type == 2 {
		if !strings.HasPrefix(mediaType, "image/") {
			return MediaFile{}, errors.New("invalid Weixin image")
		}
		ext := ".img"
		switch mediaType {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		}
		name = "weixin-image" + ext
	} else if item.Type == 3 {
		name = "weixin-voice.silk"
		mediaType = "audio/silk"
		if wav, e := voicecodec.WAV(ctx, data); e == nil && int64(len(wav)) <= limit {
			data = wav
			name = "weixin-voice.wav"
			mediaType = "audio/wav"
		}
	} else if item.Type == 5 {
		name = "weixin-video.mp4"
		mediaType = "video/mp4"
	} else if declared := mime.TypeByExtension(path.Ext(name)); declared != "" {
		mediaType = declared
	}
	return MediaFile{Name: name, Type: mediaType, Data: data}, nil
}
func (h HTTPClient) SendMedia(ctx context.Context, origin, token, user, contextToken, clientID string, file MediaFile) (string, error) {
	if int64(len(file.Data)) > mediaLimit {
		return "", &mediaError{}
	}
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", &mediaError{}
	}
	ciphertext, err := cryptMedia(file.Data, key[:], true)
	if err != nil {
		return "", &mediaError{}
	}
	filekey := string(newID())
	sum := md5.Sum(file.Data)
	mediaType := 3
	// Sniff image bytes rather than relying on an arbitrary declared MIME type.
	if strings.HasPrefix(file.Type, "image/") && strings.HasPrefix(http.DetectContentType(file.Data), "image/") {
		mediaType = 1
	}
	var upload struct {
		URL     string `json:"upload_full_url"`
		Param   string `json:"upload_param"`
		Ret     int    `json:"ret"`
		Errcode int    `json:"errcode"`
	}
	err = h.request(ctx, origin, "getuploadurl", token, map[string]any{"base_info": metadata(), "filekey": filekey, "media_type": mediaType, "to_user_id": user, "rawsize": len(file.Data), "rawfilemd5": hex.EncodeToString(sum[:]), "filesize": len(ciphertext), "no_need_thumb": true, "aeskey": hex.EncodeToString(key[:])}, &upload)
	if err != nil || upload.Ret != 0 || upload.Errcode != 0 {
		return "", &mediaError{}
	}
	raw := strings.TrimSpace(upload.URL)
	if raw == "" && upload.Param != "" {
		raw = cdnBase + "/upload?encrypted_query_param=" + url.QueryEscape(upload.Param) + "&filekey=" + url.QueryEscape(filekey)
	}
	headers, err := h.uploadCDN(ctx, raw, ciphertext)
	if err != nil {
		return "", &mediaError{}
	}
	ref := MediaItem{Media: CDNMedia{Query: headers.Get("x-encrypted-param"), Key: base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(key[:]))), EncryptType: 1}}
	var item any
	if mediaType == 1 {
		ref.MidSize = len(ciphertext)
		item = map[string]any{"type": 2, "image_item": ref}
	} else {
		ref.Name = safeMediaName(file.Name)
		ref.Length = strconv.Itoa(len(file.Data))
		item = map[string]any{"type": 4, "file_item": ref}
	}
	// Only this final call can have an uncertain message-delivery outcome.
	return h.sendItem(ctx, origin, token, user, contextToken, clientID, item)
}

// Retry only the encrypted upload, with the same file key and bytes. Never retry
// the final sendmessage here: a lost acknowledgement can mean it was delivered.
func (h HTTPClient) uploadCDN(ctx context.Context, raw string, ciphertext []byte) (http.Header, error) {
	for attempt := 0; attempt < 3; attempt++ {
		_, headers, err := h.cdn(ctx, raw, http.MethodPost, ciphertext, 64<<10)
		if err == nil && headers.Get("x-encrypted-param") != "" {
			return headers, nil
		}
		retry := err == nil
		var failure *cdnError
		if errors.As(err, &failure) {
			retry = failure.retry
		}
		if !retry || ctx.Err() != nil || attempt == 2 {
			return nil, &mediaError{}
		}
		if !sleep(ctx, time.Duration(attempt+1)*250*time.Millisecond) {
			return nil, ctx.Err()
		}
	}
	return nil, &mediaError{}
}
