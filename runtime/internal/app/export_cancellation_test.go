package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/storage/files"
	"testing"
	"time"
)

type cancelExportBody struct {
	reader           *bytes.Reader
	cancel           context.CancelFunc
	reads, bytesRead int
	closed           bool
	eofWithData      bool
	onClose          context.CancelFunc
	readErr          error
}

func (b *cancelExportBody) Read(p []byte) (int, error) {
	b.reads++
	if b.readErr != nil {
		return 0, b.readErr
	}
	n, err := b.reader.Read(p)
	b.bytesRead += n
	if b.reads == 1 && b.cancel != nil {
		b.cancel()
	}
	if b.eofWithData && b.reader.Len() == 0 {
		err = io.EOF
	}
	return n, err
}
func (b *cancelExportBody) Close() error {
	b.closed = true
	if b.onClose != nil {
		b.onClose()
	}
	return nil
}

type cancelExportFiles struct {
	files.Service
	body           io.ReadCloser
	descriptor     files.File
	opens, uploads int
}

func (s *cancelExportFiles) Open(context.Context, core.Principal, core.ID) (files.Download, error) {
	s.opens++
	return files.Download{File: s.descriptor, Body: s.body}, nil
}
func (s *cancelExportFiles) Upload(ctx context.Context, _ core.Principal, _ files.UploadRequest) (files.File, error) {
	s.uploads++
	return files.File{}, persistence.CheckContext(ctx)
}
func TestExportStopsCopyingArtifactAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	content := bytes.Repeat([]byte("export fixture "), 150000)
	body := &cancelExportBody{reader: bytes.NewReader(content), cancel: cancel}
	file := files.File{ID: "artifact", OwnerID: "owner", State: files.Ready, Size: int64(len(content)), Name: "fixture.txt"}
	service := &cancelExportFiles{body: body, descriptor: file}
	descriptor, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	bundle := settings.ExportBundle{ID: "canceled-export", IncludeArtifacts: true, Records: map[string][]json.RawMessage{files.Bucket: {descriptor}}}
	_, err = saveExport(ctx, service, core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}, bundle)
	if core.CodeOf(err) != core.Canceled {
		t.Fatalf("cancellation error=%v", err)
	}
	if !body.closed {
		t.Fatal("artifact reader not closed")
	}
	if body.reads > 1 || service.uploads != 0 {
		t.Fatalf("canceled export performed %d reads (%d bytes) and %d upload attempts", body.reads, body.bytesRead, service.uploads)
	}
}

func TestExportCancellationBoundaries(t *testing.T) {
	for _, when := range []string{"before", "deadline", "last-read", "close", "read-error"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			body := &cancelExportBody{reader: bytes.NewReader([]byte("fixture"))}
			want := core.Canceled
			switch when {
			case "before":
				cancel()
			case "deadline":
				var stop context.CancelFunc
				ctx, stop = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				defer stop()
				want = core.DeadlineExceeded
			case "last-read":
				body.cancel = cancel
				body.eofWithData = true
			case "close":
				body.onClose = cancel
			case "read-error":
				body.readErr = io.ErrUnexpectedEOF
			}
			file := files.File{ID: "artifact", OwnerID: "owner", State: files.Ready, Size: 7}
			service := &cancelExportFiles{body: body, descriptor: file}
			raw, _ := json.Marshal(file)
			bundle := settings.ExportBundle{ID: "boundary", IncludeArtifacts: true, Records: map[string][]json.RawMessage{files.Bucket: {raw}}}
			_, err := saveExport(ctx, service, core.Principal{OwnerID: "owner"}, bundle)
			if when == "read-error" {
				if err != io.ErrUnexpectedEOF {
					t.Fatal("read failure lost", err)
				}
			} else if core.CodeOf(err) != want {
				t.Fatalf("want %s got %v", want, err)
			}
			if service.uploads != 0 {
				t.Fatal("failed export attempted upload")
			}
			if when == "before" || when == "deadline" {
				if service.opens != 0 || body.reads != 0 {
					t.Fatal("expired export opened input")
				}
			} else if !body.closed {
				t.Fatal("opened reader was not closed")
			}
		})
	}
}
