package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "pglight.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.EnsureUser(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVaultRotateReencryptsSecretsAndKeepsVersion(t *testing.T) {
	s := testStore(t)
	record, key, err := NewVault("old-password")
	if err != nil {
		t.Fatal(err)
	}
	if record.Version != vaultVersion {
		t.Fatalf("version=%d, want %d", record.Version, vaultVersion)
	}
	if err := s.CreateVault(context.Background(), "u", record); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`INSERT INTO connection_profiles(id,user_id,name,host,username,dbname) VALUES('profile','u','local','localhost','postgres','postgres')`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConnectionSecret(context.Background(), "u", "profile", key, "db-secret"); err != nil {
		t.Fatal(err)
	}
	zeroBytes(key)

	if err := s.ChangeVaultMaster(context.Background(), "u", "old-password", "new-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := UnlockVault(record, "new-password"); !errors.Is(err, ErrVaultInvalid) {
		t.Fatalf("old verifier unexpectedly accepted new password: %v", err)
	}
	rotated, err := s.Vault(context.Background(), "u")
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := UnlockVault(rotated, "new-password")
	if err != nil {
		t.Fatal(err)
	}
	decrypted, ok, err := s.ConnectionSecret(context.Background(), "u", "profile", newKey)
	zeroBytes(newKey)
	if err != nil || !ok || decrypted != "db-secret" {
		t.Fatalf("rotated secret = %q, %v, %v", decrypted, ok, err)
	}
	if _, err := UnlockVault(rotated, "old-password"); !errors.Is(err, ErrVaultInvalid) {
		t.Fatalf("old password still accepted: %v", err)
	}
}

func TestUnlockVaultRejectsCorruptionAndUnknownVersion(t *testing.T) {
	record, _, err := NewVault("master-password")
	if err != nil {
		t.Fatal(err)
	}
	record.CheckCiphertext[0] ^= 1
	if _, err := UnlockVault(record, "master-password"); !errors.Is(err, ErrVaultInvalid) {
		t.Fatalf("corruption error=%v", err)
	}
	record, _, err = NewVault("master-password")
	if err != nil {
		t.Fatal(err)
	}
	record.Version = 99
	if _, err := UnlockVault(record, "master-password"); !errors.Is(err, ErrVaultUnsupported) {
		t.Fatalf("unknown version error=%v", err)
	}
}

func TestUnlockVaultConcurrentIsRaceSafe(t *testing.T) {
	record, _, err := NewVault("master-password")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, err := UnlockVault(record, "master-password")
			if err != nil {
				t.Error(err)
				return
			}
			zeroBytes(key)
		}()
	}
	wg.Wait()
}

func TestEncryptedJSONRoundTripRejectsWrongPassword(t *testing.T) {
	blob, err := EncryptJSON("export-password", []byte(`{"name":"local"}`))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := DecryptJSON("export-password", blob)
	if err != nil || string(plain) != `{"name":"local"}` {
		t.Fatalf("round trip = %q, %v", plain, err)
	}
	if _, err := DecryptJSON("wrong-password", blob); !errors.Is(err, ErrVaultInvalid) {
		t.Fatalf("wrong password error=%v", err)
	}
}

func TestResetVaultReplacesSecretsAndPreservesProfiles(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	record, key, err := NewVault("old-password")
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(key)
	if err := s.CreateVault(ctx, "u", record); err != nil {
		t.Fatal(err)
	}
	profile, err := s.UpsertConnection(ctx, "u", "local", "localhost", 5432, "postgres", "postgres", "disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetConnectionSecret(ctx, "u", profile.ID, key, "db-secret"); err != nil {
		t.Fatal(err)
	}

	if err := s.ResetVault(ctx, "u", "new-password"); err != nil {
		t.Fatal(err)
	}
	reset, err := s.Vault(ctx, "u")
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := UnlockVault(reset, "new-password")
	if err != nil {
		t.Fatal(err)
	}
	zeroBytes(newKey)
	if _, err := UnlockVault(reset, "old-password"); !errors.Is(err, ErrVaultInvalid) {
		t.Fatalf("old password accepted after reset: %v", err)
	}
	profiles, err := s.ListConnections(ctx, "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].ID != profile.ID || profiles[0].Name != "local" {
		t.Fatalf("profiles after reset = %+v", profiles)
	}
	var secrets int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_secrets WHERE user_id=?`, "u").Scan(&secrets); err != nil {
		t.Fatal(err)
	}
	if secrets != 0 {
		t.Fatalf("secret count after reset = %d, want 0", secrets)
	}
}
