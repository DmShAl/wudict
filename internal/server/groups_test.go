package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func groupCall(t *testing.T, s *Server, method, path string, body any, status int) []byte {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRecorder()
	s.ServeHTTP(r, newRequest(method, path, strings.NewReader(string(b))))
	if r.Code != status {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, r.Code, status, r.Body.String())
	}
	return r.Body.Bytes()
}

func createTestGroup(t *testing.T, s *Server, name string) groupView {
	t.Helper()
	var g groupView
	if err := json.Unmarshal(groupCall(t, s, "POST", "/api/groups", map[string]string{"name": name}, 200), &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Members) != 0 {
		t.Fatal("new group is not empty")
	}
	return g
}

func TestGroupsPersistenceAndCollection(t *testing.T) {
	s, state := newPrefsServer(t)
	e := s.reg.all()[0]
	putPrefs(t, s, `{"dicts":[{"id":"`+e.ID+`","off":true}],"ui":{"fontSize":24}}`)
	a, b := createTestGroup(t, s, "Essential"), createTestGroup(t, s, "Full")
	for _, g := range []groupView{a, b} {
		groupCall(t, s, "PUT", "/api/groups/member", map[string]any{"group": g.ID, "dict": e.ID, "member": true}, 200)
	}
	// Legacy clients saving order/enabled settings must preserve membership.
	putPrefs(t, s, `{"dicts":[{"id":"`+e.ID+`","off":true}]}`)
	s.reg.prefs = LoadPrefs(state)
	var groups []groupView
	getJSON(t, s, "/api/groups", &groups)
	if len(groups) != 3 || !groups[0].Readonly || len(groups[0].Members) != 2 || !slices.Contains(groups[1].Members, e.ID) || !slices.Contains(groups[2].Members, e.ID) {
		t.Fatalf("restart: %+v", groups)
	}
	if !s.reg.prefs.Off(e.ID, e.Path) || s.reg.prefs.UI().FontSize != 24 {
		t.Fatal("search/UI prefs changed")
	}
	groupCall(t, s, "PUT", "/api/groups/member", map[string]any{"group": a.ID, "dict": e.ID, "member": false}, 200)
	getJSON(t, s, "/api/groups", &groups)
	if len(groups[1].Members) != 0 || len(groups[2].Members) != 1 || !s.reg.has(e.ID) {
		t.Fatal("removing membership affected dictionary or other group")
	}
	// Registry changes are reflected automatically without a stored All list.
	path := filepath.Join(filepath.Dir(e.Path), "new.dsl")
	if err := os.WriteFile(path, []byte(sampleDSL), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.reg.Rescan(); err != nil {
		t.Fatal(err)
	}
	getJSON(t, s, "/api/groups", &groups)
	if len(groups[0].Members) != 3 {
		t.Fatal("new dictionary missing from All")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(e.Path); err != nil {
		t.Fatal(err)
	}
	if err := s.reg.Rescan(); err != nil {
		t.Fatal(err)
	}
	getJSON(t, s, "/api/groups", &groups)
	if len(groups[0].Members) != 1 || len(groups[2].Members) != 0 {
		t.Fatalf("removed dictionaries still visible: %+v", groups)
	}
}

func TestGroupValidationAndRollback(t *testing.T) {
	s, _ := newPrefsServer(t)
	for _, name := range []string{"", "  ", strings.Repeat("x", 101), "a\nb"} {
		groupCall(t, s, "POST", "/api/groups", map[string]string{"name": name}, 400)
	}
	createTestGroup(t, s, " Essential ")
	for _, name := range []string{"essential", "All Dictionaries", " ALL DICTIONARIES "} {
		groupCall(t, s, "POST", "/api/groups", map[string]string{"name": name}, 409)
	}
	e := s.reg.all()[0]
	groupCall(t, s, "PUT", "/api/groups/member", map[string]any{"group": "all", "dict": e.ID, "member": false}, 400)
	groupCall(t, s, "PUT", "/api/groups/member", map[string]any{"group": "missing", "dict": e.ID, "member": true}, 404)
	groupCall(t, s, "PUT", "/api/groups/member", map[string]any{"group": "all", "dict": e.ID}, 400)
	// A directory cannot be atomically replaced by a JSON file.
	s.reg.prefs.path = t.TempDir()
	groupCall(t, s, "POST", "/api/groups", map[string]string{"name": "Failed"}, 500)
	if len(s.reg.prefs.groups) != 1 {
		t.Fatal("failed save changed memory")
	}
	g := s.reg.prefs.groups[0]
	groupCall(t, s, "PUT", "/api/groups/member", map[string]any{"group": g.ID, "dict": e.ID, "member": true}, 500)
	if len(s.reg.prefs.dicts) != 0 {
		t.Fatal("failed membership save changed memory")
	}
}

func TestGroupsConcurrentPrefs(t *testing.T) {
	s, state := newPrefsServer(t)
	g := createTestGroup(t, s, "Essential")
	e := s.reg.all()[0]
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			groupCall(t, s, "PUT", "/api/groups/member", map[string]any{"group": g.ID, "dict": e.ID, "member": true}, 200)
		})
		wg.Go(func() { putPrefs(t, s, `{"dicts":[{"id":"`+e.ID+`","off":true}]}`) })
	}
	wg.Wait()
	p := LoadPrefs(state)
	if len(p.dicts) != 1 || !p.dicts[0].Off || !slices.Contains(p.dicts[0].Groups, g.ID) {
		t.Fatalf("lost concurrent save: %+v", p.dicts)
	}
}

func TestGroupsHealAndRetainUnavailable(t *testing.T) {
	s, state := newPrefsServer(t)
	e := s.reg.all()[0]
	g := createTestGroup(t, s, "Essential")
	seed := prefsFile{Version: 1, Groups: []DictionaryGroup{g.DictionaryGroup}, Dicts: []DictPref{
		{ID: "old-id", Path: filepath.Join("/old", filepath.Base(e.Path)), Off: true, Groups: []string{g.ID}},
		{ID: "offline", Path: "/offline/large.mdx", Groups: []string{g.ID}},
	}}
	data, _ := json.Marshal(seed)
	if err := os.WriteFile(state, data, 0600); err != nil {
		t.Fatal(err)
	}
	s.reg.prefs = LoadPrefs(state)
	var groups []groupView
	getJSON(t, s, "/api/groups", &groups)
	if !slices.Equal(groups[1].Members, []string{e.ID}) {
		t.Fatalf("identity was not healed: %+v", groups)
	}
	p := LoadPrefs(state)
	if len(p.dicts) != 2 || p.dicts[0].ID != e.ID || !p.dicts[0].Off || !slices.Contains(p.dicts[0].Groups, g.ID) || !slices.Contains(p.dicts[1].Groups, g.ID) {
		t.Fatalf("healing lost state: %+v", p.dicts)
	}
}
