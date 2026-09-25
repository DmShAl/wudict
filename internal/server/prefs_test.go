package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newPrefsServer builds a server over two dictionaries with a real state file,
// returning the server and the file's path.
func newPrefsServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"alpha.dsl", "beta.dsl"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(sampleDSL), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	isolatedDBDir(t)
	state := filepath.Join(t.TempDir(), StateFile)
	reg, err := NewRegistry([]string{dir}, false, WithPrefs(LoadPrefs(state)))
	if err != nil {
		t.Fatal(err)
	}
	return New(reg), state
}

type prefsResp struct {
	Exists bool       `json:"exists"`
	Dicts  []DictPref `json:"dicts"`
	UI     *UIPrefs   `json:"ui"`
}

func putPrefs(t *testing.T, s *Server, body string) prefsResp {
	t.Helper()
	req := newRequest("PUT", "/api/prefs", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("PUT /api/prefs: status %d: %s", rec.Code, rec.Body.String())
	}
	var out prefsResp
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("PUT /api/prefs: bad JSON (%v): %s", err, rec.Body.String())
	}
	return out
}

func TestPrefsRoundTrip(t *testing.T) {
	s, state := newPrefsServer(t)
	ids := s.reg.all()
	if len(ids) != 2 {
		t.Fatalf("want 2 dictionaries, got %d", len(ids))
	}
	a, b := ids[0].ID, ids[1].ID

	var first prefsResp
	getJSON(t, s, "/api/prefs", &first)
	if first.Exists {
		t.Error("exists=true before anything was saved: the client would skip adopting localStorage")
	}
	if len(first.Dicts) != 0 {
		t.Errorf("want no records on a fresh install, got %+v", first.Dicts)
	}

	// b first, a disabled
	got := putPrefs(t, s, `{"dicts":[{"id":"`+b+`","name":"Beta","off":false},{"id":"`+a+`","name":"Alpha","off":true}]}`)
	if len(got.Dicts) != 2 || got.Dicts[0].ID != b || got.Dicts[1].ID != a {
		t.Fatalf("order not preserved: %+v", got.Dicts)
	}
	if !got.Dicts[1].Off {
		t.Error("disabled flag lost")
	}
	if got.Dicts[0].Path != ids[1].Path {
		t.Errorf("path=%q, want the registry's %q", got.Dicts[0].Path, ids[1].Path)
	}

	// the file is what a restart reads
	data, err := os.ReadFile(state)
	if err != nil {
		t.Fatalf("state file not written: %v", err)
	}
	var f prefsFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}
	if f.Version != prefsVersion {
		t.Errorf("version=%d, want %d", f.Version, prefsVersion)
	}
	if len(f.Dicts) != 2 || f.Dicts[0].ID != b {
		t.Errorf("file disagrees with the response: %+v", f.Dicts)
	}

	reloaded := LoadPrefs(state)
	if !reloaded.Off(a, ids[0].Path) {
		t.Error("a disabled dictionary came back enabled after a restart")
	}
	if reloaded.Off(b, ids[1].Path) {
		t.Error("an enabled dictionary came back disabled after a restart")
	}
	if _, exists := reloaded.Snapshot(); !exists {
		t.Error("exists=false after a restart: the client would re-adopt stale localStorage")
	}
}

func TestPrefsKeepsUnseenDictionaries(t *testing.T) {
	s, state := newPrefsServer(t)
	a := s.reg.all()[0].ID

	// a dictionary on a drive that is not mounted right now
	seed := prefsFile{Version: prefsVersion, Dicts: []DictPref{
		{ID: "deadbeef0000", Path: "/Volumes/Stick/Big.mdx", Name: "Big", Off: true},
	}}
	data, _ := json.Marshal(seed)
	if err := os.WriteFile(state, data, 0o644); err != nil {
		t.Fatal(err)
	}
	s.reg.prefs = LoadPrefs(state)

	got := putPrefs(t, s, `{"dicts":[{"id":"`+a+`","name":"Alpha"}]}`)
	if len(got.Dicts) != 2 {
		t.Fatalf("the unmounted dictionary was forgotten: %+v", got.Dicts)
	}
	last := got.Dicts[len(got.Dicts)-1]
	if last.Path != "/Volumes/Stick/Big.mdx" || !last.Off {
		t.Errorf("retained record was altered: %+v", last)
	}
}

func TestPrefsHealsMovedDictionaries(t *testing.T) {
	s, state := newPrefsServer(t)
	entries := s.reg.all()
	live := entries[0]

	// The same file, remembered under the path it had before the folder was
	// renamed: its id no longer matches anything.
	seed := prefsFile{Version: prefsVersion, Dicts: []DictPref{
		{ID: "0000stale000", Path: filepath.Join("/elsewhere", filepath.Base(live.Path)), Name: "Alpha", Off: true},
	}}
	data, _ := json.Marshal(seed)
	if err := os.WriteFile(state, data, 0o644); err != nil {
		t.Fatal(err)
	}
	s.reg.prefs = LoadPrefs(state)

	var got prefsResp
	getJSON(t, s, "/api/prefs", &got)
	if len(got.Dicts) != 1 {
		t.Fatalf("want 1 record, got %+v", got.Dicts)
	}
	if got.Dicts[0].ID != live.ID || got.Dicts[0].Path != live.Path {
		t.Fatalf("record not re-attached to the moved dictionary: %+v", got.Dicts[0])
	}
	if !got.Dicts[0].Off {
		t.Error("the setting was lost while re-attaching it")
	}
	// the repair is durable, so the next save cannot resurrect the stale id
	if !LoadPrefs(state).Off(live.ID, live.Path) {
		t.Error("healed state was not written back to disk")
	}
}

func TestPrefsAmbiguousNamesAreNotGuessed(t *testing.T) {
	s, _ := newPrefsServer(t)
	// Two library entries would both be called "text.db": a file-name match is
	// only allowed when it is unique on both sides.
	dir := t.TempDir()
	for _, n := range []string{"one", "two"} {
		sub := filepath.Join(dir, n)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.reg.mu.Lock()
	s.reg.entries = []*entry{
		{ID: "aaaaaaaaaaaa", Path: filepath.Join(dir, "one", "text.db")},
		{ID: "bbbbbbbbbbbb", Path: filepath.Join(dir, "two", "text.db")},
	}
	s.reg.byID = map[string]*entry{}
	for _, e := range s.reg.entries {
		s.reg.byID[e.ID] = e
	}
	s.reg.mu.Unlock()

	p := LoadPrefs("")
	if err := p.Replace([]DictPref{{ID: "ccccccccccccc", Path: "/gone/three/text.db", Off: true}}); err != nil {
		t.Fatal(err)
	}
	got := p.heal(s.reg)
	if got[0].ID != "ccccccccccccc" {
		t.Errorf("an ambiguous file name was matched anyway: %+v", got[0])
	}
}

func TestPrefsMalformedFileIsIgnored(t *testing.T) {
	state := filepath.Join(t.TempDir(), StateFile)
	if err := os.WriteFile(state, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := LoadPrefs(state)
	if _, exists := p.Snapshot(); exists {
		t.Error("a malformed file must not be reported as usable state")
	}
	if p.Off("whatever", "/some/path") {
		t.Error("a malformed file must not disable anything")
	}
}

func TestPrefsInMemoryWithoutAPath(t *testing.T) {
	p := LoadPrefs("")
	if err := p.Replace([]DictPref{{ID: "x", Path: "/a/b.mdx", Off: true}}); err != nil {
		t.Fatalf("saving without a file must succeed silently: %v", err)
	}
	if !p.Off("x", "/a/b.mdx") {
		t.Error("in-memory state did not stick")
	}
}

// The two settings share one file and one write, but not one request: the
// client that reorders dictionaries and the client that resizes text are the
// same page sending different bodies, and neither may erase the other.
func TestPrefsUI(t *testing.T) {
	s, state := newPrefsServer(t)
	all := s.reg.all()
	if len(all) != 2 {
		t.Fatalf("want 2 dictionaries, got %d", len(all))
	}
	a, b := all[0].ID, all[1].ID
	order := `{"dicts":[{"id":"` + b + `"},{"id":"` + a + `"}]}`

	for _, tc := range []struct {
		name string
		body string
		want int // 0 = no ui record at all
	}{
		{"set", `{"ui":{"fontSize":24}}`, 24},
		{"dicts alone leave it", order, 24},
		{"over the ceiling clamps", `{"ui":{"fontSize":900}}`, FontSizeMax},
		{"under the floor clamps", `{"ui":{"fontSize":1}}`, FontSizeMin},
		{"zero is unset", `{"ui":{}}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := putPrefs(t, s, tc.body); got.UI.size() != tc.want {
				t.Fatalf("PUT echoed %d, want %d", got.UI.size(), tc.want)
			}
			var back prefsResp
			getJSON(t, s, "/api/prefs", &back)
			if back.UI.size() != tc.want {
				t.Fatalf("GET returned %d, want %d", back.UI.size(), tc.want)
			}
			// and it is on disk, not merely in memory
			if got := LoadPrefs(state).UI().size(); got != tc.want {
				t.Fatalf("reloaded %d, want %d", got, tc.want)
			}
		})
	}
	// the reorder above must have survived every ui-only write since
	var back prefsResp
	getJSON(t, s, "/api/prefs", &back)
	if len(back.Dicts) != 2 || back.Dicts[0].ID != b {
		t.Fatalf("ui writes disturbed the dictionary order: %+v", back.Dicts)
	}
}

// Every UI boolean has the standing default as its ZERO value - two of them by
// negating the feature (hlOff, fastFirst), the third by naming the non-default
// list order (sortMine) - so what is checked here is that "absent means the
// default" survives a round trip through the file for each of them, and that
// setting one never silently clears another or the font size beside them.
func TestPrefsUIFlags(t *testing.T) {
	s, state := newPrefsServer(t)

	for _, tc := range []struct {
		name            string
		body            string
		hlOff, wantFast bool
		wantSort        bool
	}{
		{"absent is the default", `{"ui":{"fontSize":24}}`, false, false, false},
		{"fastest on", `{"ui":{"fontSize":24,"fastFirst":true}}`, false, true, false},
		{"and highlighting off beside it", `{"ui":{"fontSize":24,"hlOff":true,"fastFirst":true}}`, true, true, false},
		{"back to my order, highlighting still off", `{"ui":{"fontSize":24,"hlOff":true}}`, true, false, false},
		{"my own list order, nothing else moved", `{"ui":{"fontSize":24,"sortMine":true}}`, false, false, true},
		{"all three at once", `{"ui":{"hlOff":true,"fastFirst":true,"sortMine":true}}`, true, true, true},
		{"an empty ui record clears them all", `{"ui":{}}`, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := putPrefs(t, s, tc.body)
			if hl, ff := got.UI.flags(); hl != tc.hlOff || ff != tc.wantFast || got.UI.sorted() != tc.wantSort {
				t.Fatalf("PUT echoed hlOff=%v fastFirst=%v sortMine=%v, want %v/%v/%v",
					hl, ff, got.UI.sorted(), tc.hlOff, tc.wantFast, tc.wantSort)
			}
			var back prefsResp
			getJSON(t, s, "/api/prefs", &back)
			if hl, ff := back.UI.flags(); hl != tc.hlOff || ff != tc.wantFast || back.UI.sorted() != tc.wantSort {
				t.Fatalf("GET returned hlOff=%v fastFirst=%v sortMine=%v, want %v/%v/%v",
					hl, ff, back.UI.sorted(), tc.hlOff, tc.wantFast, tc.wantSort)
			}
			// and it is on disk, not merely in memory
			ui := LoadPrefs(state).UI()
			if hl, ff := ui.flags(); hl != tc.hlOff || ff != tc.wantFast || ui.sorted() != tc.wantSort {
				t.Fatalf("reloaded hlOff=%v fastFirst=%v sortMine=%v, want %v/%v/%v",
					hl, ff, ui.sorted(), tc.hlOff, tc.wantFast, tc.wantSort)
			}
		})
	}

	// A dictionary-only write must not reach into the ui record: the page that
	// reorders the list and the page that picks a strategy send different
	// bodies, and neither may erase the other.
	putPrefs(t, s, `{"ui":{"fastFirst":true}}`)
	putPrefs(t, s, `{"dicts":[{"id":"`+s.reg.all()[0].ID+`"}]}`)
	if _, ff := LoadPrefs(state).UI().flags(); !ff {
		t.Fatal("a dicts-only write cleared fastFirst")
	}
}

// speakOff (D148) joins the same record under the same rule: absent is "on",
// and it neither clears nor is cleared by the flags beside it.
func TestPrefsSpeakOff(t *testing.T) {
	s, state := newPrefsServer(t)
	for _, tc := range []struct {
		name, body string
		want, hl   bool
	}{
		{"absent is on", `{"ui":{"fontSize":24}}`, false, false},
		{"off", `{"ui":{"speakOff":true}}`, true, false},
		{"off beside highlighting off", `{"ui":{"speakOff":true,"hlOff":true}}`, true, true},
		{"back on, highlighting still off", `{"ui":{"hlOff":true}}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			putPrefs(t, s, tc.body)
			ui := LoadPrefs(state).UI()
			if hl, _ := ui.flags(); ui.spoken() != tc.want || hl != tc.hl {
				t.Fatalf("reloaded speakOff=%v hlOff=%v, want %v/%v", ui.spoken(), hl, tc.want, tc.hl)
			}
		})
	}
}

func (u *UIPrefs) size() int {
	if u == nil {
		return 0
	}
	return u.FontSize
}

func (u *UIPrefs) flags() (hlOff, fastFirst bool) {
	if u == nil {
		return false, false
	}
	return u.HLOff, u.FastFirst
}

func (u *UIPrefs) sorted() bool {
	if u == nil {
		return false
	}
	return u.SortMine
}

func (u *UIPrefs) spoken() bool {
	if u == nil {
		return false
	}
	return u.SpeakOff
}

func TestPrefsFileNameRungNeedsBothSidesUnique(t *testing.T) {
	const live0, live1 = "live00000000", "live11111111"
	lib := func(name string) string { return filepath.Join("/lib", name, "text.db") }

	tests := []struct {
		name   string
		paths  []string   // dictionaries the registry has right now
		stored []DictPref // what state.json remembers
		want   []string   // id each record must hold afterwards
	}{{
		name:   "unique on both sides re-attaches",
		paths:  []string{lib("one")},
		stored: []DictPref{{ID: "stale0000000", Path: lib("gone"), Off: true}},
		want:   []string{live0},
	}, {
		name:   "ambiguous in the registry is not guessed",
		paths:  []string{lib("one"), lib("two")},
		stored: []DictPref{{ID: "stale0000000", Path: lib("gone"), Off: true}},
		want:   []string{"stale0000000"},
	}, {
		name:   "ambiguous in the stored records is not guessed",
		paths:  []string{lib("one")},
		stored: []DictPref{{ID: "stale0000000", Path: lib("gone")}, {ID: "stale1111111", Path: lib("alsogone")}},
		want:   []string{"stale0000000", "stale1111111"},
	}, {
		name:  "an exact path still wins over the collision",
		paths: []string{lib("one")},
		stored: []DictPref{
			{ID: "stale0000000", Path: lib("gone")},
			{ID: "stale1111111", Path: lib("one"), Off: true},
		},
		want: []string{"stale0000000", live0},
	}, {
		name:   "distinct file names are unaffected",
		paths:  []string{"/dicts/alpha.dsl", "/dicts/beta.dsl"},
		stored: []DictPref{{ID: "stale0000000", Path: "/elsewhere/beta.dsl", Off: true}},
		want:   []string{live1},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ids := []string{live0, live1}
			r := &Registry{}
			for i, p := range tc.paths {
				r.entries = append(r.entries, &entry{ID: ids[i], Path: p})
			}
			p := LoadPrefs("") // in-memory: this rung reads nothing from disk
			if err := p.Replace(tc.stored); err != nil {
				t.Fatal(err)
			}
			got := p.heal(r)
			if len(got) != len(tc.want) {
				t.Fatalf("records lost or invented: %+v", got)
			}
			for i, want := range tc.want {
				if got[i].ID != want {
					t.Errorf("record %d: id %q, want %q (%+v)", i, got[i].ID, want, got[i])
				}
			}
			// Re-attaching must carry the setting across, never reset it.
			for i, d := range got {
				if d.Off != tc.stored[i].Off {
					t.Errorf("record %d: off %v, want %v", i, d.Off, tc.stored[i].Off)
				}
			}
		})
	}
}
