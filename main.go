package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"

	"dbclient/internal/api"
	"dbclient/internal/db"
)

//go:embed web/*
var webFS embed.FS

func main() {
	mgr := db.New()
	h := &api.Handler{Mgr: mgr}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/connect", h.Connect)
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
	mux.HandleFunc("/api/complete", h.Complete)

	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Println("DbClient listening on :" + port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
