package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"unicode"
)

const allDictionariesGroup = "all"

// DictionaryGroup is collection metadata in state.json. Membership is stored
// on DictPref so the existing identity repair also repairs group membership.
type DictionaryGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type groupView struct {
	DictionaryGroup
	Members  []string `json:"members"`
	Readonly bool     `json:"readonly"`
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	s.reg.prefs.heal(s.reg)
	entries := s.reg.all()
	p := s.reg.prefs
	p.mu.RLock()
	defer p.mu.RUnlock()
	all := groupView{DictionaryGroup: DictionaryGroup{allDictionariesGroup, "All Dictionaries"}, Members: []string{}, Readonly: true}
	views := []groupView{all}
	for _, g := range p.groups {
		views = append(views, groupView{DictionaryGroup: g, Members: []string{}})
	}
	for _, e := range entries {
		views[0].Members = append(views[0].Members, e.ID)
		for _, d := range p.dicts {
			if d.ID != e.ID {
				continue
			}
			for i := 1; i < len(views); i++ {
				if slices.Contains(d.Groups, views[i].ID) {
					views[i].Members = append(views[i].Members, e.ID)
				}
			}
			break
		}
	}
	writeJSON(w, views)
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "Invalid group name", 400)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 100 || strings.ContainsFunc(name, unicode.IsControl) {
		http.Error(w, "Enter a group name (1–100 characters, without control characters).", 400)
		return
	}
	p := s.reg.prefs
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.EqualFold(name, "All Dictionaries") {
		http.Error(w, "All Dictionaries is a reserved group name.", 409)
		return
	}
	for _, g := range p.groups {
		if strings.EqualFold(g.Name, name) {
			http.Error(w, "A group with this name already exists.", 409)
			return
		}
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		http.Error(w, "Could not create group", 500)
		return
	}
	g := DictionaryGroup{hex.EncodeToString(id[:]), name}
	old := p.groups
	p.groups = append(slices.Clone(p.groups), g)
	if err := p.saveLocked(); err != nil {
		p.groups = old
		http.Error(w, "Could not save group: "+err.Error(), 500)
		return
	}
	writeJSON(w, groupView{DictionaryGroup: g, Members: []string{}})
}

// Set one membership, avoiding lost updates from clients editing other rows.
func (s *Server) handleGroupMember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Group  string `json:"group"`
		Dict   string `json:"dict"`
		Member *bool  `json:"member"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.Member == nil {
		http.Error(w, "Invalid membership", 400)
		return
	}
	if req.Group == allDictionariesGroup {
		http.Error(w, "All Dictionaries always contains every dictionary.", 400)
		return
	}
	e, err := s.reg.get(req.Dict)
	if err != nil {
		http.Error(w, "Dictionary no longer available", 404)
		return
	}
	p := s.reg.prefs
	p.editMu.Lock()
	defer p.editMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !slices.ContainsFunc(p.groups, func(g DictionaryGroup) bool { return g.ID == req.Group }) {
		http.Error(w, "Group not found", 404)
		return
	}
	old := p.dicts
	p.dicts = slices.Clone(old)
	i := slices.IndexFunc(p.dicts, func(d DictPref) bool { return d.ID == e.ID || samePath(d.Path, e.Path) })
	if i < 0 {
		p.dicts = append(p.dicts, DictPref{ID: e.ID, Path: e.Path})
		i = len(p.dicts) - 1
	}
	groups := slices.Clone(p.dicts[i].Groups)
	groups = slices.DeleteFunc(groups, func(id string) bool { return id == req.Group })
	if *req.Member {
		groups = append(groups, req.Group)
	}
	p.dicts[i].Groups = groups
	if err := p.saveLocked(); err != nil {
		p.dicts = old
		http.Error(w, "Could not save membership: "+err.Error(), 500)
		return
	}
	writeJSON(w, map[string]bool{"saved": true})
}
