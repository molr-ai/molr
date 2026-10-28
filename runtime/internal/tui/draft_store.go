package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/storage/files"
)

const draftCacheLimit int64 = 256 << 20

type diskPaste struct {
	Start, End  int
	Label, Text string
}
type diskAttachment struct {
	Key, Name string
	SHA256    string
	File      files.File
	Pasted    bool
}
type diskPending struct {
	ClearVersion uint64
	Text, Key    string
	Attachments  []core.ID
}
type diskDraft struct {
	Text         string
	Caret        int
	Pastes       []diskPaste
	Attachments  []diskAttachment
	Pending      diskPending
	ClearVersion uint64
}
type draftStore struct {
	root     string
	lock     *os.File
	last     map[core.ID]string
	restored map[core.ID]bool
	err      error
	selected core.ID
}

func openDraftStore(root, connection string, owner core.ID) *draftStore {
	sum := sha256.Sum256([]byte(connection + "\x00" + string(owner)))
	s := &draftStore{root: filepath.Join(root, "drafts", connection, hex.EncodeToString(sum[:])), last: map[core.ID]string{}, restored: map[core.ID]bool{}}
	if err := os.MkdirAll(s.root, 0700); err != nil {
		s.err = err
		return s
	}
	f, err := os.OpenFile(filepath.Join(s.root, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		s.err = err
		return s
	}
	if err = lockDraftFile(f); err != nil {
		f.Close()
		s.err = fmt.Errorf("此账号草稿正在被另一个 TUI 使用，请关闭该实例后重新连接")
		return s
	}
	s.lock = f
	if raw, err := privateRead(filepath.Join(s.root, "selected"), 256); err == nil {
		if id := core.ID(raw); id.Validate() == nil {
			s.selected = id
		}
	}
	return s
}
func (s *draftStore) close() {
	if s != nil && s.lock != nil {
		s.lock.Close()
		s.lock = nil
	}
}
func privateRead(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > max {
		return nil, fmt.Errorf("本机草稿文件权限或大小无效")
	}
	return os.ReadFile(path)
}
func atomicPrivate(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".draft-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func validBlobKey(key string) bool {
	if len(key) != 48 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}
func (s *draftStore) restore(a *assistant) (restoreErr error) {
	defer func() {
		if restoreErr != nil {
			s.err = restoreErr
		}
	}()
	if s.restored[a.id] {
		return nil
	}
	s.restored[a.id] = true
	if s.err != nil {
		return s.err
	}
	if a.id.Validate() != nil {
		return fmt.Errorf("助理标识无效")
	}
	root := filepath.Join(s.root, string(a.id))
	raw, err := privateRead(filepath.Join(root, "draft.json"), 2<<20)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var d diskDraft
	if err = json.Unmarshal(raw, &d); err != nil {
		return fmt.Errorf("草稿文件损坏，原文件已保留")
	}
	if len(d.Attachments) > maxDraftAttachments {
		return fmt.Errorf("草稿附件数量无效")
	}
	a.composer.SetValue(d.Text)
	a.pastes = nil
	for _, b := range d.Pastes {
		if b.Start < 0 || b.End < b.Start || b.End > len([]rune(d.Text)) {
			return fmt.Errorf("粘贴块范围无效")
		}
		a.pastes = append(a.pastes, pasteBlock{start: b.Start, end: b.End, label: b.Label, text: b.Text})
	}
	a.attachments = nil
	for _, f := range d.Attachments {
		if !validBlobKey(f.Key) {
			return fmt.Errorf("草稿附件标识无效")
		}
		data, err := privateRead(filepath.Join(root, f.Key+".bin"), maxAttachmentBytes)
		if err != nil {
			return fmt.Errorf("附件原文不可读取，草稿保留：%w", err)
		}
		if blobDigest(data) != f.SHA256 {
			return fmt.Errorf("附件快照校验失败，原文件已保留")
		}
		item := draftAttachment{key: f.Key, name: f.Name, file: f.File, pasted: f.Pasted, data: data, digest: f.SHA256}
		if item.file.ID == "" {
			item.err = "上传已中断，R 重试"
		}
		a.attachments = append(a.attachments, item)
	}
	a.pending = pendingSend{text: d.Pending.Text, key: d.Pending.Key, attachments: d.Pending.Attachments, clearVersion: d.Pending.ClearVersion}
	a.clearVersion = d.ClearVersion
	// Reuse the existing caret positioning logic without persisting renderer state.
	temp := model{input: a.composer}
	temp.setComposerCaret(d.Caret)
	a.composer = temp.input
	s.last[a.id] = string(raw)
	return nil
}
func (m *model) persistDrafts() error {
	s := m.drafts
	if s == nil {
		return nil
	}
	if s.err != nil {
		return s.err
	}
	if len(m.assistants) > 0 {
		id := m.assistants[m.selected].id
		if id.Validate() == nil && id != s.selected {
			if err := atomicPrivate(filepath.Join(s.root, "selected"), []byte(id)); err != nil {
				return err
			}
			s.selected = id
		}
		m.assistants[m.selected].composer = m.input
	}
	for _, a := range m.assistants {
		if a.id.Validate() != nil {
			continue
		}
		if !s.restored[a.id] {
			continue
		}
		d := diskDraft{Text: a.composer.Value(), Caret: composerCaret(a.composer), ClearVersion: a.clearVersion, Pending: diskPending{Text: a.pending.text, Key: a.pending.key, Attachments: a.pending.attachments, ClearVersion: a.pending.clearVersion}}
		for _, p := range a.pastes {
			d.Pastes = append(d.Pastes, diskPaste{p.start, p.end, p.label, p.text})
		}
		for _, f := range a.attachments {
			d.Attachments = append(d.Attachments, diskAttachment{Key: f.key, Name: f.name, SHA256: f.digest, File: f.file, Pasted: f.pasted})
		}
		raw, err := json.Marshal(d)
		if err != nil {
			return err
		}
		if s.last[a.id] == string(raw) {
			continue
		}
		root := filepath.Join(s.root, string(a.id))
		if err = os.MkdirAll(root, 0700); err != nil {
			return err
		}
		used := map[string]bool{}
		for _, f := range a.attachments {
			if !validBlobKey(f.key) {
				return fmt.Errorf("附件标识无效")
			}
			name := f.key + ".bin"
			used[name] = true
			file := filepath.Join(root, name)
			if _, err := os.Lstat(file); errors.Is(err, os.ErrNotExist) {
				var size int64
				err = filepath.WalkDir(s.root, func(_ string, e os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !e.IsDir() {
						info, err := e.Info()
						if err != nil {
							return err
						}
						size += info.Size()
					}
					return nil
				})
				if err != nil {
					return err
				}
				if size+int64(len(f.data)) > draftCacheLimit {
					return fmt.Errorf("草稿缓存已达到 256 MiB，请先发送或移除其他草稿附件")
				}
				if err = atomicPrivate(file, f.data); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		if err = atomicPrivate(filepath.Join(root, "draft.json"), raw); err != nil {
			return err
		}
		s.last[a.id] = string(raw)
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".bin") && !used[entry.Name()] {
				_ = os.Remove(filepath.Join(root, entry.Name()))
			}
		}
	}
	return nil
}

func blobDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
