package installationwindows

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsSchedulerCodePages(t *testing.T) {
	for _, tc := range []struct {
		page uint32
		raw  []byte
		want string
	}{
		{936, []byte{0xb4, 0xed, 0xce, 0xf3}, "错误"},
		{437, []byte{0x82}, "é"},
		{1252, []byte{0xe9}, "é"},
		{65001, []byte("中文路径"), "中文路径"},
	} {
		out, err := codePageText(tc.raw, tc.page)
		if err != nil || string(out) != tc.want {
			t.Fatal(tc.page, string(out), err)
		}
	}
}
func TestWindowsSchedulerUnicodeRegistration(t *testing.T) {
	root := filepath.Join(t.TempDir(), "中文 & 路径 🐕")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	sid, err := currentSID()
	if err != nil {
		t.Fatal(err)
	}
	name := "molr-desktop-encoding-" + time.Now().Format("150405.000000000")
	r := record{Name: name, Binary: filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")}
	r.Paths.Unit = filepath.Join(root, "task.xml")
	config := filepath.Join(root, "配置.json")
	definition := taskXML(r, config, sid)
	start, end := strings.Index(definition, "<Triggers>"), strings.Index(definition, "</Triggers>")+len("</Triggers>")
	definition = definition[:start] + "<Triggers/>" + definition[end:]
	if err = os.WriteFile(r.Paths.Unit, taskXMLFile(definition), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if _, err = scheduler(ctx, "/Create", "/TN", name, "/XML", r.Paths.Unit, "/F"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := scheduler(ctx, "/Delete", "/TN", name, "/F"); err != nil {
			t.Error(err)
		}
	})
	query, err := scheduler(ctx, "/Query", "/TN", name, "/XML")
	if err != nil {
		t.Fatal(err)
	}
	task, err := parseTaskXML(query)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.Actions[0].Exec[0].Arguments, config) {
		t.Fatal(task.Actions[0].Exec[0].Arguments)
	}
	if err = verifyTask(ctx, r, config); err != nil {
		t.Fatal(err)
	}
}
