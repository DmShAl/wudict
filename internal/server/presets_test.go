// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"encoding/json"
	"path"
	"testing"
)

// The manifest says which theme a preset is for by WHICH SLOTS IT FILLS, and
// the pane, the radio rule and the page all read that. A manifest that also
// declares a theme is a second answer to the same question, and the two have
// disagreed before: high_contrast was declared for both themes and guarded to
// the light one, so the pane offered it at night and it did nothing there.
func TestPresetManifestDeclaresNoTheme(t *testing.T) {
	var man struct {
		Groups []struct {
			Presets []map[string]any `json:"presets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(presetManifest, &man); err != nil {
		t.Fatal(err)
	}
	if len(man.Groups) == 0 {
		t.Fatal("no groups parsed out of the manifest")
	}
	for _, g := range man.Groups {
		for _, p := range g.Presets {
			if _, ok := p["theme"]; ok {
				t.Errorf("preset %v declares a theme; the slots decide it now", p["id"])
			}
		}
	}
}

// Every slot the manifest names must be a file that is there, and every preset
// must have a half for at least one theme. A name with no file is skipped with
// a warning at load, which leaves a switch that turns on nothing - the one
// failure this pipeline cannot report on its own.
func TestPresetSlotsAreReadable(t *testing.T) {
	groups, _ := presetRegistry()
	if len(groups) == 0 {
		t.Fatal("no presets at all")
	}
	for _, g := range groups {
		for _, p := range g.Presets {
			if p.Theme() == "" {
				t.Errorf("preset %s has no half for either theme", p.ID)
			}
			for _, name := range []string{p.App, p.Article, p.AppNight, p.ArticleNight} {
				if name == "" {
					continue
				}
				if _, err := presetFS.ReadFile(path.Join("web", "presets", p.Dir, name)); err != nil {
					t.Errorf("preset %s names %s, which is not there: %v", p.ID, name, err)
				}
			}
		}
	}
}

// The radio rule is about the surface two presets claim, and the theme is what
// decides it: Sepia and True black are both on the same group's paper, but one
// is the day's and the other the night's, so they never compete.
func TestPresetCompetesByTheme(t *testing.T) {
	_, index := presetRegistry()
	sepia, trueBlack := index["sepia"], index["true_black"]
	if sepia == nil || trueBlack == nil {
		t.Fatal("sepia and true_black are both expected to ship")
	}
	if sepia.competes(trueBlack) {
		t.Fatal("a day preset and a night preset must not compete")
	}
	background := index["background"]
	if background == nil {
		t.Fatal("the background preset is expected to ship")
	}
	if !background.competes(sepia) || !background.competes(trueBlack) {
		t.Fatal("a preset that covers both themes competes with either")
	}
	if sepia.Theme() != "light" || trueBlack.Theme() != "dark" || background.Theme() != "both" {
		t.Fatalf("themes must be derived from the slots: sepia=%q true_black=%q background=%q",
			sepia.Theme(), trueBlack.Theme(), background.Theme())
	}
}
