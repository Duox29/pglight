package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"pglight/internal/api"
	"pglight/internal/db"
	"pglight/internal/logging"
	"pglight/internal/store"

	"golang.org/x/term"
)

//go:embed web/dist
var webFS embed.FS

func main() {
	options, err := parseStartupOptions(os.Args[1:], os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	storePath := os.Getenv("PGLIGHT_STORE")
	userID := os.Getenv("PGLIGHT_USER_ID")
	if userID == "" {
		userID = "default"
	}
	if options.vaultReset {
		if options.vaultStore != "" {
			storePath = options.vaultStore
		}
		if options.vaultUser != "" {
			userID = options.vaultUser
		}
		if err := resetVaultFromCLI(storePath, userID, options); err != nil {
			log.Fatal("reset vault: ", err)
		}
		return
	}
	storePath, err = store.ResolvePath(storePath)
	if err != nil {
		log.Fatal("resolve app storage: ", err)
	}
	storeLock, err := store.AcquireProcessLock(storePath)
	if err != nil {
		log.Fatal("lock app storage: ", err)
	}
	defer storeLock.Close()

	mgr := db.New()
	mgr.StartSweeper(db.DefaultSweepInterval, db.DefaultIdleTTL, db.DefaultTxnIdleTTL)
	appLog := logging.New("data/logging.json")
	appStore, err := store.Open(storePath)
	if err != nil {
		log.Fatal("open app storage: ", err)
	}
	defer appStore.Close()
	if err := appStore.EnsureUser(context.Background(), userID); err != nil {
		log.Fatal("initialize app user: ", err)
	}
	h := &api.Handler{Mgr: mgr, Log: appLog, Store: appStore, UserID: userID}
	serverSettings, err := api.LoadServerSettings(api.ServerSettingsPath)
	if err != nil {
		log.Printf("warning: could not load server settings: %v; using defaults", err)
		serverSettings = api.ServerSettings{}
	}
	h.ServerConfig = serverSettings
	h.ServerSettingsPath = api.ServerSettingsPath

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
	mux.HandleFunc("/api/connections/folders", h.ConnectionFolders)
	mux.HandleFunc("/api/connections/export", h.ConnectionExport)
	mux.HandleFunc("/api/connections/import", h.ConnectionImport)
	mux.HandleFunc("/api/connections/test", h.TestConnection)
	mux.HandleFunc("/api/vault", h.Vault)
	mux.HandleFunc("/api/preferences", h.Preferences)
	mux.HandleFunc("/api/preferences/shortcuts", h.ShortcutPreferences)
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

	listener, port, err := listenHTTP(options)
	if err != nil {
		log.Fatal(err)
	}

	appURL := "http://127.0.0.1:" + strconv.Itoa(port)
	if serverSettings.AllowLANAccess {
		log.Printf("pglight listening on %s (LAN access enabled)", appURL)
	} else {
		log.Println("pglight listening on " + appURL + " (LAN requests blocked)")
	}
	if !options.noBrowser {
		if err := openBrowser(appURL); err != nil {
			log.Printf("warning: could not open browser: %v", err)
		}
	}

	server := &http.Server{Handler: logging.Middleware(appLog, h.LANAccess(mux))}
	log.Fatal(server.Serve(listener))
}

func resetVaultFromCLI(storePath, userID string, options startupOptions) error {
	resolvedPath, err := store.ExistingPath(storePath)
	if err != nil {
		return err
	}
	storeLock, err := store.AcquireProcessLock(resolvedPath)
	if err != nil {
		return err
	}
	defer storeLock.Close()

	appStore, err := store.Open(resolvedPath)
	if err != nil {
		return err
	}
	defer appStore.Close()
	ctx := context.Background()
	info, err := appStore.VaultResetInfo(ctx, userID)
	if err != nil {
		return err
	}
	if err := confirmVaultReset(resolvedPath, userID, info, options); err != nil {
		return err
	}
	newMaster, err := readVaultResetPassword(options.vaultPasswordStdin)
	if err != nil {
		return err
	}
	if err := validateResetMasterPassword(newMaster); err != nil {
		return err
	}
	if err := appStore.ResetVault(ctx, userID, newMaster); err != nil {
		var cleanupErr *store.VaultResetCleanupError
		if !errors.As(err, &cleanupErr) {
			return err
		}
		log.Printf("warning: %v", cleanupErr)
	}
	log.Printf("vault reset complete: store=%s user=%s removed_secrets=%d preserved_profiles=%d", resolvedPath, userID, info.Secrets, info.Profiles)
	return nil
}

func confirmVaultReset(storePath, userID string, info store.VaultResetInfo, options startupOptions) error {
	if options.vaultResetYes {
		return nil
	}
	if options.vaultPasswordStdin {
		return errors.New("--yes is required when using --password-stdin")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("reset confirmation requires an interactive terminal; use --password-stdin --yes")
	}
	fmt.Fprintf(os.Stderr, "This will delete %d saved database password(s) from %s for user %s; %d profile(s) will remain.\nType RESET to continue: ", info.Secrets, storePath, userID, info.Profiles)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read reset confirmation: %w", err)
	}
	if strings.TrimSpace(line) != "RESET" {
		return errors.New("vault reset cancelled")
	}
	return nil
}

func readVaultResetPassword(fromStdin bool) (string, error) {
	if fromStdin {
		input, err := io.ReadAll(io.LimitReader(os.Stdin, 4097))
		if err != nil {
			return "", fmt.Errorf("read new master password: %w", err)
		}
		if len(input) > 4096 {
			return "", errors.New("new master password input is too long")
		}
		return strings.TrimRight(string(input), "\r\n"), nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("password input requires an interactive terminal; use --password-stdin")
	}
	fmt.Fprint(os.Stderr, "New master password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read new master password: %w", err)
	}
	defer clear(first)
	fmt.Fprint(os.Stderr, "Confirm new master password: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password confirmation: %w", err)
	}
	defer clear(second)
	if !bytes.Equal(first, second) {
		return "", errors.New("master passwords do not match")
	}
	return string(first), nil
}

func validateResetMasterPassword(password string) error {
	length := len([]rune(password))
	if length < 8 {
		return errors.New("master password must be at least 8 characters")
	}
	if length > 256 {
		return errors.New("master password must be at most 256 characters")
	}
	return nil
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
