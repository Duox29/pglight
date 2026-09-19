package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	"pglight/internal/api"
	"pglight/internal/db"
	"pglight/internal/logging"
	"pglight/internal/store"
)

//go:embed web/dist
var webFS embed.FS

func main() {
	mgr := db.New()
	mgr.StartSweeper(db.DefaultSweepInterval, db.DefaultIdleTTL, db.DefaultTxnIdleTTL)
	appLog := logging.New("data/logging.json")
	storePath := os.Getenv("PGLIGHT_STORE")
	appStore, err := store.Open(storePath)
	if err != nil {
		log.Fatal("open app storage: ", err)
	}
	defer appStore.Close()
	userID := os.Getenv("PGLIGHT_USER_ID")
	if userID == "" {
		userID = "default"
	}
	if err := appStore.EnsureUser(context.Background(), userID); err != nil {
		log.Fatal("initialize app user: ", err)
	}
	h := &api.Handler{Mgr: mgr, Log: appLog, Store: appStore, UserID: userID}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/connect", h.Connect)
	mux.HandleFunc("/api/sessions", h.Sessions)
	mux.HandleFunc("/api/disconnect", h.Disconnect)
	mux.HandleFunc("/api/databases", h.Databases)
	mux.HandleFunc("/api/schemas", h.Schemas)
	mux.HandleFunc("/api/tables", h.Tables)
	mux.HandleFunc("/api/objects", h.Objects)
	mux.HandleFunc("/api/columns", h.Columns)
	mux.HandleFunc("/api/ddl", h.DDL)
	mux.HandleFunc("/api/table-data", h.TableData)
	mux.HandleFunc("/api/query", h.Query)
	mux.HandleFunc("/api/explain", h.Explain)
	mux.HandleFunc("/api/activity", h.Activity)
	mux.HandleFunc("/api/cancel", h.Cancel)
	mux.HandleFunc("/api/row", h.RowOp)
	mux.HandleFunc("/api/rows-delete", h.BatchDelete)
	mux.HandleFunc("/api/complete", h.Complete)
	mux.HandleFunc("/api/aliases", h.Aliases)
	mux.HandleFunc("/api/snippets", h.Snippets)
	mux.HandleFunc("/api/connections", h.Connections)
	mux.HandleFunc("/api/preferences", h.Preferences)
	mux.HandleFunc("/api/history", h.History)
	mux.HandleFunc("/api/txn", h.Txn)
	mux.HandleFunc("/api/server-info", h.ServerInfo)
	mux.HandleFunc("/api/stats", h.Stats)
	mux.HandleFunc("/api/locks", h.Locks)
	mux.HandleFunc("/api/roles", h.Roles)
	mux.HandleFunc("/api/extensions", h.Extensions)
	mux.HandleFunc("/api/types", h.Types)
	mux.HandleFunc("/api/triggers", h.Triggers)
	mux.HandleFunc("/api/constraints", h.Constraints)
	mux.HandleFunc("/api/view-def", h.ViewDef)
	mux.HandleFunc("/api/func-def", h.FuncDef)
	mux.HandleFunc("/api/seq-def", h.SeqDef)
	mux.HandleFunc("/api/type-def", h.TypeDef)
	mux.HandleFunc("/api/table-stats", h.TableStats)
	mux.HandleFunc("/api/erd", h.ERD)
	mux.HandleFunc("/api/search", h.Search)
	mux.HandleFunc("/api/maintenance", h.Maintenance)
	mux.HandleFunc("/api/import", h.Import)
	mux.HandleFunc("/api/mock-data/meta", h.MockMeta)
	mux.HandleFunc("/api/mock-data/preview", h.MockPreview)
	mux.HandleFunc("/api/mock-data/generate", h.MockGenerate)
	mux.HandleFunc("/api/alter-table", h.AlterTable)
	mux.HandleFunc("/api/settings", h.Settings)
	mux.HandleFunc("/api/logs", h.Logs)
	mux.HandleFunc("/api/shutdown", h.Shutdown)

	sub, _ := fs.Sub(webFS, "web/dist")
	mux.Handle("/", spaHandler(sub, http.FileServer(http.FS(sub))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Println("pglight listening on :" + port)
	log.Fatal(http.ListenAndServe(":"+port, logging.Middleware(appLog, mux)))
}

// spaHandler serves the embedded Vite build (web/dist). Unknown non-API paths
// fall back to index.html so the React app boots. API routes are registered
// first on the mux, so they never reach here.
func spaHandler(sub fs.FS, files http.Handler) http.Handler {
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return files
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/" {
			if f, err := sub.Open(strings.TrimPrefix(r.URL.Path, "/")); err == nil {
				f.Close()
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
}
