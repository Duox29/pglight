// Package mockgen is the pure, deterministic mock-data generation engine
// behind /api/mock-data/* (see internal/api/mockdata.go).
//
// It is intentionally dependency-free and database-free: callers load a
// TableMeta (via LoadMeta in the API layer or by hand in tests), build a
// Plan with BuildPlan, then materialize rows with GenerateRows. Foreign-key
// value pools and sequence counters are injected so tests can drive the
// engine without PostgreSQL.
package mockgen

import "strings"

// ColumnMeta is one normalized table column for mock generation.
type ColumnMeta struct {
	Name         string   `json:"name"`
	DataType     string   `json:"data_type"`
	Udt          string   `json:"udt"`
	Nullable     bool     `json:"nullable"`
	Default      *string  `json:"default"`
	Identity     bool     `json:"identity"`
	Generated    bool     `json:"generated"`
	PrimaryKey   bool     `json:"primary_key"`
	Unique       bool     `json:"unique"`
	EnumValues   []string `json:"enum_values"`
	SemanticHint string   `json:"semantic_hint"`
}

// ForeignKeyMeta maps one local column to one referenced column.
type ForeignKeyMeta struct {
	Name      string `json:"name"`
	Column    string `json:"column"`
	RefSchema string `json:"ref_schema"`
	RefTable  string `json:"ref_table"`
	RefColumn string `json:"ref_column"`
}

// CheckMeta is one CHECK constraint with a best-effort structural hint.
// Kind is one of: between, in, comparison, unsupported.
type CheckMeta struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	Kind       string `json:"kind"`
	Warning    string `json:"warning,omitempty"`
}

// TableMeta is the normalized introspection result served by
// GET /api/mock-data/meta. React never parses DDL; it consumes this.
type TableMeta struct {
	Columns     []ColumnMeta     `json:"columns"`
	ForeignKeys []ForeignKeyMeta `json:"foreign_keys"`
	Checks      []CheckMeta      `json:"checks"`
}

// ColumnByName returns the column meta for name, or nil.
func (m *TableMeta) ColumnByName(name string) *ColumnMeta {
	for i := range m.Columns {
		if m.Columns[i].Name == name {
			return &m.Columns[i]
		}
	}
	return nil
}

// IsSerialDefault reports a nextval(...) column default (serial/bigserial).
func IsSerialDefault(def *string) bool {
	if def == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(strings.ToLower(*def)), "nextval")
}

// OmittedInSimple reports whether Simple mode skips the column and lets
// PostgreSQL fill it: identity/generated/serial columns and any column
// with a database default.
func OmittedInSimple(c ColumnMeta) bool {
	if c.Identity || c.Generated {
		return true
	}
	if c.Default != nil {
		return true
	}
	return false
}

// SemanticHintFor derives the Advanced-mode Auto generator hint from a
// column name. Simple mode MUST NOT call this.
func SemanticHintFor(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(n, "email"):
		return "email"
	case n == "first_name" || n == "firstname":
		return "first_name"
	case n == "last_name" || n == "lastname":
		return "last_name"
	case n == "full_name" || n == "fullname" || n == "name":
		return "full_name"
	case n == "username" || n == "user_name" || n == "login":
		return "username"
	case strings.Contains(n, "phone") || strings.Contains(n, "mobile") || n == "tel":
		return "phone"
	case n == "url" || n == "website" || n == "homepage" || n == "link" || strings.HasSuffix(n, "_url"):
		return "url"
	case n == "price" || n == "amount" || n == "total" || n == "cost" || n == "balance":
		return "decimal"
	case n == "created_at" || n == "updated_at":
		return "datetime"
	default:
		return ""
	}
}
