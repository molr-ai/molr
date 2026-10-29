package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/molrdocs"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
)

func TestSkillsCatalogScopesAndReadOnlyContent(t *testing.T) {
	cfg := testConfig(t)
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	p, token := ownerSession(t, b)
	writeSkill := func(root, name, body string) {
		t.Helper()
		dir := filepath.Join(root, ".agents", "skills", name)
		need(t, os.MkdirAll(dir, 0700))
		need(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0600))
	}
	for _, a := range []assistant.Assistant{
		{ID: "alpha", OwnerID: p.OwnerID, Name: "Alpha", State: assistant.Active},
		{ID: "beta", OwnerID: p.OwnerID, Name: "Beta", State: assistant.Active},
		{ID: "archived", OwnerID: p.OwnerID, State: assistant.Dismissed},
		{ID: "foreign", OwnerID: "other", State: assistant.Active},
	} {
		need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "business.assistants", persistence.Key(a.OwnerID, a.ID), a)
		}))
		writeSkill(platform.AssistantWorkspace(cfg.DataDir, cfg.Sandbox.WorkRoot, a.OwnerID, a.ID), "same", "---\nname: Same skill\ndescription: |\n  Read a report.\n  Preserve its sources.\n---\n# "+string(a.ID))
	}
	writeSkill(platform.UserGlobal(cfg.DataDir, p.OwnerID), "shared", "---\nname: [invalid]\n---\nShared instructions")
	writeSkill(platform.UserGlobal(cfg.DataDir, "other"), "private", "Other account secret")
	request := func(query string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "https://runtime.example/api/skills"+query, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("X-Forwarded-Proto", "https")
		if authenticated {
			r.Header.Set("Cookie", "__Host-molr_session="+token.Reveal())
		}
		response := httptest.NewRecorder()
		b.HTTP.Handler().ServeHTTP(response, r)
		return response
	}
	response := request("", true)
	if response.Code != 200 {
		t.Fatalf("catalog: %d %s", response.Code, response.Body.String())
	}
	var catalog agent.SkillsCatalog
	need(t, json.Unmarshal(response.Body.Bytes(), &catalog))
	if len(catalog.Items) != 4 || len(catalog.Assistants) != 2 {
		t.Fatalf("wrong scope: %+v", catalog)
	}
	if catalog.Items[0].Source != "builtin" || catalog.Items[1].Name != "shared" || catalog.Items[2].Name != "Same skill" || catalog.Items[2].AssistantID != "alpha" || catalog.Items[3].AssistantID != "beta" || !strings.Contains(catalog.Items[2].Description, "Preserve its sources") {
		t.Fatalf("metadata: %+v", catalog.Items)
	}
	if strings.Contains(response.Body.String(), cfg.DataDir) || strings.Contains(response.Body.String(), "# alpha") {
		t.Fatal("catalog leaked host paths or eagerly returned body")
	}
	for _, item := range catalog.Items {
		q := url.Values{"path": {item.Path}, "assistant_id": {string(item.AssistantID)}}
		r := request("?"+q.Encode(), true)
		if r.Code != 200 {
			t.Fatalf("read: %d %s", r.Code, r.Body.String())
		}
		var content map[string]string
		need(t, json.Unmarshal(r.Body.Bytes(), &content))
		if content["Text"] == "" || content["Version"] != item.Version {
			t.Fatal("wrong content version", content)
		}
	}
	for _, q := range []url.Values{
		{"path": {"/work/.agents/skills/same/SKILL.md"}, "assistant_id": {"foreign"}},
		{"path": {"/work/.agents/skills/same/SKILL.md"}, "assistant_id": {"archived"}},
		{"path": {"/global/.agents/skills/shared/../shared/SKILL.md"}},
		{"path": {"/global/.agents/skills/shared/secret.txt"}},
		{"path": {"/global/.agents/skills/private/SKILL.md"}},
		{"path": {molrdocs.SkillPath}, "assistant_id": {"alpha"}},
	} {
		if r := request("?"+q.Encode(), true); r.Code == 200 {
			t.Fatal("unsafe read accepted", q)
		}
	}
	if request("", false).Code == 200 {
		t.Fatal("unauthenticated catalog")
	}
	ad := adapters{store: b.store, dataDir: cfg.DataDir, storageConfig: cfg.Sandbox}
	if _, err := listSkills(t.Context(), ad, core.Principal{Kind: core.ActorAssistant, OwnerID: p.OwnerID, SubjectID: "alpha"}); err == nil {
		t.Fatal("assistant principal accepted")
	}
	// Inspection must neither write nor activate the skill; external edits appear on refresh.
	root := platform.AssistantWorkspace(cfg.DataDir, cfg.Sandbox.WorkRoot, p.OwnerID, "alpha")
	writeSkill(root, "same", "Changed instructions")
	changed := request("?"+url.Values{"path": {"/work/.agents/skills/same/SKILL.md"}, "assistant_id": {"alpha"}}.Encode(), true)
	if changed.Code != 200 || !strings.Contains(changed.Body.String(), "Changed instructions") {
		t.Fatal(changed.Body.String())
	}
	need(t, os.Remove(filepath.Join(root, ".agents/skills/same/SKILL.md")))
	need(t, os.Symlink(filepath.Join(platform.UserGlobal(cfg.DataDir, "other"), ".agents/skills/private/SKILL.md"), filepath.Join(root, ".agents/skills/same/SKILL.md")))
	if request("?"+url.Values{"path": {"/work/.agents/skills/same/SKILL.md"}, "assistant_id": {"alpha"}}.Encode(), true).Code == 200 {
		t.Fatal("symlink escaped workspace")
	}
}
