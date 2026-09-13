// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/wuweidict/wudict/internal/config"
	"github.com/wuweidict/wudict/internal/intake"
)

// intakeServer is a server over an EMPTY dictionary folder: the state a user
// who has just pointed wudict at a folder is in, and the one importing exists
// for.
func intakeServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	isolatedDBDir(t)
	reg, err := NewRegistry([]string{dir}, false)
	if err != nil {
		t.Fatal(err)
	}
	return New(reg), dir
}

// oneDictZip is an archive holding a single complete DSL dictionary.
func oneDictZip(t *testing.T, name string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name + ".dsl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(sampleDSL)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func post(t *testing.T, s *Server, target string, body []byte, into any) *httptest.ResponseRecorder {
	t.Helper()
	r := newRequest("POST", target, bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if into != nil {
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatalf("%s: %v (body %q)", target, err, w.Body.String())
		}
	}
	return w
}

// The whole round trip a page makes: upload, see what is inside, install it,
// and find the dictionary in the folder afterwards.
func TestIntakeUploadAndInstall(t *testing.T) {
	s, dir := intakeServer(t)

	var sniffed intakeStatus
	if w := post(t, s, "/api/intake?name=bundle.zip", oneDictZip(t, "Oxford"), &sniffed); w.Code != 200 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	if sniffed.State != intake.StateReady || len(sniffed.Candidates) != 1 {
		t.Fatalf("sniffed = %+v", sniffed)
	}
	if got := sniffed.Candidates[0].Name; got != "Oxford" {
		t.Errorf("candidate name = %q, want Oxford", got)
	}
	if sniffed.Dir != dir {
		t.Errorf("dir = %q, want %q", sniffed.Dir, dir)
	}
	if sniffed.Keep != config.ImportKeepAsk {
		t.Errorf("keep = %q, want ask", sniffed.Keep)
	}

	var started intakeStatus
	if w := post(t, s, "/api/intake?confirm=1", []byte(`{"pick":[0],"keep":false}`), &started); w.Code != 202 {
		t.Fatalf("confirm: %d %s", w.Code, w.Body)
	}
	s.intake.Wait()

	final := s.importer().Status()
	if final.State != intake.StateDone {
		t.Fatalf("state = %q, error %q", final.State, final.Error)
	}
	if len(final.Installed) != 1 || final.Installed[0] != "Oxford" {
		t.Fatalf("installed = %v", final.Installed)
	}
	if _, err := os.Stat(filepath.Join(dir, "Oxford", "Oxford.dsl")); err != nil {
		t.Fatalf("the dictionary is not in the folder: %v", err)
	}
	// The staging area must not outlive the import, successful or not.
	if _, err := os.Stat(intake.StageRoot(dir)); !os.IsNotExist(err) {
		t.Errorf("staging root survived the import: %v", err)
	}
	// And the library has been told to look again, or the import would only
	// take effect on the next restart.
	if n := s.reg.Count(); n != 1 {
		t.Errorf("registry holds %d dictionaries after the import, want 1", n)
	}
}

// A spooled upload that turns out to hold nothing is answered, not stored: the
// copy this server made of it is its own to remove.
func TestIntakeEmptyArchiveLeavesNothing(t *testing.T) {
	s, dir := intakeServer(t)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("readme.txt")
	_, _ = w.Write([]byte("not a dictionary"))
	_ = zw.Close()

	rec := post(t, s, "/api/intake?name=empty.zip", buf.Bytes(), nil)
	if rec.Code != 422 {
		t.Fatalf("code = %d, want 422 (%s)", rec.Code, rec.Body)
	}
	if s.importer().Status().State != "" {
		t.Errorf("a failed acquisition left a job behind")
	}
	ents, err := os.ReadDir(intake.StageRoot(dir))
	if err == nil && len(ents) > 0 {
		t.Errorf("the spooled copy survived: %v", ents)
	}
}

// Confirming with nothing to confirm is a conflict, not a bad request: the
// page asked a reasonable thing of a server whose job had gone.
func TestIntakeConfirmWithoutJob(t *testing.T) {
	s, _ := intakeServer(t)
	if rec := post(t, s, "/api/intake?confirm=1", []byte(`{"pick":[0]}`), nil); rec.Code != 409 {
		t.Fatalf("code = %d, want 409 (%s)", rec.Code, rec.Body)
	}
}

// IMPORT_KEEP=delete answers the question, so the page is not asked it - and
// the request cannot contradict it.
func TestIntakeKeepPolicyOverridesRequest(t *testing.T) {
	s, _ := intakeServer(t)
	s.ImportKeep = config.ImportKeepDelete
	yes := true
	if s.keepSource(&yes) {
		t.Error("a request must not override IMPORT_KEEP=delete")
	}
	s.ImportKeep = config.ImportKeepYes
	no := false
	if !s.keepSource(&no) {
		t.Error("a request must not override IMPORT_KEEP=keep")
	}
	s.ImportKeep = config.ImportKeepAsk
	if !s.keepSource(nil) {
		t.Error("an unanswered ask must keep the source")
	}
}

// A cancel with nothing running is not an error, and clears a sniffed archive
// the user never confirmed.
func TestIntakeCancelClearsSniffedJob(t *testing.T) {
	s, dir := intakeServer(t)
	var sniffed intakeStatus
	post(t, s, "/api/intake?name=bundle.zip", oneDictZip(t, "Collins"), &sniffed)
	if sniffed.State != intake.StateReady {
		t.Fatalf("state = %q", sniffed.State)
	}
	r := newRequest("DELETE", "/api/intake", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("cancel: %d %s", w.Code, w.Body)
	}
	if s.importer().Status().State != "" {
		t.Error("the job survived a cancel")
	}
	if ents, err := os.ReadDir(intake.StageRoot(dir)); err == nil && len(ents) > 0 {
		t.Errorf("the spooled copy survived the cancel: %v", ents)
	}
}

// A loose dictionary file uploaded on its own. The name matters here in a way
// it does not for an archive: it IS the dictionary's name, so a spool that
// renamed the upload would install a dictionary called "incoming-3141592".
func TestIntakeUploadOfALooseFile(t *testing.T) {
	s, dir := intakeServer(t)

	var sniffed intakeStatus
	if w := post(t, s, "/api/intake?name=Oxford.dsl", []byte(sampleDSL), &sniffed); w.Code != 200 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	if sniffed.State != intake.StateReady || len(sniffed.Candidates) != 1 {
		t.Fatalf("sniffed = %+v", sniffed)
	}
	if got := sniffed.Candidates[0].Name; got != "Oxford" {
		t.Fatalf("candidate name = %q, want the file's own name", got)
	}
	// Nothing was downloaded, so there is nothing beside it to offer.
	if len(sniffed.Extras) != 0 {
		t.Errorf("extras = %+v, want none for an upload", sniffed.Extras)
	}

	if w := post(t, s, "/api/intake?confirm=1", []byte(`{"pick":[0],"keep":false}`), nil); w.Code != 202 {
		t.Fatalf("confirm: %d %s", w.Code, w.Body)
	}
	s.intake.Wait()
	if st := s.importer().Status(); st.State != intake.StateDone {
		t.Fatalf("state = %q, error %q", st.State, st.Error)
	}
	if _, err := os.Stat(filepath.Join(dir, "Oxford", "Oxford.dsl")); err != nil {
		t.Fatalf("the dictionary is not in the folder: %v", err)
	}
	if _, err := os.Stat(intake.StageRoot(dir)); !os.IsNotExist(err) {
		t.Errorf("staging root survived the import: %v", err)
	}
}

// A caller with no JSON body names its companions the same way it names its
// dictionaries. Out of range is refused rather than ignored: silently
// installing without the file the user asked for is the one outcome they did
// not choose.
func TestIntakeConfirmRejectsAnUnknownExtra(t *testing.T) {
	s, _ := intakeServer(t)
	if w := post(t, s, "/api/intake?name=bundle.zip", oneDictZip(t, "Oxford"), nil); w.Code != 200 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	if w := post(t, s, "/api/intake?confirm=1&pick=0&extras=0", nil, nil); w.Code != 400 {
		t.Fatalf("confirm with an unknown extra: %d %s", w.Code, w.Body)
	}
	if st := s.importer().Status(); st.State != intake.StateReady {
		t.Fatalf("state = %q, want the job still waiting for a real answer", st.State)
	}
}
