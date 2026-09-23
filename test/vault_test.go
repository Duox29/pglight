package test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"pglight/internal/store"
)

func TestVaultLifecycleAndSecretAtRest(t *testing.T) {
	h := newStoreHandler(t)
	code, body := callGET(t, h.Vault, "/api/vault")
	requireStatus(t, body, code, 200)
	status := decodeObj(t, body)
	if status["exists"] != false || status["unlocked"] != false {
		t.Fatalf("unexpected initial vault status: %s", body)
	}
	code, body = callPOST(t, h.Vault, "/api/vault", `{"action":"setup","master_password":"correct horse battery staple"}`)
	requireStatus(t, body, code, 200)
	if decodeObj(t, body)["unlocked"] != true {
		t.Fatalf("vault did not unlock after setup: %s", body)
	}
	code, body = callPOST(t, h.Connections, "/api/connections", `{"name":"local","host":"localhost","port":5432,"user":"postgres","dbname":"postgres","sslmode":"disable","password":"postgres","save_password":true}`)
	requireStatus(t, body, code, 200)
	profile := decodeObj(t, body)["connection"].(map[string]any)
	if profile["has_password"] != true {
		t.Fatalf("profile did not report a saved password: %s", body)
	}
	var ciphertext string
	if err := h.Store.DB().QueryRowContext(context.Background(), `SELECT hex(ciphertext) FROM connection_secrets WHERE connection_id=?`, profile["id"]).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(ciphertext), "706f737467726573") {
		t.Fatalf("password appears in ciphertext: %s", ciphertext)
	}
	code, body = callPOST(t, h.Connections, "/api/connections", `{"name":"local-copy","host":"localhost","port":5432,"user":"postgres","dbname":"postgres","sslmode":"disable","duplicate_from":"`+profile["id"].(string)+`"}`)
	requireStatus(t, body, code, 200)
	if decodeObj(t, body)["connection"].(map[string]any)["has_password"] != true {
		t.Fatalf("duplicated profile lost its vault secret: %s", body)
	}
	code, body = callPOST(t, h.Vault, "/api/vault", `{"action":"lock"}`)
	requireStatus(t, body, code, 200)
	code, body = callPOST(t, h.Connect, "/api/connect", `{"profile_id":"`+profile["id"].(string)+`"}`)
	requireErrContains(t, body, code, 409, "vault is locked")
	code, body = callPOST(t, h.Vault, "/api/vault", `{"action":"unlock","master_password":"wrong password"}`)
	requireErrContains(t, body, code, 401, "invalid master password")
	// Failed unlocks impose a short server-side backoff before the next KDF.
	time.Sleep(300 * time.Millisecond)
	code, body = callPOST(t, h.Vault, "/api/vault", `{"action":"unlock","master_password":"correct horse battery staple"}`)
	requireStatus(t, body, code, 200)
	// Destructive vault reset is intentionally an offline CLI operation, not
	// an HTTP action. Keep the API contract explicit here.
	code, body = callPOST(t, h.Vault, "/api/vault", `{"action":"reset","master_password":"correct horse battery staple"}`)
	requireErrContains(t, body, code, 400, "unknown vault action")
}

func TestSSHProfileAndVaultCredentialSurviveStoreReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ssh-profiles.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	const userID = "ssh-reopen-user"
	if err := s.EnsureUser(ctx, userID); err != nil {
		t.Fatal(err)
	}
	record, key, err := store.NewVault("vault master password")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateVault(ctx, userID, record); err != nil {
		t.Fatal(err)
	}
	profile, err := s.SaveConnectionProfile(ctx, userID, store.ConnectionSave{
		Name: "ssh saved", Host: "db.internal", Port: 5432, User: "app", DBName: "main", SSLMode: "verify-full",
		Options:    store.ConnectionOptions{SSHEnabled: true, SSHHost: "bastion.internal", SSHPort: 22, SSHUser: "deploy", SSHAuthMethod: "private_key", SSHHostKey: "SHA256:pinned"},
		SSHSecrets: store.SSHSecretValues{PrivateKey: "private-key-material", Passphrase: "key-passphrase"},
	}, key)
	for i := range key {
		key[i] = 0
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.GetConnection(ctx, userID, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Options.SSHEnabled || loaded.Options.SSHHostKey != "SHA256:pinned" {
		t.Fatalf("SSH profile metadata did not persist: %+v", loaded.Options)
	}
	newRecord, err := reopened.Vault(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := store.UnlockVault(newRecord, "vault master password")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for i := range newKey {
			newKey[i] = 0
		}
	}()
	secret, found, err := reopened.SSHSecret(ctx, userID, profile.ID, newKey)
	if err != nil || !found {
		t.Fatalf("SSH secret after reopen: found=%v err=%v", found, err)
	}
	defer func() {
		for i := range secret {
			secret[i] = 0
		}
	}()
	if !strings.Contains(string(secret), "private-key-material") {
		t.Fatalf("SSH credential did not persist: %s", secret)
	}
}

func TestSSHTunnelProfileSecretsStayInVaultAndEncryptedTransfer(t *testing.T) {
	h := newStoreHandler(t)
	code, body := callPOST(t, h.Vault, "/api/vault", `{"action":"setup","master_password":"correct horse battery staple"}`)
	requireStatus(t, body, code, 200)
	bodyText := `{"name":"ssh-profile","host":"db.internal","port":5432,"user":"postgres","dbname":"app","sslmode":"verify-full","ssh_enabled":true,"ssh_host":"bastion.internal","ssh_port":22,"ssh_user":"deploy","ssh_auth_method":"password","ssh_host_key":"SHA256:host-key-pin","ssh_password":"ssh-password-marker"}`
	code, body = callPOST(t, h.Connections, "/api/connections", bodyText)
	requireStatus(t, body, code, 200)
	profile := decodeObj(t, body)["connection"].(map[string]any)
	if strings.Contains(body, "ssh-password-marker") {
		t.Fatalf("profile response exposed SSH credential: %s", body)
	}
	options := profile["options"].(map[string]any)
	if options["ssh_enabled"] != true || options["ssh_host_key"] != "SHA256:host-key-pin" {
		t.Fatalf("SSH metadata missing from profile response: %s", body)
	}
	var encryptedSSHSecret string
	if err := h.Store.DB().QueryRowContext(context.Background(), `SELECT hex(ciphertext) FROM connection_ssh_secrets WHERE connection_id=?`, profile["id"]).Scan(&encryptedSSHSecret); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(encryptedSSHSecret), "7373682d70617373776f72642d6d61726b6572") {
		t.Fatalf("SSH password appears in ciphertext: %s", encryptedSSHSecret)
	}
	code, body = callPOST(t, h.Connections, "/api/connections", `{"name":"ssh-copy","host":"db.internal","port":5432,"user":"postgres","dbname":"app","sslmode":"verify-full","duplicate_from":"`+profile["id"].(string)+`"}`)
	requireStatus(t, body, code, 200)
	copyProfile := decodeObj(t, body)["connection"].(map[string]any)
	if copyProfile["options"].(map[string]any)["ssh_enabled"] != true {
		t.Fatalf("duplicate lost SSH settings: %s", body)
	}
	var duplicateSecretCount int
	if err := h.Store.DB().QueryRowContext(context.Background(), `SELECT count(*) FROM connection_ssh_secrets WHERE connection_id=?`, copyProfile["id"]).Scan(&duplicateSecretCount); err != nil || duplicateSecretCount != 1 {
		t.Fatalf("duplicate SSH secret count=%d err=%v", duplicateSecretCount, err)
	}

	plainCode, plainBody := callGET(t, h.ConnectionExport, "/api/connections/export")
	requireStatus(t, plainBody, plainCode, 200)
	if strings.Contains(plainBody, "ssh-password-marker") {
		t.Fatalf("plain export exposed SSH credential: %s", plainBody)
	}

	encryptedCode, encryptedBody := callPOST(t, h.ConnectionExport, "/api/connections/export", `{"encrypted":true,"password":"export password"}`)
	requireStatus(t, encryptedBody, encryptedCode, 200)
	var encrypted json.RawMessage
	if err := json.Unmarshal([]byte(encryptedBody), &encrypted); err != nil {
		t.Fatalf("encrypted export is not JSON: %v", err)
	}
	plain, err := store.DecryptJSON("export password", encrypted)
	if err != nil {
		t.Fatalf("decrypt encrypted export: %v", err)
	}
	if !strings.Contains(string(plain), "ssh-password-marker") {
		t.Fatalf("encrypted export omitted SSH credentials: %s", plain)
	}
	importCode, importBody := callPOST(t, h.ConnectionImport, "/api/connections/import", `{"payload":`+encryptedBody+`,"password":"export password"}`)
	requireStatus(t, importBody, importCode, 200)
	if decodeObj(t, importBody)["imported"] != float64(2) {
		t.Fatalf("encrypted import did not restore both profiles: %s", importBody)
	}

	if err := h.Store.ChangeVaultMaster(context.Background(), h.UserID, "correct horse battery staple", "replacement master password"); err != nil {
		t.Fatal(err)
	}
	vault, err := h.Store.Vault(context.Background(), h.UserID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.UnlockVault(vault, "replacement master password")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	secret, found, err := h.Store.SSHSecret(context.Background(), h.UserID, profile["id"].(string), key)
	if err != nil || !found {
		t.Fatalf("read rotated SSH secret: found=%v err=%v", found, err)
	}
	defer func() {
		for i := range secret {
			secret[i] = 0
		}
	}()
	if !strings.Contains(string(secret), "ssh-password-marker") {
		t.Fatalf("rotated SSH secret mismatch: %s", secret)
	}
}

func TestVaultBackedConnectionTestAgainstDocker(t *testing.T) {
	h, _ := newHandler(t)
	port := strconv.Itoa(testPort())
	code, body := callPOST(t, h.Vault, "/api/vault", `{"action":"setup","master_password":"correct horse battery staple"}`)
	requireStatus(t, body, code, 200)
	code, body = callPOST(t, h.Connections, "/api/connections", `{"name":"docker","host":"localhost","port":`+port+`,"user":"postgres","dbname":"postgres","sslmode":"disable","password":"postgres","save_password":true}`)
	requireStatus(t, body, code, 200)
	profile := decodeObj(t, body)["connection"].(map[string]any)
	code, body = callPOST(t, h.TestConnection, "/api/connections/test", `{"profile_id":"`+profile["id"].(string)+`"}`)
	requireStatus(t, body, code, 200)
	code, body = callPOST(t, h.Vault, "/api/vault", `{"action":"lock"}`)
	requireStatus(t, body, code, 200)
	code, body = callPOST(t, h.TestConnection, "/api/connections/test", `{"profile_id":"`+profile["id"].(string)+`"}`)
	requireErrContains(t, body, code, 409, "vault is locked")
}
