package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// Completion aliases (ssf => SELECT * FROM) are a backend-owned domain:
// the editor fetches them once via GET /api/aliases and reads them from
// memory per keystroke (same zero-network shape as the schema snapshot).
// Defaults are hardcoded; user entries are persisted in the application SQLite store.

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

// Aliases serves the alias domain: GET lists merged entries (builtin +
// user), POST upserts a user entry, DELETE drops one (?trigger=) or clears
// all user entries (no param, restoring defaults). Global like
// /api/settings — no session, no PG.
func (h *Handler) Aliases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		custom, err := h.Store.ListAliases(r.Context(), h.UserID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		merged := make(map[string]Alias, len(defaultAliases)+len(custom))
		for _, d := range defaultAliases {
			merged[d.Trigger] = d
		}
		for _, c := range custom {
			merged[c.Trigger] = Alias{Trigger: c.Trigger, Expansion: c.Expansion, Builtin: false}
		}
		out := make([]Alias, 0, len(merged))
		for _, a := range merged {
			out = append(out, a)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Trigger < out[j].Trigger })
		writeJSON(w, 200, map[string]any{"aliases": out})
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
		custom, err := h.Store.ListAliases(r.Context(), h.UserID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if len(custom) >= maxAliases {
			found := false
			for _, a := range custom {
				if a.Trigger == trigger {
					found = true
					break
				}
			}
			if !found {
				writeJSON(w, 400, map[string]string{"error": "alias limit reached"})
				return
			}
		}
		a, err := h.Store.UpsertAlias(r.Context(), h.UserID, trigger, req.Expansion)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"alias": Alias{Trigger: a.Trigger, Expansion: a.Expansion, Builtin: false}})
	case "DELETE":
		trigger := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("trigger")))
		if trigger == "" {
			custom, err := h.Store.ListAliases(r.Context(), h.UserID)
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			for _, a := range custom {
				if _, err := h.Store.DeleteAlias(r.Context(), h.UserID, a.Trigger); err != nil {
					writeJSON(w, 500, map[string]string{"error": err.Error()})
					return
				}
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
			return
		}
		deleted, err := h.Store.DeleteAlias(r.Context(), h.UserID, trigger)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if !deleted {
			writeJSON(w, 404, map[string]string{"error": "alias not found"})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}
