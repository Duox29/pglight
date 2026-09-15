package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Completion aliases (ssf => SELECT * FROM) are a backend-owned domain:
// the editor fetches them once via GET /api/aliases and reads them from
// memory per keystroke (same zero-network shape as the schema snapshot).
// Defaults are hardcoded for now; user entries overlay them in-process and
// will move to persistent storage later.

// Alias is one trigger => expansion pair served to the editor.
type Alias struct {
	Trigger   string `json:"trigger"`
	Expansion string `json:"expansion"`
	Detail    string `json:"detail,omitempty"`
	Builtin   bool   `json:"builtin"`
}

// defaultAliases ships the built-in triggers (hardcoded until persisted).
var defaultAliases = []Alias{
	{Trigger: "ssf", Expansion: "SELECT * FROM ", Detail: "SELECT * FROM …", Builtin: true},
	{Trigger: "sel", Expansion: "SELECT ${cols} FROM ${table} LIMIT 100;", Detail: "SELECT … FROM … LIMIT", Builtin: true},
	{Trigger: "join", Expansion: "JOIN ${table} ON ${a}.${fk} = ${b}.id", Detail: "JOIN … ON fk", Builtin: true},
	{Trigger: "cte", Expansion: "WITH ${name} AS (\n  SELECT ${cols} FROM ${table}\n)\nSELECT * FROM ${name};", Detail: "WITH … SELECT", Builtin: true},
	{Trigger: "where", Expansion: "WHERE ${col} = ${val}", Detail: "WHERE clause", Builtin: true},
	{Trigger: "ins", Expansion: "INSERT INTO ${table} (${cols}) VALUES (${vals});", Detail: "INSERT … VALUES", Builtin: true},
	{Trigger: "upd", Expansion: "UPDATE ${table} SET ${col} = ${val} WHERE ${cond};", Detail: "UPDATE … WHERE", Builtin: true},
	{Trigger: "del", Expansion: "DELETE FROM ${table} WHERE ${cond};", Detail: "DELETE … WHERE", Builtin: true},
}

var triggerRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

const (
	maxAliases   = 200
	maxExpansion = 2000
)

// aliasStore is the process-wide user overlay: trigger (lowercased) to entry.
// A user entry with the same trigger shadows the builtin; deleting it
// restores the default.
type aliasStore struct {
	mu     sync.RWMutex
	custom map[string]Alias
}

var globalAliases = &aliasStore{custom: map[string]Alias{}}

// List returns builtins (shadowed by user entries) plus extra user entries
// sorted by trigger for a stable Settings UI.
func (s *aliasStore) List() []Alias {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Alias, 0, len(defaultAliases)+len(s.custom))
	seen := make(map[string]bool, len(defaultAliases))
	for _, d := range defaultAliases {
		if c, ok := s.custom[d.Trigger]; ok {
			c.Builtin = false
			out = append(out, c)
		} else {
			out = append(out, d)
		}
		seen[d.Trigger] = true
	}
	extra := make([]Alias, 0, len(s.custom))
	for k, c := range s.custom {
		if !seen[k] {
			c.Builtin = false
			extra = append(extra, c)
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].Trigger < extra[j].Trigger })
	return append(out, extra...)
}

// Put upserts a user entry. False when the store is full and the trigger is new.
func (s *aliasStore) Put(trigger, expansion string) (Alias, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.custom[trigger]; !ok && len(s.custom) >= maxAliases {
		return Alias{}, false
	}
	a := Alias{Trigger: trigger, Expansion: expansion}
	s.custom[trigger] = a
	return a, true
}

// Del drops one user entry; false when there was none.
func (s *aliasStore) Del(trigger string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.custom[trigger]; !ok {
		return false
	}
	delete(s.custom, trigger)
	return true
}

// Reset drops all user entries, restoring defaults.
func (s *aliasStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.custom = map[string]Alias{}
}

// Aliases serves the alias domain: GET lists merged entries (builtin +
// user), POST upserts a user entry, DELETE drops one (?trigger=) or clears
// all user entries (no param, restoring defaults). Global like
// /api/settings — no session, no PG.
func (h *Handler) Aliases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, 200, map[string]any{"aliases": globalAliases.List()})
	case "POST":
		var req struct {
			Trigger   string `json:"trigger"`
			Expansion string `json:"expansion"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		trigger := strings.ToLower(strings.TrimSpace(req.Trigger))
		if !triggerRe.MatchString(trigger) {
			writeJSON(w, 400, map[string]string{"error": "trigger must be 1-32 chars: a letter followed by letters/digits/_"})
			return
		}
		if strings.TrimSpace(req.Expansion) == "" || len(req.Expansion) > maxExpansion {
			writeJSON(w, 400, map[string]string{"error": "expansion must be 1-2000 chars"})
			return
		}
		a, ok := globalAliases.Put(trigger, req.Expansion)
		if !ok {
			writeJSON(w, 400, map[string]string{"error": "alias limit reached"})
			return
		}
		writeJSON(w, 200, map[string]any{"alias": a})
	case "DELETE":
		trigger := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("trigger")))
		if trigger == "" {
			globalAliases.Reset()
			writeJSON(w, 200, map[string]bool{"ok": true})
			return
		}
		if !globalAliases.Del(trigger) {
			writeJSON(w, 404, map[string]string{"error": "alias not found"})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}
