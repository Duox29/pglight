// Package logging implements DbClient's AOP-style observability: one HTTP
// middleware plus one Querier wrapper capture every request and query in a
// central ring buffer, with runtime config (see Config) persisted to disk.
// Handlers must NOT log ad-hoc; route everything through Logger.
package logging

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

func ParseLevel(s string) Level {
	switch s {
	case "debug":
		return Debug
	case "warn", "warning":
		return Warn
	case "error":
		return Error
	default:
		return Info
	}
}

func (l Level) String() string {
	switch l {
	case Debug:
		return "debug"
	case Warn:
		return "warn"
	case Error:
		return "error"
	default:
		return "info"
	}
}

// Config is edited from the web Settings panel and persisted as JSON.
type Config struct {
	Enabled    bool   `json:"enabled"`
	Level      string `json:"level"` // debug|info|warn|error
	LogHTTP    bool   `json:"log_http"`
	LogQuery   bool   `json:"log_query"`
	SlowMs     int64  `json:"slow_ms"`
	MaxEntries int    `json:"max_entries"`
}

func DefaultConfig() Config {
	return Config{Enabled: true, Level: "info", LogHTTP: true, LogQuery: true, SlowMs: 500, MaxEntries: 500}
}

func normalize(c Config) Config {
	d := DefaultConfig()
	if c.Level == "" {
		c.Level = d.Level
	}
	ParseLevel(c.Level) // valid values only; String() falls back to info
	c.Level = ParseLevel(c.Level).String()
	if c.SlowMs < 0 {
		c.SlowMs = d.SlowMs
	}
	if c.MaxEntries < 50 {
		c.MaxEntries = 50
	}
	if c.MaxEntries > 2000 {
		c.MaxEntries = 2000
	}
	return c
}

type Entry struct {
	Time       string `json:"time"`
	Level      string `json:"level"`
	Category   string `json:"category"` // http|query|txn|system
	Message    string `json:"message"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type Logger struct {
	mu   sync.RWMutex
	cfg  Config
	buf  []Entry
	path string
}

func New(path string) *Logger {
	l := &Logger{cfg: DefaultConfig(), path: path}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			var c Config
			if json.Unmarshal(raw, &c) == nil {
				l.cfg = normalize(c)
			}
		}
	}
	return l
}

func (l *Logger) GetConfig() Config {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cfg
}

// UpdateConfig replaces the config (normalized) and persists it.
func (l *Logger) UpdateConfig(c Config) (Config, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cfg = normalize(c)
	if l.path != "" {
		if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
			return l.cfg, err
		}
		raw, _ := json.MarshalIndent(l.cfg, "", "  ")
		if err := os.WriteFile(l.path, raw, 0o644); err != nil {
			return l.cfg, err
		}
	}
	l.trim()
	return l.cfg, nil
}

func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = nil
}

func (l *Logger) Enabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cfg.Enabled
}

func (l *Logger) shouldLocked(level Level) bool {
	return l.cfg.Enabled && level >= ParseLevel(l.cfg.Level)
}

func (l *Logger) Add(level Level, category, message string, durationMs int64, detail string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.shouldLocked(level) {
		return
	}
	l.buf = append(l.buf, Entry{
		Time: time.Now().Format(time.RFC3339), Level: level.String(),
		Category: category, Message: message, DurationMs: durationMs, Detail: detail,
	})
	l.trim()
}

func (l *Logger) trim() {
	if len(l.buf) > l.cfg.MaxEntries {
		l.buf = append([]Entry(nil), l.buf[len(l.buf)-l.cfg.MaxEntries:]...)
	}
}

// Recent returns newest-first entries matching the filters.
// Empty minLevel/category means no filtering on that axis.
func (l *Logger) Recent(limit int, minLevel, category string) []Entry {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	lvl := Debug
	if minLevel != "" {
		lvl = ParseLevel(minLevel)
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := []Entry{}
	for i := len(l.buf) - 1; i >= 0 && len(out) < limit; i-- {
		e := l.buf[i]
		if ParseLevel(e.Level) < lvl {
			continue
		}
		if category != "" && e.Category != category {
			continue
		}
		out = append(out, e)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// LogHTTP records one request unless disabled or on the skip list.
// The logs-polling endpoint itself is skipped to avoid self-noise.
func (l *Logger) LogHTTP(r *http.Request, status int, durationMs int64) {
	l.mu.RLock()
	enabled, want := l.cfg.Enabled, l.cfg.LogHTTP
	l.mu.RUnlock()
	if !enabled || !want {
		return
	}
	if r.URL.Path == "/api/logs" {
		return
	}
	lvl := Info
	if status >= 500 {
		lvl = Error
	}
	l.Add(lvl, "http", r.Method+" "+r.URL.Path, durationMs, http.StatusText(status))
}

func (l *Logger) queryOn() (bool, int64) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cfg.Enabled && l.cfg.LogQuery, l.cfg.SlowMs
}

// LogQuery records one statement. Steady-state traffic is Debug so the
// default info level shows only slow queries and errors.
func (l *Logger) LogQuery(sql string, durationMs int64, qerr error) {
	on, slow := l.queryOn()
	if !on {
		return
	}
	detail := truncate(sql, 500)
	switch {
	case qerr != nil:
		l.Add(Error, "query", truncate(qerr.Error(), 300), durationMs, detail)
	case slow > 0 && durationMs >= slow:
		l.Add(Warn, "query", "slow query", durationMs, detail)
	default:
		l.Add(Debug, "query", detail, durationMs, "")
	}
}

func (l *Logger) LogTxn(action string, qerr error) {
	if qerr != nil {
		l.Add(Error, "txn", action+" failed: "+truncate(qerr.Error(), 200), 0, "")
		return
	}
	l.Add(Info, "txn", action, 0, "")
}
