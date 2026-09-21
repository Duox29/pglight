package test

import (
	"context"
	"strconv"
	"strings"
	"testing"
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
	code, body = callPOST(t, h.Vault, "/api/vault", `{"action":"unlock","master_password":"correct horse battery staple"}`)
	requireStatus(t, body, code, 200)
	code, body = callPOST(t, h.Vault, "/api/vault", `{"action":"reset","master_password":"correct horse battery staple"}`)
	requireStatus(t, body, code, 200)
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
