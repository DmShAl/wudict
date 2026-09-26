// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wuweidict/wudict/internal/logx"
)

// Saved appearances: one name for everything the Appearance sheet owns.
//
// That sheet now holds a whole look - the article's size and weight, which
// built-in preset layers are on, the window's colour and wallpaper per theme,
// what the screen margins and the bars do, and the reader's own stylesheets -
// and the only way back to a look that worked was to set every one of those
// again by hand. This is the name for it.
//
// Three things a look deliberately is NOT:
//
//   - Not a fifth stylesheet. The built-in layers are stored as the IDs that
//     are ON, never as copies of their CSS: those files are embedded in the
//     binary, so a look saved today picks up tomorrow's version of "Sepia"
//     instead of freezing the one it was saved against.
//   - Not per-theme. A look carries BOTH halves - a day colour and a night
//     colour, a day sheet and a night sheet - and never touches the theme
//     itself, because switching to a look at night must not turn the lights
//     on.
//   - Not the shell's own settings. The colour, the wallpaper, the margins and
//     the bars live in the Android shell's SharedPreferences, which this
//     process can neither read nor write. The page sends them in with every
//     request and pushes them back on apply; the server stores them opaquely
//     and compares them like any other field.
//
// State lives in style/looks.json, beside presets.json and the four
// stylesheets it speaks for: one folder, backed up as one piece.

// lookHalf is one theme's half: the window's backdrop and the reader's own
// sheet for it. The shell half of a request carries the backdrop fields and
// leaves the two sheets empty, because the text is the server's to read.
type lookHalf struct {
	ColorEnabled bool   `json:"colorEnabled"`
	Color        string `json:"color,omitempty"`
	Image        string `json:"image,omitempty"`
	App          string `json:"app,omitempty"`
	Article      string `json:"article,omitempty"`
}

// lookState is the whole appearance, in one value. The two sheet fields of
// each half are what the editor holds; the backdrop fields are the shell's.
type lookState struct {
	FontSize   int      `json:"fontSize,omitempty"`
	FontWeight int      `json:"fontWeight,omitempty"`
	Layers     []string `json:"layers"`
	Light      lookHalf `json:"light"`
	Dark       lookHalf `json:"dark"`
	// The margins and the bars are the shell's too, and they are NOT per
	// theme - the shell keeps one value for both. Pointers so that a request
	// that does not carry them is not read as asking for zero: a browser with
	// no shell at all must still be able to save and apply a look.
	EdgeMode  *int   `json:"edgeMode,omitempty"`
	EdgeColor string `json:"edgeColor,omitempty"`
	Bars      *int   `json:"bars,omitempty"`
}

type look struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Builtin bool      `json:"builtin,omitempty"`
	State   lookState `json:"state"`
}

// The three that ship. They are states, not files: applying one writes the
// same fields any other look writes, and its sheets are empty on purpose -
// "Clean" is the one way back to no custom CSS at all that does not mean
// emptying four boxes by hand.
//
// The wallpaper names come from WindowBackground's seeded store, and the
// colours are the tone the page derives from each image (a 4-bit-per-channel
// histogram, averaged inside its largest bucket). They are written out rather
// than derived here because the shell is the one that stores them: if the
// number did not match what the page computes, the look would report itself
// modified the moment it was applied.
const (
	lookClean    = "clean"
	lookWarm     = "sepia"
	lookOldPaper = "oldpaper"
)

func builtinLooks() []look {
	return []look{
		{
			ID: lookClean, Name: "Clean", Builtin: true,
			// Everything off, and nothing else: the app's own defaults.
			State: lookState{Layers: []string{}},
		},
		{
			// WARM, not Sepia, and the id is the old one on purpose: an
			// install that has this look in force keeps it across the rename,
			// and the id is never shown to anyone. "Sepia" is what the shell's
			// window colour is called in its own code and what the style layer
			// is called in the pane; a third thing by that name was one too
			// many, and the reader asked for this one to move.
			//
			// No window colour: the palette in the layer is the whole look.
			// It used to enable the warm window colour as well, which meant
			// the look appeared to do nothing when that colour was switched
			// off - and the two are different things: the layer paints the
			// page, the colour paints the window behind it.
			ID: lookWarm, Name: "Warm", Builtin: true,
			State: lookState{
				Layers: []string{"sepia"},
			},
		},
		{
			ID: lookOldPaper, Name: "Old paper", Builtin: true,
			// A paper for each theme, which is what the night pair is for:
			// paper_02 is the light paper and paper_03 the dark one.
			State: lookState{
				Layers: []string{"background"},
				Light:  lookHalf{ColorEnabled: true, Color: "#EDD1A6", Image: "paper_02.jpg"},
				Dark:   lookHalf{ColorEnabled: true, Color: "#332111", Image: "paper_03.jpg"},
			},
		},
	}
}

func builtinLook(id string) (look, bool) {
	for _, l := range builtinLooks() {
		if l.ID == id {
			return l, true
		}
	}
	return look{}, false
}

// ── the state file ───────────────────────────────────────────────────────

// looksFile is what looks.json holds: the reader's own looks and which one is
// in force. The built-ins are not in here - they come from the code, so an
// app update can improve one without a migration, and a file cannot claim to
// be "Clean" and mean something else.
type looksFile struct {
	Current string `json:"current,omitempty"`
	Looks   []look `json:"looks"`
}

func (s *Server) looksPath() string {
	if s.StyleDir == "" {
		return ""
	}
	return filepath.Join(s.StyleDir, "looks.json")
}

func (s *Server) looksRead() looksFile {
	var f looksFile
	p := s.looksPath()
	if p == "" {
		return f
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return f
	}
	if err := json.Unmarshal(b, &f); err != nil {
		logx.Warn("looks unreadable, treating as none saved: %v", err)
		return looksFile{}
	}
	// An id that names nothing is dropped rather than reported: it can only
	// come from a hand-edited file or a look deleted in another window.
	// Checked against the built-ins and this file's own looks by hand, NOT
	// through lookByID: that one reads this file, and calling it here would
	// be a loop.
	if f.Current != "" {
		_, known := builtinLook(f.Current)
		for _, l := range f.Looks {
			if l.ID == f.Current {
				known = true
				break
			}
		}
		if !known {
			f.Current = ""
		}
	}
	kept := f.Looks[:0]
	for _, l := range f.Looks {
		if l.ID != "" && l.Name != "" && !l.Builtin {
			kept = append(kept, l)
		}
	}
	f.Looks = kept
	return f
}

// looksWrite is temp-file + rename like every other file this surface writes:
// a crash mid-save must not lose the reader's looks.
func (s *Server) looksWrite(f looksFile) error {
	p := s.looksPath()
	if p == "" {
		return os.ErrPermission
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".looks-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// lookByID finds a look among the built-ins and the saved ones.
func (s *Server) lookByID(id string) (look, bool) {
	if l, ok := builtinLook(id); ok {
		return l, true
	}
	for _, l := range s.looksRead().Looks {
		if l.ID == id {
			return l, true
		}
	}
	return look{}, false
}

// ── reading the state that is in force ───────────────────────────────────

// lookNow is what the PAGE knows about the screen, sent with every save and
// every apply: the shell's half, which this process cannot read, and the two
// text settings, which it can - but reads from a file the page writes on a
// 400ms delay, so a reader who taps the size stepper and immediately switches
// a look would be compared against the size they had a moment ago. The page
// has the number on screen; it sends it, and the file is the fallback for a
// client that does not.
type lookNow struct {
	Shell      *lookState `json:"shell"`
	FontSize   *int       `json:"fontSize"`
	FontWeight *int       `json:"fontWeight"`
}

// lookStateNow assembles what the app looks like right now: the reader's
// settings, the enabled layers, the four sheets from disk, and whatever the
// caller reported about the window and the text. This is what "Save Current"
// stores, and what every apply is compared against.
func (s *Server) lookStateNow(n *lookNow) lookState {
	var st lookState
	if ui := s.reg.prefs.UI(); ui != nil {
		st.FontSize, st.FontWeight = ui.FontSize, ui.FontWeight
	}
	if n != nil {
		if n.FontSize != nil {
			st.FontSize = *n.FontSize
		}
		if n.FontWeight != nil {
			st.FontWeight = *n.FontWeight
		}
	}
	st.Layers = append([]string{}, s.presetEnabled()...)
	if st.Layers == nil {
		st.Layers = []string{}
	}
	st.Light = lookHalf{
		App:     string(s.styleRead(appCSSName)),
		Article: string(s.styleRead(articleCSSName)),
	}
	st.Dark = lookHalf{
		App:     string(s.styleRead(appNightCSSName)),
		Article: string(s.styleRead(articleNightCSSName)),
	}
	if n != nil && n.Shell != nil {
		sh := n.Shell
		st.Light.ColorEnabled, st.Light.Color, st.Light.Image = sh.Light.ColorEnabled, sh.Light.Color, sh.Light.Image
		st.Dark.ColorEnabled, st.Dark.Color, st.Dark.Image = sh.Dark.ColorEnabled, sh.Dark.Color, sh.Dark.Image
		st.EdgeMode, st.EdgeColor, st.Bars = sh.EdgeMode, sh.EdgeColor, sh.Bars
	}
	return st
}

// lookIsCustomised reports whether the screen holds anything the reader made:
// a size or a weight away from the default, a layer switched on, a backdrop,
// or a stylesheet of their own. It is the question asked when NOTHING is in
// force, and it deliberately ignores the values that always carry something -
// the colour the shell would use anyway, the margin colour - because those
// are settings rather than changes.
func lookIsCustomised(st lookState) bool {
	// The DEFAULTS, not the raw encoding: the page reports the size it draws
	// with (15), and a screen showing the app's own default is not customised
	// whatever number stands for that in the record.
	if effectiveSize(st.FontSize) != fontSizeDefault ||
		effectiveWeight(st.FontWeight) != fontWeightDefault || len(st.Layers) > 0 {
		return true
	}
	for _, h := range []lookHalf{st.Light, st.Dark} {
		if h.ColorEnabled || h.Image != "" || h.App != "" || h.Article != "" {
			return true
		}
	}
	return false
}

// shellHalf is the part of a state the page has to push into the shell: the
// backdrop of each theme, and the two window settings. Returned on apply so
// the page can send them one by one over the bridge it owns.
func shellHalf(st lookState) lookState {
	return lookState{
		Light:     lookHalf{ColorEnabled: st.Light.ColorEnabled, Color: st.Light.Color, Image: st.Light.Image},
		Dark:      lookHalf{ColorEnabled: st.Dark.ColorEnabled, Color: st.Dark.Color, Image: st.Dark.Image},
		EdgeMode:  st.EdgeMode,
		EdgeColor: st.EdgeColor,
		Bars:      st.Bars,
	}
}

// lookDiffers reports whether the app has drifted from a look: what the
// reader changed and has not saved anywhere. Every field counts, the sheets
// included, because all of them are restored by an apply and none of them can
// be guessed back.
//
// A pointer field the caller did not send is NOT compared: a browser with no
// shell has no margins to report, and "unknown" must not read as "changed".
func lookDiffers(now, want lookState) bool {
	if effectiveSize(now.FontSize) != effectiveSize(want.FontSize) ||
		effectiveWeight(now.FontWeight) != effectiveWeight(want.FontWeight) {
		return true
	}
	if !slices.Equal(sortedCopy(now.Layers), sortedCopy(want.Layers)) {
		return true
	}
	if halfDiffers(now.Light, want.Light) || halfDiffers(now.Dark, want.Dark) {
		return true
	}
	if now.EdgeMode != nil && want.EdgeMode != nil && *now.EdgeMode != *want.EdgeMode {
		return true
	}
	if now.Bars != nil && want.Bars != nil && *now.Bars != *want.Bars {
		return true
	}
	if now.EdgeColor != "" && want.EdgeColor != "" && !strings.EqualFold(now.EdgeColor, want.EdgeColor) {
		return true
	}
	return false
}

// The two text settings are stored with 0 meaning "the reader never set this,
// use the default", while the page normalises that to the number it draws with
// (FS_DEF 15, FW_DEF 400) and sends it back. The comparison has to be about
// what is IN FORCE rather than about the encoding, or a look saved with the
// default in it would report itself changed the moment it was applied - and
// then every switch between two built-ins asked whether to save changes nobody
// had made. The numbers are the page's, and the page's constants are the ones
// named here.
const (
	fontSizeDefault   = 15
	fontWeightDefault = 400
)

func effectiveSize(px int) int {
	if px == 0 {
		return fontSizeDefault
	}
	return px
}

func effectiveWeight(w int) int {
	if w == 0 {
		return fontWeightDefault
	}
	return w
}

// halfDiffers compares one theme's half. Two things it deliberately does NOT
// treat as differences:
//
//   - The COLOUR's case. The page derives a tone from a wallpaper with its
//     canvas code ("#edd1a6") while a built-in look states its own in capitals,
//     and the two are the same colour.
//   - The colour's VALUE while it is not in use. The shell keeps the last value
//     in that field whether or not the checkbox beside it is on, so a look with
//     no colour of its own leaves whatever was there - and comparing it would
//     report the reader's screen as changed for a value nothing is painting.
func halfDiffers(a, b lookHalf) bool {
	if a.ColorEnabled != b.ColorEnabled || a.Image != b.Image || a.App != b.App || a.Article != b.Article {
		return true
	}
	if !a.ColorEnabled || !b.ColorEnabled {
		return false
	}
	return !strings.EqualFold(a.Color, b.Color)
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	slices.Sort(out)
	return out
}

// ── applying ─────────────────────────────────────────────────────────────

// normalizeLayers drops ids this build does not ship and enforces the radio
// rule, which is the one rule the presets pane lives by: inside a group and a
// theme, at most one preset can be on. A single switch enforces it on the way
// in (handlePresetSave); a look carries a whole list, and applying it must not
// be a way to smuggle in two presets that claim the same surface.
func normalizeLayers(ids []string) []string {
	_, index := presetRegistry()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		p, ok := index[id]
		if !ok {
			continue
		}
		kept := out[:0]
		for _, have := range out {
			if other, ok := index[have]; ok && other.competes(p) {
				continue
			}
			kept = append(kept, have)
		}
		out = append(kept, id)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// applyLook writes the whole state: the reader's settings, the layers, and
// all four sheets. The shell's half is not written here - the page does that
// over its bridge - so a failure past this point leaves the page and the
// shell disagreeing, which the page reports rather than hiding.
func (s *Server) applyLook(l look) error {
	if s.StyleDir == "" {
		return errors.New("no configuration folder to write to")
	}
	// One write for the settings, so the two that live in state.json can
	// never disagree on disk, and the dictionary order - which this request
	// says nothing about - is carried through untouched.
	s.reg.prefs.editMu.Lock()
	dicts, _ := s.reg.prefs.Snapshot()
	var ui UIPrefs
	if cur := s.reg.prefs.UI(); cur != nil {
		ui = *cur
	}
	ui.FontSize, ui.FontWeight = l.State.FontSize, l.State.FontWeight
	err := s.reg.prefs.update(dicts, &ui)
	s.reg.prefs.editMu.Unlock()
	if err != nil {
		return err
	}
	if err := s.presetStateWrite(normalizeLayers(l.State.Layers)); err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		body string
	}{
		{appCSSName, l.State.Light.App},
		{articleCSSName, l.State.Light.Article},
		{appNightCSSName, l.State.Dark.App},
		{articleNightCSSName, l.State.Dark.Article},
	} {
		if err := s.styleWrite(f.name, f.body); err != nil {
			return err
		}
	}
	return nil
}

// ── HTTP ─────────────────────────────────────────────────────────────────

// lookPayload is the list the drop-down draws: names, which one is in force,
// and nothing else. The states are deliberately absent - the menu does not
// need four stylesheets to say "Sepia".
func (s *Server) lookPayload() map[string]any {
	f := s.looksRead()
	rows := make([]map[string]any, 0, len(f.Looks)+3)
	for _, l := range builtinLooks() {
		rows = append(rows, map[string]any{"id": l.ID, "name": l.Name, "builtin": true})
	}
	for _, l := range f.Looks {
		rows = append(rows, map[string]any{"id": l.ID, "name": l.Name, "builtin": false})
	}
	return map[string]any{
		"writable": s.StyleDir != "",
		"current":  f.Current,
		"looks":    rows,
	}
}

func (s *Server) handleLooks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.lookPayload())
}

// lookNameOK is the same rule the group editor lives by: a name that is not
// empty, not absurdly long, and not carrying control characters. The server
// is the one that checks it, so every client gets the same answer.
func lookNameOK(name string) bool {
	if name == "" || len([]rune(name)) > 100 {
		return false
	}
	return !strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

func lookNameTaken(f looksFile, name string) bool {
	for _, l := range f.Looks {
		if strings.EqualFold(l.Name, name) {
			return true
		}
	}
	return false
}

func newLookID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// saveRequest is the body of both saves: a name for a new look, or the id of
// one to overwrite, plus the shell's half as the page sees it. The rest of
// the state the server reads for itself.
type saveLookRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	lookNow
}

// POST /api/looks - save what is on screen right now under a name. The new
// look becomes the one in force, because it IS what is in force.
func (s *Server) handleLookSave(w http.ResponseWriter, r *http.Request) {
	var req saveLookRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if s.StyleDir == "" {
		http.Error(w, "no configuration folder: there is nowhere to save a look", http.StatusConflict)
		return
	}
	name := strings.TrimSpace(req.Name)
	if !lookNameOK(name) {
		http.Error(w, "Enter a name (1–100 characters, without control characters).", http.StatusBadRequest)
		return
	}
	f := s.looksRead()
	if lookNameTaken(f, name) {
		http.Error(w, "A look with this name already exists.", http.StatusConflict)
		return
	}
	st := s.lookStateNow(&req.lookNow)
	id := newLookID()
	if id == "" {
		http.Error(w, "could not make a name for it", http.StatusInternalServerError)
		return
	}
	f.Looks = append(f.Looks, look{ID: id, Name: name, State: st})
	f.Current = id
	if err := s.looksWrite(f); err != nil {
		http.Error(w, "could not save the look: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.lookPayload())
}

// PUT /api/looks - "save my changes into this one": the named look is
// replaced by what is on screen now. Built-ins are refused: they are the
// app's, not the reader's, and a look called "Clean" that means something
// else is worse than no look at all.
func (s *Server) handleLookUpdate(w http.ResponseWriter, r *http.Request) {
	var req saveLookRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if s.StyleDir == "" {
		http.Error(w, "no configuration folder: there is nowhere to save a look", http.StatusConflict)
		return
	}
	if _, ok := builtinLook(req.ID); ok {
		http.Error(w, "A built-in look cannot be changed. Save yours under a new name.", http.StatusConflict)
		return
	}
	f := s.looksRead()
	i := slices.IndexFunc(f.Looks, func(l look) bool { return l.ID == req.ID })
	if i < 0 {
		http.Error(w, "no such look", http.StatusNotFound)
		return
	}
	st := s.lookStateNow(&req.lookNow)
	f.Looks[i].State = st
	f.Current = f.Looks[i].ID
	if err := s.looksWrite(f); err != nil {
		http.Error(w, "could not save the look: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.lookPayload())
}

// DELETE /api/looks - forget one. The built-ins cannot be deleted, and
// deleting the look in force leaves nothing in force, which is honest: what
// is on screen came from somewhere the reader has just thrown away.
func (s *Server) handleLookDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if _, ok := builtinLook(req.ID); ok {
		http.Error(w, "A built-in look cannot be deleted.", http.StatusConflict)
		return
	}
	f := s.looksRead()
	i := slices.IndexFunc(f.Looks, func(l look) bool { return l.ID == req.ID })
	if i < 0 {
		http.Error(w, "no such look", http.StatusNotFound)
		return
	}
	f.Looks = append(f.Looks[:i], f.Looks[i+1:]...)
	if f.Current == req.ID {
		f.Current = ""
	}
	if err := s.looksWrite(f); err != nil {
		http.Error(w, "could not delete the look: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.lookPayload())
}

// applyLookRequest carries the shell's half so the server can decide whether
// applying would throw away something unsaved, and `confirm` so the page can
// ask first. With confirm false the server only ANSWERS: nothing is written,
// which is what makes the question safe to ask on every switch.
type applyLookRequest struct {
	ID string `json:"id"`
	lookNow
	Confirm bool `json:"confirm"`
	// Dry is "answer only": report whether the screen has drifted from the
	// look in force, and whether a switch would have to ask, without writing
	// anything and without needing a target at all.
	Dry bool `json:"dry"`
}

// POST /api/looks/apply - put a look on. Two passes over one endpoint: the
// first reports whether the current state has drifted from the look in force
// (the page's cue to offer saving it first), the second applies.
//
// Re-applying the look that is ALREADY in force is never a question. That is
// the "put it back how it was" gesture after a change the reader does not
// want, and answering it with a dialog would put a form in front of the one
// action that has no doubt in it.
func (s *Server) handleLookApply(w http.ResponseWriter, r *http.Request) {
	var req applyLookRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if s.StyleDir == "" {
		http.Error(w, "no configuration folder: there is nowhere to remember this", http.StatusConflict)
		return
	}
	// A target is only needed to APPLY one; a dry run asks about the screen.
	target := look{}
	if req.ID != "" || !req.Dry {
		var ok bool
		target, ok = s.lookByID(req.ID)
		if !ok {
			http.Error(w, "no such look", http.StatusNotFound)
			return
		}
	}
	f := s.looksRead()
	now := s.lookStateNow(&req.lookNow)
	// What the reader would lose: the difference between the screen and the
	// look in force. With NOTHING in force there is no baseline to compare
	// against, so the only question is whether the screen has been changed at
	// all - which is the state of every install on the day this arrives, and
	// a fresh one applying its first look has nothing to save.
	var inForce look
	haveBase := false
	if f.Current != "" {
		inForce, haveBase = s.lookByID(f.Current)
	}
	drifted := lookIsCustomised(now)
	if haveBase {
		drifted = lookDiffers(now, inForce.State)
	}
	sameLook := haveBase && f.Current == req.ID
	cur := map[string]any{}
	if haveBase {
		cur = map[string]any{"id": inForce.ID, "name": inForce.Name, "builtin": inForce.Builtin}
	}
	// The DRY pass answers and writes nothing. It is what the panel's button
	// asks on every change: "has the screen drifted from the look in force?",
	// which is the one thing the drop-down cannot say, because the answer
	// changes with every tap on the steppers. It answers the switch question
	// too, so the caller that is about to apply needs no second round trip.
	if req.Dry {
		writeJSON(w, map[string]any{
			"needsConfirm": drifted && !sameLook,
			"drifted":      drifted,
			"current":      cur,
			"payload":      s.lookPayload(),
		})
		return
	}
	if !req.Confirm && drifted && !sameLook {
		writeJSON(w, map[string]any{
			"needsConfirm": true,
			"current":      cur,
			"payload":      s.lookPayload(),
		})
		return
	}
	if err := s.applyLook(target); err != nil {
		http.Error(w, "could not apply the look: "+err.Error(), http.StatusInternalServerError)
		return
	}
	f = s.looksRead()
	f.Current = target.ID
	if err := s.looksWrite(f); err != nil {
		http.Error(w, "could not remember the look: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// The two text settings ride along with the shell's half, for the same
	// reason: they are the parts of the look the PAGE has to put back on
	// itself. What the server wrote is on disk; what the reader is looking at
	// is a custom property and a stepper, and neither reads the file.
	writeJSON(w, map[string]any{
		"applied":    target.ID,
		"fontSize":   target.State.FontSize,
		"fontWeight": target.State.FontWeight,
		"shell":      shellHalf(target.State),
		"payload":    s.lookPayload(),
	})
}
