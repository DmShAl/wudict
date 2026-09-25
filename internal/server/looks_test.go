// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lookCall is groupCall's twin: a JSON body in, the status checked, the bytes
// back for whoever wants to read them.
func lookCall(t *testing.T, s *Server, method, path string, body any, status int) []byte {
	t.Helper()
	b, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, newRequest(method, path, strings.NewReader(string(b))))
	if rec.Code != status {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, rec.Code, status, rec.Body.String())
	}
	return rec.Body.Bytes()
}

type looksPayload struct {
	Writable bool   `json:"writable"`
	Current  string `json:"current"`
	Looks    []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Builtin bool   `json:"builtin"`
	} `json:"looks"`
}

// applyAnswer is what /api/looks/apply says back. One named type, and a fresh
// value per call: an absent field unmarshals to the zero value only if the
// variable starts at zero, and a re-used one would carry the previous answer's
// `applied` into a pass that only asked.
type applyAnswer struct {
	NeedsConfirm bool   `json:"needsConfirm"`
	Applied      string `json:"applied"`
	Current      struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Builtin bool   `json:"builtin"`
	} `json:"current"`
	Shell lookState `json:"shell"`
}

func listLooks(t *testing.T, s *Server) looksPayload {
	t.Helper()
	var out looksPayload
	if err := json.Unmarshal(lookCall(t, s, "GET", "/api/looks", nil, 200), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// A fresh install has the three that ship and nothing in force, which is what
// the drop-down draws before anyone has saved anything.
func TestLooksStartWithTheBuiltins(t *testing.T) {
	s, _ := newStyleServer(t)

	got := listLooks(t, s)
	if !got.Writable {
		t.Fatal("a server with a style dir says it cannot write looks")
	}
	if got.Current != "" {
		t.Fatalf("something is in force on a fresh install: %q", got.Current)
	}
	if len(got.Looks) != 3 {
		t.Fatalf("want the three built-ins, got %d", len(got.Looks))
	}
	for _, l := range got.Looks {
		if !l.Builtin || l.Name == "" {
			t.Fatalf("built-in looks must be named and marked: %+v", l)
		}
	}
}

// The whole point of "Save Current": the state is assembled by the server, so
// what comes back is the state, not the fields a client happened to know
// about. The shell's half rides along as the client sent it.
func TestLookSaveStoresTheWholeState(t *testing.T) {
	s, _ := newStyleServer(t)
	putPrefs(t, s, `{"dicts":[],"ui":{"fontSize":21,"fontWeight":700}}`)
	lookCall(t, s, "PUT", "/api/style", map[string]string{"app": "body{color:red}"}, 200)

	shell := map[string]any{
		"light": map[string]any{"colorEnabled": true, "color": "#EDD1A6", "image": "paper_02.jpg"},
		"dark":  map[string]any{"colorEnabled": false},
	}
	lookCall(t, s, "POST", "/api/looks", map[string]any{"name": "Mine", "shell": shell}, 200)

	got := listLooks(t, s)
	if len(got.Looks) != 4 {
		t.Fatalf("want the three built-ins plus mine, got %d", len(got.Looks))
	}
	if got.Current == "" {
		t.Fatal("saving a look must leave it in force: it IS what is on screen")
	}
	var mine look
	for _, l := range s.looksRead().Looks {
		if l.Name == "Mine" {
			mine = l
		}
	}
	if mine.State.FontSize != 21 || mine.State.FontWeight != 700 {
		t.Fatalf("the reader's settings did not ride along: %+v", mine.State)
	}
	if mine.State.Light.App != "body{color:red}" {
		t.Fatalf("the sheet did not ride along: %q", mine.State.Light.App)
	}
	if !mine.State.Light.ColorEnabled || mine.State.Light.Image != "paper_02.jpg" {
		t.Fatalf("the shell's half did not ride along: %+v", mine.State.Light)
	}
	if mine.State.Dark.ColorEnabled {
		t.Fatalf("the night half must stay as it was sent: %+v", mine.State.Dark)
	}
}

// Names are the server's to check, so every client gets the same answer: an
// empty or over-long name is refused, and a name already in use is a conflict
// rather than a second look with the same label in the menu.
func TestLookNamesAreChecked(t *testing.T) {
	s, _ := newStyleServer(t)
	lookCall(t, s, "POST", "/api/looks", map[string]any{"name": "Mine"}, 200)
	lookCall(t, s, "POST", "/api/looks", map[string]any{"name": "  "}, 400)
	lookCall(t, s, "POST", "/api/looks", map[string]any{"name": strings.Repeat("x", 101)}, 400)
	lookCall(t, s, "POST", "/api/looks", map[string]any{"name": "mine"}, 409)
	if got := listLooks(t, s); len(got.Looks) != 4 {
		t.Fatalf("a refused save changed the list: %d", len(got.Looks))
	}
}

// Applying a built-in writes every half of the state: the settings back to the
// default, the layers to the look's list, and all four sheets - which is how
// "Clean" means no custom CSS at all without emptying four boxes by hand.
func TestLookApplyWritesTheWholeState(t *testing.T) {
	s, dir := newStyleServer(t)
	putPrefs(t, s, `{"dicts":[],"ui":{"fontSize":24,"fontWeight":700,"hlOff":true}}`)
	lookCall(t, s, "PUT", "/api/style", map[string]string{"app": "body{color:red}", "article": "p{margin:0}"}, 200)

	// Nothing is in force and the screen HAS been customised, so the first
	// apply of all asks - that is the state of every install on the day this
	// feature arrives - and the pass that asks writes nothing.
	body := lookCall(t, s, "POST", "/api/looks/apply", map[string]any{"id": lookSepia}, 200)
	var asked applyAnswer
	if err := json.Unmarshal(body, &asked); err != nil {
		t.Fatal(err)
	}
	if !asked.NeedsConfirm {
		t.Fatal("a customised screen with nothing saved must be asked about")
	}
	if got := string(s.styleRead(appCSSName)); got != "body{color:red}" {
		t.Fatalf("the asking pass wrote something: %q", got)
	}
	first := lookCall(t, s, "POST", "/api/looks/apply", map[string]any{"id": lookSepia, "confirm": true}, 200)
	if err := json.Unmarshal(first, &asked); err != nil {
		t.Fatal(err)
	}
	if asked.Applied != lookSepia {
		t.Fatalf("want %q applied, got %q", lookSepia, asked.Applied)
	}
	// The font goes back to the DEFAULT, which is a zero in the record: an
	// apply that only ever raised values could never undo one.
	if ui := s.reg.prefs.UI(); ui == nil || ui.FontSize != 0 || ui.FontWeight != 0 {
		t.Fatalf("the settings did not go back to the default: %+v", ui)
	}
	if ui := s.reg.prefs.UI(); !ui.HLOff {
		t.Fatal("a setting the look says nothing about must be carried through, not cleared")
	}
	if got := s.presetEnabled(); len(got) != 1 || got[0] != "sepia" {
		t.Fatalf("the layers did not follow the look: %v", got)
	}
	if got := string(s.styleRead(appCSSName)); got != "" {
		t.Fatalf("the reader's own sheet survived an apply that carries none: %q", got)
	}
	if got := string(s.styleRead(articleCSSName)); got != "" {
		t.Fatalf("the article sheet survived too: %q", got)
	}
	// The shell's half comes back for the page to push: Sepia's light colour,
	// and nothing at night.
	if !asked.Shell.Light.ColorEnabled || asked.Shell.Light.Color != "#F4ECD8" {
		t.Fatalf("Sepia's light half is wrong: %+v", asked.Shell.Light)
	}
	if asked.Shell.Dark.ColorEnabled || asked.Shell.Dark.Image != "" {
		t.Fatalf("Sepia's night half must be empty: %+v", asked.Shell.Dark)
	}
	if _, err := os.ReadFile(filepath.Join(dir, "looks.json")); err != nil {
		t.Fatalf("the look was not remembered: %v", err)
	}
	if got := listLooks(t, s); got.Current != lookSepia {
		t.Fatalf("the applied look is not the one in force: %q", got.Current)
	}
}

// The question the feature exists for: change something after applying a look
// and the next apply asks first, because that change exists nowhere else.
func TestLookApplyAsksBeforeLosingUnsavedChanges(t *testing.T) {
	s, _ := newStyleServer(t)
	lookCall(t, s, "POST", "/api/looks/apply", map[string]any{"id": lookSepia, "confirm": true}, 200)

	// Nothing changed since, so no question. The shell's half is part of the
	// comparison, which is why the caller has to send it: the page reports
	// what the window is wearing, and Sepia left it wearing this.
	sepiaShell := map[string]any{
		"light": map[string]any{"colorEnabled": true, "color": "#F4ECD8"},
		"dark":  map[string]any{"colorEnabled": false},
	}
	body := lookCall(t, s, "POST", "/api/looks/apply",
		map[string]any{"id": lookOldPaper, "shell": sepiaShell}, 200)
	var asked applyAnswer
	if err := json.Unmarshal(body, &asked); err != nil {
		t.Fatal(err)
	}
	if asked.NeedsConfirm {
		t.Fatal("nothing was changed, so there was nothing to ask about")
	}
	if asked.Applied != lookOldPaper {
		t.Fatalf("want Old paper applied, got %q", asked.Applied)
	}
	if got := s.presetEnabled(); len(got) != 1 || got[0] != "background" {
		t.Fatalf("Old paper's layer did not follow: %v", got)
	}

	// Now the reader writes a sheet of their own. The next switch must ask,
	// and must say which look they are about to leave.
	lookCall(t, s, "PUT", "/api/style", map[string]string{"app": "body{color:blue}"}, 200)
	body = lookCall(t, s, "POST", "/api/looks/apply",
		map[string]any{"id": lookClean, "shell": sepiaShell}, 200)
	asked = applyAnswer{}
	if err := json.Unmarshal(body, &asked); err != nil {
		t.Fatal(err)
	}
	if !asked.NeedsConfirm {
		t.Fatal("a sheet nobody has saved is exactly what the question is for")
	}
	if asked.Current.ID != lookOldPaper || !asked.Current.Builtin {
		t.Fatalf("the question must name the look in force: %+v", asked.Current)
	}
	if asked.Applied != "" {
		t.Fatal("the pass that only answers must not apply anything")
	}
	// Nothing was written by the pass that asked: the sheet is still there.
	if got := string(s.styleRead(appCSSName)); got != "body{color:blue}" {
		t.Fatalf("the asking pass wrote something: %q", got)
	}

	// The same look that is already in force is never a question: that is the
	// "put it back" gesture, and it has no doubt in it.
	body = lookCall(t, s, "POST", "/api/looks/apply",
		map[string]any{"id": lookOldPaper, "shell": sepiaShell}, 200)
	asked = applyAnswer{}
	if err := json.Unmarshal(body, &asked); err != nil {
		t.Fatal(err)
	}
	if asked.NeedsConfirm || asked.Applied != lookOldPaper {
		t.Fatalf("re-applying the look in force must just do it: %+v", asked)
	}
	if got := string(s.styleRead(appCSSName)); got != "" {
		t.Fatalf("putting it back did not put the sheet back: %q", got)
	}
}

// A saved look is the reader's, so it can be overwritten with what is on
// screen now - and the built-ins cannot, because a look called "Clean" that
// means something else is worse than no look at all.
func TestLookUpdateAndDelete(t *testing.T) {
	s, _ := newStyleServer(t)
	lookCall(t, s, "POST", "/api/looks", map[string]any{"name": "Mine"}, 200)
	id := listLooks(t, s).Current

	lookCall(t, s, "PUT", "/api/prefs", map[string]any{"dicts": []any{}, "ui": map[string]any{"fontSize": 26}}, 200)
	lookCall(t, s, "PUT", "/api/looks", map[string]any{"id": id}, 200)
	for _, l := range s.looksRead().Looks {
		if l.ID == id && l.State.FontSize != 26 {
			t.Fatalf("the update did not take what is on screen: %+v", l.State)
		}
	}

	lookCall(t, s, "PUT", "/api/looks", map[string]any{"id": lookClean}, 409)
	lookCall(t, s, "DELETE", "/api/looks", map[string]any{"id": lookClean}, 409)
	lookCall(t, s, "DELETE", "/api/looks", map[string]any{"id": id}, 200)
	got := listLooks(t, s)
	if len(got.Looks) != 3 {
		t.Fatalf("the look was not forgotten: %d left", len(got.Looks))
	}
	if got.Current != "" {
		t.Fatalf("deleting the look in force must leave nothing in force: %q", got.Current)
	}
	lookCall(t, s, "DELETE", "/api/looks", map[string]any{"id": id}, 404)
}

// A look carries a whole list of layers, and applying it must not be a way to
// smuggle in two presets that claim the same surface: the radio rule is the
// server's, and it applies to a list as much as to a single switch.
func TestLookApplyEnforcesTheRadioRule(t *testing.T) {
	s, _ := newStyleServer(t)
	// Hand-written file, the way a reader editing it in an editor would leave
	// it: two presets of the same group and theme, both on.
	if err := s.presetStateWrite([]string{"true_black", "warm_dark", "compact"}); err != nil {
		t.Fatal(err)
	}
	lookCall(t, s, "POST", "/api/looks", map[string]any{"name": "Both"}, 200)
	id := listLooks(t, s).Current

	// The look as saved holds both, which is what the file said; applying it
	// keeps one of the group and both of the others.
	lookCall(t, s, "POST", "/api/looks/apply", map[string]any{"id": id, "confirm": true}, 200)
	got := s.presetEnabled()
	if len(got) != 2 {
		t.Fatalf("want one of the two dark papers plus Compact, got %v", got)
	}
	if !slicesContains(got, "compact") {
		t.Fatalf("a preset in a group of its own must survive: %v", got)
	}
	// Sepia is the same GROUP but the other THEME, so it is not a competitor:
	// each applies in its own theme, by its own CSS.
	if err := s.presetStateWrite([]string{"sepia", "true_black"}); err != nil {
		t.Fatal(err)
	}
	lookCall(t, s, "POST", "/api/looks", map[string]any{"name": "Themes"}, 200)
	themed := listLooks(t, s).Current
	lookCall(t, s, "POST", "/api/looks/apply", map[string]any{"id": themed, "confirm": true}, 200)
	if got := s.presetEnabled(); len(got) != 2 {
		t.Fatalf("a light preset and a dark one do not compete: %v", got)
	}
	// And an id this build does not ship is dropped rather than written back.
	if err := s.presetStateWrite([]string{"compact", "no_such_preset"}); err != nil {
		t.Fatal(err)
	}
	lookCall(t, s, "POST", "/api/looks", map[string]any{"name": "Stale"}, 200)
	stale := listLooks(t, s).Current
	lookCall(t, s, "POST", "/api/looks/apply", map[string]any{"id": stale, "confirm": true}, 200)
	if got := s.presetEnabled(); len(got) != 1 || got[0] != "compact" {
		t.Fatalf("an unknown id was written back: %v", got)
	}
}

func slicesContains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

// No config directory means nowhere to remember anything: the list says so
// rather than offering switches that cannot stick, and every write is refused.
func TestLooksWithoutAStyleDir(t *testing.T) {
	s := newTestServer(t) // StyleDir left empty

	got := listLooks(t, s)
	if got.Writable {
		t.Fatal("a server with no style dir says it can write looks")
	}
	if len(got.Looks) != 3 {
		t.Fatalf("the built-ins are the app's, so they are listed anyway: %d", len(got.Looks))
	}
	lookCall(t, s, "POST", "/api/looks", map[string]any{"name": "Mine"}, 409)
	lookCall(t, s, "POST", "/api/looks/apply", map[string]any{"id": lookSepia, "confirm": true}, 409)
}

// The caller's own numbers decide the COMPARISON, and only that: what a look
// writes is the look's state, never what the caller happened to have. They
// matter because the file those numbers live in is written on a 400ms delay -
// a reader who taps the size stepper and switches a look in the same breath
// was compared against the size they had a moment before, which is how this
// was found: the question about unsaved changes simply did not appear.
func TestLooksTrustTheCallersText(t *testing.T) {
	s, _ := newStyleServer(t)
	// Saved with the caller's numbers, so the look holds 18 while state.json
	// still knows nothing about it.
	lookCall(t, s, "POST", "/api/looks",
		map[string]any{"name": "Mine", "fontSize": 18, "fontWeight": 500}, 200)
	if got := listLooks(t, s); got.Current == "" {
		t.Fatal("saving a look must leave it in force")
	}

	// One tap more than the look holds is unsaved work, and the switch asks.
	body := lookCall(t, s, "POST", "/api/looks/apply",
		map[string]any{"id": lookClean, "fontSize": 19, "fontWeight": 500}, 200)
	var asked applyAnswer
	if err := json.Unmarshal(body, &asked); err != nil {
		t.Fatal(err)
	}
	if !asked.NeedsConfirm {
		t.Fatal("a size the reader just tapped is unsaved work")
	}
	// The SAME numbers are not a change - and they can only come from the
	// caller, because the file says zero.
	body = lookCall(t, s, "POST", "/api/looks/apply",
		map[string]any{"id": lookClean, "fontSize": 18, "fontWeight": 500}, 200)
	// Fresh value: the answer to an APPLY does not carry needsConfirm at all,
	// so a re-used struct would keep the previous question's `true`.
	asked = applyAnswer{}
	if err := json.Unmarshal(body, &asked); err != nil {
		t.Fatal(err)
	}
	if asked.NeedsConfirm {
		t.Fatalf("the size the look holds is not a change: %+v", asked)
	}
	// And what landed is Clean's own state, not the caller's numbers.
	if ui := s.reg.prefs.UI(); ui == nil || ui.FontSize != 0 || ui.FontWeight != 0 {
		t.Fatalf("an apply writes the LOOK's state, not the caller's: %+v", ui)
	}
}

// The reader's report, exactly: switching between Clean, Sepia and Old paper
// asked whether to save changes EVERY time. Two encodings were being compared
// as if they were two states - the stored "0 is the default" against the 15
// the page draws with, and the colour the shell keeps in its field against a
// look that names none - so each built-in reported itself changed the moment
// it had been applied.
//
// The shells below are the state BEFORE each switch, which is what the check
// sees: the page reports what the window is wearing at that moment, not what
// it is about to wear.
func TestLookSwitchBetweenBuiltinsNeverAsks(t *testing.T) {
	s, _ := newStyleServer(t)
	// A fresh screen with no look in force: nothing in use, and the colour
	// fields holding the shell's own defaults.
	fresh := map[string]any{
		"light": map[string]any{"colorEnabled": false, "color": "#EDD1A6", "image": ""},
		"dark":  map[string]any{"colorEnabled": false, "color": "#332111", "image": ""},
	}
	// After Clean: the same, because a look with no colour of its own leaves
	// the field alone.
	afterClean := fresh
	// After Sepia: its own light colour, and nothing at night.
	afterSepia := map[string]any{
		"light": map[string]any{"colorEnabled": true, "color": "#F4ECD8", "image": ""},
		"dark":  map[string]any{"colorEnabled": false, "color": "#332111", "image": ""},
	}
	// After Old paper: a paper for each theme.
	afterOldPaper := map[string]any{
		"light": map[string]any{"colorEnabled": true, "color": "#EDD1A6", "image": "paper_02.jpg"},
		"dark":  map[string]any{"colorEnabled": true, "color": "#332111", "image": "paper_03.jpg"},
	}

	steps := []struct {
		shell map[string]any
		to    string
	}{
		{fresh, lookClean},
		{afterClean, lookSepia},
		{afterSepia, lookOldPaper},
		{afterOldPaper, lookClean},
		{afterClean, lookSepia},
		{afterSepia, lookOldPaper},
	}
	for i, step := range steps {
		body := lookCall(t, s, "POST", "/api/looks/apply", map[string]any{
			"id": step.to, "fontSize": 15, "fontWeight": 400, "shell": step.shell,
			"confirm": false,
		}, 200)
		var a applyAnswer
		if err := json.Unmarshal(body, &a); err != nil {
			t.Fatal(err)
		}
		if a.NeedsConfirm {
			t.Fatalf("step %d: switching to %s asked about changes nobody made: %+v", i+1, step.to, a)
		}
		if a.Applied != step.to {
			t.Fatalf("step %d: %s was not applied", i+1, step.to)
		}
	}
}
