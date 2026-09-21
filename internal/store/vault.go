package store

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	vaultKeyLen       = 32
	vaultSaltLen      = 16
	vaultKDFMemoryKiB = 64 * 1024
	vaultKDFTime      = 3
	vaultKDFThreads   = 1
	vaultVersion      = 2
	vaultCheckTextV2  = "pglight-vault-check-v2"
)

var ErrVaultNotFound = errors.New("vault is not configured")
var ErrVaultExists = errors.New("vault already exists")
var ErrVaultInvalid = errors.New("invalid master password")
var ErrVaultUnsupported = errors.New("unsupported vault version")
var ErrUserNotFound = errors.New("app user does not exist")

type VaultResetCleanupError struct {
	Err error
}

func (e *VaultResetCleanupError) Error() string {
	return fmt.Sprintf("vault reset committed but SQLite cleanup failed: %v", e.Err)
}

func (e *VaultResetCleanupError) Unwrap() error { return e.Err }

type EncryptedJSON struct {
	Format     string `json:"format"`
	Version    int    `json:"version"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func EncryptJSON(master string, payload []byte) ([]byte, error) {
	salt := make([]byte, vaultSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	key := deriveVaultKey(master, salt)
	defer zeroBytes(key)
	nonce, ciphertext, err := seal(key, payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(EncryptedJSON{Format: "pglight-encrypted-json", Version: 1, Salt: base64.RawStdEncoding.EncodeToString(salt), Nonce: base64.RawStdEncoding.EncodeToString(nonce), Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext)})
}

func DecryptJSON(master string, encoded []byte) ([]byte, error) {
	var blob EncryptedJSON
	if err := json.Unmarshal(encoded, &blob); err != nil {
		return nil, ErrVaultInvalid
	}
	if blob.Format != "pglight-encrypted-json" || blob.Version != 1 {
		return nil, ErrVaultUnsupported
	}
	salt, err := base64.RawStdEncoding.DecodeString(blob.Salt)
	if err != nil || len(salt) != vaultSaltLen {
		return nil, ErrVaultInvalid
	}
	nonce, err := base64.RawStdEncoding.DecodeString(blob.Nonce)
	if err != nil {
		return nil, ErrVaultInvalid
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(blob.Ciphertext)
	if err != nil {
		return nil, ErrVaultInvalid
	}
	key := deriveVaultKey(master, salt)
	defer zeroBytes(key)
	plain, err := open(key, nonce, ciphertext)
	if err != nil {
		return nil, ErrVaultInvalid
	}
	return plain, nil
}

type VaultRecord struct {
	Version         int
	Salt            []byte
	CheckNonce      []byte
	CheckCiphertext []byte
}

func deriveVaultKey(master string, salt []byte) []byte {
	return argon2.IDKey([]byte(master), salt, vaultKDFTime, vaultKDFMemoryKiB, vaultKDFThreads, vaultKeyLen)
}

func seal(key, plaintext []byte) (nonce, ciphertext []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return nonce, gcm.Seal(nil, nonce, plaintext, nil), nil
}

func open(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// NewVault creates the encrypted verifier and returns the in-memory key. The
// master password itself is never persisted by the store.
func NewVault(master string) (VaultRecord, []byte, error) {
	salt := make([]byte, vaultSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return VaultRecord{}, nil, err
	}
	key := deriveVaultKey(master, salt)
	nonce, ciphertext, err := seal(key, []byte(vaultCheckTextV2))
	if err != nil {
		zeroBytes(key)
		return VaultRecord{}, nil, err
	}
	return VaultRecord{Version: vaultVersion, Salt: salt, CheckNonce: nonce, CheckCiphertext: ciphertext}, key, nil
}

func UnlockVault(record VaultRecord, master string) ([]byte, error) {
	if record.Version != vaultVersion {
		return nil, ErrVaultUnsupported
	}
	if len(record.Salt) != vaultSaltLen || len(record.CheckNonce) == 0 || len(record.CheckCiphertext) == 0 {
		return nil, ErrVaultInvalid
	}
	key := deriveVaultKey(master, record.Salt)
	plain, err := open(key, record.CheckNonce, record.CheckCiphertext)
	check := []byte(vaultCheckTextV2)
	if err != nil || subtle.ConstantTimeCompare(plain, check) != 1 {
		zeroBytes(plain)
		zeroBytes(key)
		return nil, ErrVaultInvalid
	}
	zeroBytes(plain)
	return key, nil
}

// ChangeVaultMaster rotates the KDF salt and re-encrypts every stored secret
// in one SQLite transaction. Plaintext credentials never leave this method.
func (s *Store) ChangeVaultMaster(ctx context.Context, userID, oldMaster, newMaster string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.Vault(ctx, userID)
	if err != nil {
		return err
	}
	oldKey, err := UnlockVault(record, oldMaster)
	if err != nil {
		return err
	}
	defer zeroBytes(oldKey)
	newRecord, newKey, err := NewVault(newMaster)
	if err != nil {
		return err
	}
	defer zeroBytes(newKey)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT connection_id,nonce,ciphertext FROM connection_secrets WHERE user_id=?`, userID)
	if err != nil {
		return err
	}
	type secret struct {
		id                string
		nonce, ciphertext []byte
	}
	var secrets []secret
	for rows.Next() {
		var x secret
		if err := rows.Scan(&x.id, &x.nonce, &x.ciphertext); err != nil {
			rows.Close()
			return err
		}
		secrets = append(secrets, x)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, x := range secrets {
		plain, err := open(oldKey, x.nonce, x.ciphertext)
		if err != nil {
			zeroBytes(plain)
			return ErrVaultInvalid
		}
		nonce, ciphertext, err := seal(newKey, plain)
		zeroBytes(plain)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE connection_secrets SET nonce=?,ciphertext=?,updated_at=CURRENT_TIMESTAMP WHERE user_id=? AND connection_id=?`, nonce, ciphertext, userID, x.id); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE vaults SET version=?,salt=?,check_nonce=?,check_ciphertext=?,updated_at=CURRENT_TIMESTAMP WHERE user_id=?`, newRecord.Version, newRecord.Salt, newRecord.CheckNonce, newRecord.CheckCiphertext, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Vault(ctx context.Context, userID string) (VaultRecord, error) {
	var v VaultRecord
	err := s.db.QueryRowContext(ctx, `SELECT version,salt,check_nonce,check_ciphertext FROM vaults WHERE user_id=?`, userID).
		Scan(&v.Version, &v.Salt, &v.CheckNonce, &v.CheckCiphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return VaultRecord{}, ErrVaultNotFound
	}
	if err != nil {
		return VaultRecord{}, fmt.Errorf("read vault: %w", err)
	}
	return v, nil
}

func (s *Store) CreateVault(ctx context.Context, userID string, record VaultRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO vaults(user_id,version,salt,check_nonce,check_ciphertext) VALUES(?,?,?,?,?)`, userID, record.Version, record.Salt, record.CheckNonce, record.CheckCiphertext)
	if err != nil {
		if bytes.Contains([]byte(err.Error()), []byte("UNIQUE constraint failed")) {
			return ErrVaultExists
		}
		return fmt.Errorf("create vault: %w", err)
	}
	return nil
}

type VaultResetInfo struct {
	Profiles int
	Secrets  int
}

func (s *Store) VaultResetInfo(ctx context.Context, userID string) (VaultResetInfo, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM app_users WHERE id=?)`, userID).Scan(&exists); err != nil {
		return VaultResetInfo{}, fmt.Errorf("check app user: %w", err)
	}
	if !exists {
		return VaultResetInfo{}, ErrUserNotFound
	}
	var info VaultResetInfo
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_profiles WHERE user_id=?`, userID).Scan(&info.Profiles); err != nil {
		return VaultResetInfo{}, fmt.Errorf("count connection profiles: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_secrets WHERE user_id=?`, userID).Scan(&info.Secrets); err != nil {
		return VaultResetInfo{}, fmt.Errorf("count vault secrets: %w", err)
	}
	return info, nil
}

// ResetVault removes all vault material and creates a fresh locked vault with
// newMaster. Connection profiles and their non-secret metadata are preserved.
// The operation is atomic so a failed reset cannot leave a half-written vault.
func (s *Store) ResetVault(ctx context.Context, userID, newMaster string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	newRecord, newKey, err := NewVault(newMaster)
	if err != nil {
		return err
	}
	defer zeroBytes(newKey)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin vault reset: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM connection_secrets WHERE user_id=?`, userID); err != nil {
		return fmt.Errorf("delete vault secrets: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM vaults WHERE user_id=?`, userID); err != nil {
		return fmt.Errorf("delete vault: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO vaults(user_id,version,salt,check_nonce,check_ciphertext) VALUES(?,?,?,?,?)`, userID, newRecord.Version, newRecord.Salt, newRecord.CheckNonce, newRecord.CheckCiphertext); err != nil {
		return fmt.Errorf("create reset vault: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit vault reset: %w", err)
	}

	// Reclaim SQLite pages where possible. Reset is already complete if either
	// cleanup pragma is unsupported by a platform-specific SQLite build.
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return &VaultResetCleanupError{Err: err}
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
		return &VaultResetCleanupError{Err: err}
	}
	return nil
}

func (s *Store) SetConnectionSecret(ctx context.Context, userID, connectionID string, key []byte, password string) error {
	nonce, ciphertext, err := seal(key, []byte(password))
	if err != nil {
		return fmt.Errorf("encrypt connection secret: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO connection_secrets(user_id,connection_id,nonce,ciphertext)
		VALUES(?,?,?,?)
		ON CONFLICT(connection_id) DO UPDATE SET user_id=excluded.user_id,nonce=excluded.nonce,ciphertext=excluded.ciphertext,updated_at=CURRENT_TIMESTAMP`,
		userID, connectionID, nonce, ciphertext)
	if err != nil {
		return fmt.Errorf("save connection secret: %w", err)
	}
	return nil
}

func (s *Store) CopyConnectionSecret(ctx context.Context, userID, sourceID, targetID string) (bool, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO connection_secrets(user_id,connection_id,nonce,ciphertext)
		SELECT user_id,?,nonce,ciphertext FROM connection_secrets WHERE user_id=? AND connection_id=?
		ON CONFLICT(connection_id) DO UPDATE SET user_id=excluded.user_id,nonce=excluded.nonce,ciphertext=excluded.ciphertext,updated_at=CURRENT_TIMESTAMP`,
		targetID, userID, sourceID)
	if err != nil {
		return false, err
	}
	var exists bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM connection_secrets WHERE user_id=? AND connection_id=?)`, userID, targetID).Scan(&exists)
	return exists, err
}

func (s *Store) DeleteConnectionSecret(ctx context.Context, userID, connectionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM connection_secrets WHERE user_id=? AND connection_id=?`, userID, connectionID)
	return err
}

func (s *Store) ConnectionSecret(ctx context.Context, userID, connectionID string, key []byte) (string, bool, error) {
	var nonce, ciphertext []byte
	err := s.db.QueryRowContext(ctx, `SELECT nonce,ciphertext FROM connection_secrets WHERE user_id=? AND connection_id=?`, userID, connectionID).Scan(&nonce, &ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	plain, err := open(key, nonce, ciphertext)
	if err != nil {
		return "", false, fmt.Errorf("decrypt connection secret: %w", err)
	}
	return string(plain), true, nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
