package weixin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestMediaEncryptionAndBounds(t *testing.T) {
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	plain, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	encrypted, err := cryptMedia(plain, key, true)
	must(t, err)
	if hex.EncodeToString(encrypted[:16]) != "69c4e0d86a7b0430d8cdb78070b4c55a" {
		t.Fatal("AES wire format differs from NIST vector")
	}
	decoded, err := cryptMedia(encrypted, key, false)
	must(t, err)
	if !bytes.Equal(decoded, plain) {
		t.Fatal("round trip")
	}
	for _, encoded := range []string{base64.StdEncoding.EncodeToString(key), base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(key)))} {
		decoded, err := decodeKey(encoded)
		must(t, err)
		if !bytes.Equal(decoded, key) {
			t.Fatal("key encoding")
		}
	}
	for _, raw := range [][]byte{nil, {1}, bytes.Repeat([]byte{0}, 16)} {
		if _, err := cryptMedia(raw, key, false); err == nil {
			t.Fatal("accepted invalid ciphertext")
		}
	}
	if _, err := decodeKey("bad-key"); err == nil {
		t.Fatal("accepted invalid key")
	}
}

func TestMediaCDNUploadDownloadProtocol(t *testing.T) {
	image, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2nSIAAAAASUVORK5CYII=")
	for _, file := range []MediaFile{{Name: "pixel.png", Type: "image/png", Data: image}, {Name: "report.pdf", Type: "application/pdf", Data: []byte("%PDF-1.4\nexample")}, {Name: "empty.txt", Type: "text/plain"}} {
		t.Run(file.Name, func(t *testing.T) {
			var key, ciphertext []byte
			var sent Item
			calls := 0
			sendFailure := false
			h := HTTPClient{client: func(string) *http.Client {
				return &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					result := "{}"
					header := http.Header{}
					switch r.URL.Path {
					case "/ilink/bot/getuploadurl":
						var input struct {
							AESKey    string `json:"aeskey"`
							RawSize   int    `json:"rawsize"`
							MediaType int    `json:"media_type"`
						}
						must(t, json.NewDecoder(r.Body).Decode(&input))
						var err error
						key, err = hex.DecodeString(input.AESKey)
						must(t, err)
						if input.RawSize != len(file.Data) || r.Header.Get("Authorization") != "Bearer token" {
							t.Fatal("upload metadata")
						}
						if (input.MediaType == 1) != (file.Type == "image/png") {
							t.Fatal("media route")
						}
						result = `{"upload_param":"ticket"}`
					case "/c2c/upload":
						if r.Header.Get("Authorization") != "" || r.URL.Query().Get("encrypted_query_param") != "ticket" || r.Method != "POST" {
							t.Fatal("CDN credential or request")
						}
						var err error
						ciphertext, err = io.ReadAll(r.Body)
						must(t, err)
						decoded, err := cryptMedia(ciphertext, key, false)
						must(t, err)
						if !bytes.Equal(decoded, file.Data) {
							t.Fatal("uploaded bytes changed")
						}
						header.Set("x-encrypted-param", "download-ticket")
					case "/ilink/bot/sendmessage":
						if sendFailure {
							return nil, errors.New("connection lost after write")
						}
						var input struct {
							Msg struct {
								Items   []Item `json:"item_list"`
								ID      string `json:"client_id"`
								Context string `json:"context_token"`
							} `json:"msg"`
						}
						must(t, json.NewDecoder(r.Body).Decode(&input))
						sent = input.Msg.Items[0]
						if input.Msg.ID != "delivery" || input.Msg.Context != "context" {
							t.Fatal("send context")
						}
					case "/c2c/download":
						if r.Header.Get("Authorization") != "" {
							t.Fatal("CDN leaked credentials")
						}
						return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(ciphertext)), Header: header}, nil
					default:
						t.Fatal("unexpected request", r.URL.Path)
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(result)), Header: header}, nil
				})}
			}}
			must(t, sendError(h.SendMedia(t.Context(), baseURL, "token", "user", "context", "delivery", file)))
			received, err := h.Download(t.Context(), sent, 1024)
			must(t, err)
			if !bytes.Equal(received.Data, file.Data) || received.Name != file.Name && file.Type != "image/png" {
				t.Fatal("received file changed")
			}
			if len(file.Data) > 2 {
				if _, err := h.Download(t.Context(), sent, 1); err == nil {
					t.Fatal("download limit ignored")
				}
			}
			before := calls
			sent.File.Media.URL = "https://127.0.0.1/private"
			sent.Image.Media.URL = sent.File.Media.URL
			if _, err := h.Download(t.Context(), sent, 1024); err == nil || calls != before {
				t.Fatal("unsafe CDN reached transport")
			}
			sendFailure = true
			if failure(sendError(h.SendMedia(t.Context(), baseURL, "token", "user", "context", "delivery", file))) != "unknown" {
				t.Fatal("uncertain media send marked failed")
			}
		})
	}
}

func TestMediaUploadFailureDoesNotSendMessage(t *testing.T) {
	calls := 0
	h := HTTPClient{client: func(string) *http.Client {
		return &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.Path != "/ilink/bot/getuploadurl" {
				t.Fatal("sent message after failed upload")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"upload_full_url":"https://evil.example/upload"}`)), Header: http.Header{}}, nil
		})}
	}}
	if failure(sendError(h.SendMedia(context.Background(), baseURL, "token", "user", "context", "id", MediaFile{Name: "test", Data: []byte("x")}))) != "attachment_failed" || calls != 1 {
		t.Fatal("upload failure classification")
	}
}

func TestUploadRetriesOnlyBeforeMessageSubmission(t *testing.T) {
	for _, scenario := range []string{"server-error", "missing-header", "client-error", "send-timeout", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			uploads, sends := 0, 0
			var first []byte
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h := HTTPClient{client: func(string) *http.Client {
				return &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
					body := "{}"
					status := 200
					headers := http.Header{}
					switch r.URL.Path {
					case "/ilink/bot/getuploadurl":
						body = `{"upload_param":"ticket"}`
					case "/c2c/upload":
						uploads++
						data, _ := io.ReadAll(r.Body)
						if uploads == 1 {
							first = data
						} else if !bytes.Equal(first, data) {
							t.Fatal("upload retry changed ciphertext")
						}
						headers.Set("x-encrypted-param", "download")
						if scenario == "client-error" {
							status = 403
						} else if scenario == "server-error" && uploads < 3 {
							status = 503
						} else if scenario == "missing-header" && uploads < 3 {
							headers.Del("x-encrypted-param")
						} else if scenario == "cancel" {
							cancel()
							return nil, ctx.Err()
						}
					case "/ilink/bot/sendmessage":
						sends++
						if scenario == "send-timeout" {
							return nil, errors.New("lost acknowledgement")
						}
					default:
						t.Fatal(r.URL.Path)
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: headers}, nil
				})}
			}}
			_, err := h.SendMedia(ctx, baseURL, "token", "user", "context", "stable", MediaFile{Name: "file.txt", Data: []byte("hello")})
			switch scenario {
			case "server-error", "missing-header":
				must(t, err)
				if uploads != 3 || sends != 1 {
					t.Fatal(uploads, sends)
				}
			case "client-error", "cancel":
				if err == nil || uploads != 1 || sends != 0 {
					t.Fatal(err, uploads, sends)
				}
			case "send-timeout":
				if failure(err) != "unknown" || uploads != 1 || sends != 1 {
					t.Fatal("message submission retried", err, uploads, sends)
				}
			}
		})
	}
}
func TestVoiceDownloadWithoutTranscriptProducesWAV(t *testing.T) {
	data, err := os.ReadFile("../../voicecodec/testdata/tone.silk")
	must(t, err)
	key := bytes.Repeat([]byte{7}, 16)
	encrypted, err := cryptMedia(data, key, true)
	must(t, err)
	h := HTTPClient{client: func(string) *http.Client {
		return &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(encrypted)), Header: http.Header{}}, nil
		})}
	}}
	item := Item{Type: 3}
	item.Voice.Media = CDNMedia{Query: "voice", Key: base64.StdEncoding.EncodeToString(key)}
	file, err := h.Download(t.Context(), item, 20<<20)
	must(t, err)
	if file.Type != "audio/wav" || file.Name != "weixin-voice.wav" || string(file.Data[:4]) != "RIFF" {
		t.Fatal("voice not decoded")
	}
	encrypted, err = cryptMedia([]byte("broken silk"), key, true)
	must(t, err)
	file, err = h.Download(t.Context(), item, 20<<20)
	must(t, err)
	if file.Type != "audio/silk" || string(file.Data) != "broken silk" {
		t.Fatal("failed decode discarded original")
	}
}
