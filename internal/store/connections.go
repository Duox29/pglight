package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
)

type ConnectionProfile struct {
	ID, Name, Host                                              string
	Port                                                        int
	Username, DBName, SSLMode, CreatedAt, UpdatedAt, LastUsedAt string
	HasPassword                                                 bool
	FolderID, Environment, Color, Description                   string
	Favorite, Default                                           bool
	Tags                                                        []string
	Options                                                     ConnectionOptions
}

type ConnectionOptions struct {
	ConnectTimeout  int    `json:"connect_timeout"`
	Keepalive       int    `json:"keepalive"`
	ApplicationName string `json:"application_name"`
	SearchPath      string `json:"search_path"`
	SSLRootCert     string `json:"sslrootcert"`
	SSLCert         string `json:"sslcert"`
	SSLKey          string `json:"sslkey"`
	UnixSocket      string `json:"unix_socket"`
	SSHEnabled      bool   `json:"ssh_enabled,omitempty"`
	SSHHost         string `json:"ssh_host,omitempty"`
	SSHPort         int    `json:"ssh_port,omitempty"`
	SSHUser         string `json:"ssh_user,omitempty"`
	SSHAuthMethod   string `json:"ssh_auth_method,omitempty"`
	SSHHostKey      string `json:"ssh_host_key,omitempty"`
}

type SSHSecretValues struct {
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

type ConnectionImportItem struct {
	Name        string
	Host        string
	Port        int
	User        string
	DBName      string
	SSLMode     string
	Password    string
	FolderID    string
	Environment string
	Color       string
	Description string
	Favorite    bool
	Default     bool
	Tags        []string
	Options     ConnectionOptions
	SSHSecrets  SSHSecretValues
}

type ConnectionSave struct {
	ID            string
	Name          string
	Host          string
	Port          int
	User          string
	DBName        string
	SSLMode       string
	Password      string
	SavePassword  bool
	ClearPassword bool
	DuplicateFrom string
	FolderID      string
	Environment   string
	Color         string
	Description   string
	Favorite      bool
	Default       bool
	Tags          []string
	Options       ConnectionOptions
	SSHSecrets    SSHSecretValues
}

func (s *Store) ListConnections(ctx context.Context, userID string) ([]ConnectionProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.name,c.host,c.port,c.username,c.dbname,c.sslmode,c.created_at,c.updated_at,COALESCE(c.last_used_at,''),EXISTS(SELECT 1 FROM connection_secrets x WHERE x.user_id=c.user_id AND x.connection_id=c.id),COALESCE(c.folder_id,''),COALESCE(c.environment,''),COALESCE(c.color,''),COALESCE(c.description,''),c.is_favorite,c.is_default FROM connection_profiles c WHERE c.user_id=? ORDER BY c.is_default DESC,c.is_favorite DESC,COALESCE(c.last_used_at,c.updated_at) DESC,c.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	defer rows.Close()
	out := []ConnectionProfile{}
	for rows.Next() {
		var x ConnectionProfile
		var favorite, def int
		if err := rows.Scan(&x.ID, &x.Name, &x.Host, &x.Port, &x.Username, &x.DBName, &x.SSLMode, &x.CreatedAt, &x.UpdatedAt, &x.LastUsedAt, &x.HasPassword, &x.FolderID, &x.Environment, &x.Color, &x.Description, &favorite, &def); err != nil {
			return nil, err
		}
		x.Favorite, x.Default = favorite != 0, def != 0
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.loadConnectionTags(ctx, userID, &out[i]); err != nil {
			return nil, err
		}
		if err := s.loadConnectionOptions(ctx, userID, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) GetConnection(ctx context.Context, userID, id string) (ConnectionProfile, error) {
	var x ConnectionProfile
	var favorite, def int
	err := s.db.QueryRowContext(ctx, `SELECT c.id,c.name,c.host,c.port,c.username,c.dbname,c.sslmode,c.created_at,c.updated_at,COALESCE(c.last_used_at,''),EXISTS(SELECT 1 FROM connection_secrets x WHERE x.user_id=c.user_id AND x.connection_id=c.id),COALESCE(c.folder_id,''),COALESCE(c.environment,''),COALESCE(c.color,''),COALESCE(c.description,''),c.is_favorite,c.is_default FROM connection_profiles c WHERE c.user_id=? AND c.id=?`, userID, id).
		Scan(&x.ID, &x.Name, &x.Host, &x.Port, &x.Username, &x.DBName, &x.SSLMode, &x.CreatedAt, &x.UpdatedAt, &x.LastUsedAt, &x.HasPassword, &x.FolderID, &x.Environment, &x.Color, &x.Description, &favorite, &def)
	x.Favorite, x.Default = favorite != 0, def != 0
	if err == nil {
		err = s.loadConnectionTags(ctx, userID, &x)
	}
	if err == nil {
		err = s.loadConnectionOptions(ctx, userID, &x)
	}
	return x, err
}
func (s *Store) UpsertConnection(ctx context.Context, userID, name, host string, port int, username, dbname, sslmode string) (ConnectionProfile, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `INSERT INTO connection_profiles(id,user_id,name,host,port,username,dbname,sslmode) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(user_id,name) DO UPDATE SET host=excluded.host,port=excluded.port,username=excluded.username,dbname=excluded.dbname,sslmode=excluded.sslmode,updated_at=CURRENT_TIMESTAMP`, id, userID, name, host, port, username, dbname, sslmode)
	if err != nil {
		return ConnectionProfile{}, fmt.Errorf("upsert connection: %w", err)
	}
	var x ConnectionProfile
	var foundID string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM connection_profiles WHERE user_id=? AND name=?`, userID, name).Scan(&foundID)
	if err == nil {
		x, err = s.GetConnection(ctx, userID, foundID)
	}
	return x, err
}

// SaveConnectionProfile applies profile, secret, metadata, and options in one
// SQLite transaction so a later validation/encryption failure cannot leave a
// partially updated profile behind.
func (s *Store) SaveConnectionProfile(ctx context.Context, userID string, item ConnectionSave, key []byte) (ConnectionProfile, error) {
	if item.Options.SSHEnabled && item.Options.SSHPort == 0 {
		item.Options.SSHPort = 22
	}
	if err := validateConnectionOptions(item.Options); err != nil {
		return ConnectionProfile{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ConnectionProfile{}, err
	}
	defer tx.Rollback()
	id := strings.TrimSpace(item.ID)
	if id != "" {
		res, err := tx.ExecContext(ctx, `UPDATE connection_profiles SET name=?,host=?,port=?,username=?,dbname=?,sslmode=?,updated_at=CURRENT_TIMESTAMP WHERE user_id=? AND id=?`, item.Name, item.Host, item.Port, item.User, item.DBName, item.SSLMode, userID, id)
		if err != nil {
			return ConnectionProfile{}, fmt.Errorf("update connection: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil || n == 0 {
			if err != nil {
				return ConnectionProfile{}, err
			}
			return ConnectionProfile{}, sql.ErrNoRows
		}
	} else {
		id = uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO connection_profiles(id,user_id,name,host,port,username,dbname,sslmode) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(user_id,name) DO UPDATE SET host=excluded.host,port=excluded.port,username=excluded.username,dbname=excluded.dbname,sslmode=excluded.sslmode,updated_at=CURRENT_TIMESTAMP`, id, userID, item.Name, item.Host, item.Port, item.User, item.DBName, item.SSLMode); err != nil {
			return ConnectionProfile{}, fmt.Errorf("upsert connection: %w", err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM connection_profiles WHERE user_id=? AND name=?`, userID, item.Name).Scan(&id); err != nil {
			return ConnectionProfile{}, err
		}
	}
	if item.ClearPassword {
		if _, err := tx.ExecContext(ctx, `DELETE FROM connection_secrets WHERE user_id=? AND connection_id=?`, userID, id); err != nil {
			return ConnectionProfile{}, err
		}
	}
	if source := strings.TrimSpace(item.DuplicateFrom); source != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO connection_secrets(user_id,connection_id,nonce,ciphertext) SELECT user_id,?,nonce,ciphertext FROM connection_secrets WHERE user_id=? AND connection_id=? ON CONFLICT(connection_id) DO UPDATE SET user_id=excluded.user_id,nonce=excluded.nonce,ciphertext=excluded.ciphertext,updated_at=CURRENT_TIMESTAMP`, id, userID, source); err != nil {
			return ConnectionProfile{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO connection_ssh_secrets(user_id,connection_id,nonce,ciphertext) SELECT user_id,?,nonce,ciphertext FROM connection_ssh_secrets WHERE user_id=? AND connection_id=? ON CONFLICT(connection_id) DO UPDATE SET user_id=excluded.user_id,nonce=excluded.nonce,ciphertext=excluded.ciphertext,updated_at=CURRENT_TIMESTAMP`, id, userID, source); err != nil {
			return ConnectionProfile{}, err
		}
		var sshEnabled int
		var sshPort int
		err := tx.QueryRowContext(ctx, `SELECT connect_timeout,keepalive,application_name,search_path,sslrootcert,sslcert,sslkey,unix_socket,ssh_enabled,ssh_host,ssh_port,ssh_user,ssh_auth_method,ssh_host_key FROM connection_options WHERE user_id=? AND profile_id=?`, userID, source).Scan(&item.Options.ConnectTimeout, &item.Options.Keepalive, &item.Options.ApplicationName, &item.Options.SearchPath, &item.Options.SSLRootCert, &item.Options.SSLCert, &item.Options.SSLKey, &item.Options.UnixSocket, &sshEnabled, &item.Options.SSHHost, &sshPort, &item.Options.SSHUser, &item.Options.SSHAuthMethod, &item.Options.SSHHostKey)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ConnectionProfile{}, err
		}
		item.Options.SSHEnabled, item.Options.SSHPort = sshEnabled != 0, sshPort
	}
	if item.SavePassword {
		if len(key) == 0 {
			return ConnectionProfile{}, errors.New("vault is locked; unlock it before saving a password")
		}
		nonce, ciphertext, err := seal(key, []byte(item.Password))
		if err != nil {
			return ConnectionProfile{}, fmt.Errorf("encrypt connection secret: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO connection_secrets(user_id,connection_id,nonce,ciphertext) VALUES(?,?,?,?) ON CONFLICT(connection_id) DO UPDATE SET user_id=excluded.user_id,nonce=excluded.nonce,ciphertext=excluded.ciphertext,updated_at=CURRENT_TIMESTAMP`, userID, id, nonce, ciphertext); err != nil {
			return ConnectionProfile{}, err
		}
	}
	if item.SSHSecrets.Password != "" || item.SSHSecrets.PrivateKey != "" || item.SSHSecrets.Passphrase != "" {
		if len(key) == 0 {
			return ConnectionProfile{}, errors.New("vault is locked; unlock it before saving SSH credentials")
		}
		payload, err := json.Marshal(item.SSHSecrets)
		if err != nil {
			return ConnectionProfile{}, err
		}
		nonce, ciphertext, err := seal(key, payload)
		zeroBytes(payload)
		if err != nil {
			return ConnectionProfile{}, fmt.Errorf("encrypt SSH credentials: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO connection_ssh_secrets(user_id,connection_id,nonce,ciphertext) VALUES(?,?,?,?) ON CONFLICT(connection_id) DO UPDATE SET user_id=excluded.user_id,nonce=excluded.nonce,ciphertext=excluded.ciphertext,updated_at=CURRENT_TIMESTAMP`, userID, id, nonce, ciphertext); err != nil {
			return ConnectionProfile{}, err
		}
	}
	if err := updateConnectionMetadataTx(ctx, tx, userID, id, item.FolderID, item.Environment, item.Color, item.Description, item.Favorite, item.Default, item.Tags); err != nil {
		return ConnectionProfile{}, err
	}
	if _, err := tx.ExecContext(ctx, connectionOptionsSQL, connectionOptionArgs(userID, id, item.Options)...); err != nil {
		return ConnectionProfile{}, err
	}
	if err := tx.Commit(); err != nil {
		return ConnectionProfile{}, err
	}
	return s.GetConnection(ctx, userID, id)
}

// UpdateConnectionMetadata updates the profile-manager metadata in one transaction.
func (s *Store) UpdateConnectionMetadata(ctx context.Context, userID, id, folderID, environment, color, description string, favorite, makeDefault bool, tags []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := updateConnectionMetadataTx(ctx, tx, userID, id, folderID, environment, color, description, favorite, makeDefault, tags); err != nil {
		return err
	}
	return tx.Commit()
}

func updateConnectionMetadataTx(ctx context.Context, tx *sql.Tx, userID, id, folderID, environment, color, description string, favorite, makeDefault bool, tags []string) error {
	if strings.TrimSpace(folderID) != "" {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM connection_folders WHERE user_id=? AND id=?)`, userID, strings.TrimSpace(folderID)).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("connection folder not found")
		}
	}
	if makeDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE connection_profiles SET is_default=0 WHERE user_id=?`, userID); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE connection_profiles SET folder_id=?,environment=?,color=?,description=?,is_favorite=?,is_default=?,updated_at=CURRENT_TIMESTAMP WHERE user_id=? AND id=?`, nullableText(folderID), strings.TrimSpace(environment), strings.TrimSpace(color), strings.TrimSpace(description), boolInt(favorite), boolInt(makeDefault), userID, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		if err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM connection_tags WHERE user_id=? AND profile_id=?`, userID, id); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		if len([]rune(tag)) > 40 {
			return fmt.Errorf("tag is too long")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO connection_tags(user_id,profile_id,tag) VALUES(?,?,?)`, userID, id, tag); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadConnectionTags(ctx context.Context, userID string, x *ConnectionProfile) error {
	rows, err := s.db.QueryContext(ctx, `SELECT tag FROM connection_tags WHERE user_id=? AND profile_id=? ORDER BY tag`, userID, x.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return err
		}
		x.Tags = append(x.Tags, tag)
	}
	return rows.Err()
}

func (s *Store) loadConnectionOptions(ctx context.Context, userID string, x *ConnectionProfile) error {
	var o ConnectionOptions
	err := s.db.QueryRowContext(ctx, `SELECT connect_timeout,keepalive,application_name,search_path,sslrootcert,sslcert,sslkey,unix_socket,ssh_enabled,ssh_host,ssh_port,ssh_user,ssh_auth_method,ssh_host_key FROM connection_options WHERE user_id=? AND profile_id=?`, userID, x.ID).Scan(&o.ConnectTimeout, &o.Keepalive, &o.ApplicationName, &o.SearchPath, &o.SSLRootCert, &o.SSLCert, &o.SSLKey, &o.UnixSocket, &o.SSHEnabled, &o.SSHHost, &o.SSHPort, &o.SSHUser, &o.SSHAuthMethod, &o.SSHHostKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	x.Options = o
	return nil
}

func (s *Store) UpdateConnectionOptions(ctx context.Context, userID, id string, o ConnectionOptions) error {
	if o.SSHEnabled && o.SSHPort == 0 {
		o.SSHPort = 22
	}
	if o.ConnectTimeout < 0 || o.ConnectTimeout > 300 || o.Keepalive < 0 || o.Keepalive > 86400 {
		return fmt.Errorf("invalid connection timeout or keepalive")
	}
	_, err := s.db.ExecContext(ctx, connectionOptionsSQL, connectionOptionArgs(userID, id, o)...)
	return err
}

const connectionOptionsSQL = `INSERT INTO connection_options(user_id,profile_id,connect_timeout,keepalive,application_name,search_path,sslrootcert,sslcert,sslkey,unix_socket,ssh_enabled,ssh_host,ssh_port,ssh_user,ssh_auth_method,ssh_host_key) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(profile_id) DO UPDATE SET user_id=excluded.user_id,connect_timeout=excluded.connect_timeout,keepalive=excluded.keepalive,application_name=excluded.application_name,search_path=excluded.search_path,sslrootcert=excluded.sslrootcert,sslcert=excluded.sslcert,sslkey=excluded.sslkey,unix_socket=excluded.unix_socket,ssh_enabled=excluded.ssh_enabled,ssh_host=excluded.ssh_host,ssh_port=excluded.ssh_port,ssh_user=excluded.ssh_user,ssh_auth_method=excluded.ssh_auth_method,ssh_host_key=excluded.ssh_host_key`

func connectionOptionArgs(userID, profileID string, o ConnectionOptions) []any {
	return []any{userID, profileID, o.ConnectTimeout, o.Keepalive, strings.TrimSpace(o.ApplicationName), strings.TrimSpace(o.SearchPath), strings.TrimSpace(o.SSLRootCert), strings.TrimSpace(o.SSLCert), strings.TrimSpace(o.SSLKey), strings.TrimSpace(o.UnixSocket), boolInt(o.SSHEnabled), strings.TrimSpace(o.SSHHost), o.SSHPort, strings.TrimSpace(o.SSHUser), strings.TrimSpace(o.SSHAuthMethod), strings.TrimSpace(o.SSHHostKey)}
}

func validateConnectionOptions(o ConnectionOptions) error {
	if o.ConnectTimeout < 0 || o.ConnectTimeout > 300 || o.Keepalive < 0 || o.Keepalive > 86400 {
		return fmt.Errorf("invalid connection timeout or keepalive")
	}
	if o.SSHEnabled {
		if strings.TrimSpace(o.UnixSocket) != "" {
			return fmt.Errorf("SSH tunnel cannot be combined with a PostgreSQL Unix socket")
		}
		if strings.TrimSpace(o.SSHHost) == "" || strings.TrimSpace(o.SSHUser) == "" {
			return fmt.Errorf("SSH host and username are required")
		}
		if o.SSHPort < 0 || o.SSHPort > 65535 {
			return fmt.Errorf("invalid SSH port")
		}
		if o.SSHPort == 0 {
			o.SSHPort = 22
		}
		if o.SSHAuthMethod != "password" && o.SSHAuthMethod != "private_key" {
			return fmt.Errorf("SSH authentication must be password or private_key")
		}
		if key := strings.TrimSpace(o.SSHHostKey); key != "" && !strings.HasPrefix(key, "SHA256:") {
			return fmt.Errorf("SSH host key must be a SHA256 fingerprint")
		}
	}
	return nil
}

// ImportConnectionProfiles persists the complete import as one SQLite
// transaction. Any invalid profile, metadata, option, or secret rolls back
// every profile in the batch.
func (s *Store) ImportConnectionProfiles(ctx context.Context, userID string, items []ConnectionImportItem, key []byte) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, item := range items {
		if item.Options.SSHEnabled && item.Options.SSHPort == 0 {
			item.Options.SSHPort = 22
		}
		if err := validateConnectionOptions(item.Options); err != nil {
			return 0, err
		}
		id := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO connection_profiles(id,user_id,name,host,port,username,dbname,sslmode) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(user_id,name) DO UPDATE SET host=excluded.host,port=excluded.port,username=excluded.username,dbname=excluded.dbname,sslmode=excluded.sslmode,updated_at=CURRENT_TIMESTAMP`, id, userID, item.Name, item.Host, item.Port, item.User, item.DBName, item.SSLMode); err != nil {
			return 0, fmt.Errorf("upsert connection: %w", err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM connection_profiles WHERE user_id=? AND name=?`, userID, item.Name).Scan(&id); err != nil {
			return 0, err
		}
		if err := updateConnectionMetadataTx(ctx, tx, userID, id, item.FolderID, item.Environment, item.Color, item.Description, item.Favorite, item.Default, item.Tags); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, connectionOptionsSQL, connectionOptionArgs(userID, id, item.Options)...); err != nil {
			return 0, err
		}
		if item.Password != "" {
			if len(key) == 0 {
				return 0, errors.New("vault is locked; unlock it before importing passwords")
			}
			nonce, ciphertext, err := seal(key, []byte(item.Password))
			if err != nil {
				return 0, fmt.Errorf("encrypt connection secret: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO connection_secrets(user_id,connection_id,nonce,ciphertext) VALUES(?,?,?,?) ON CONFLICT(connection_id) DO UPDATE SET user_id=excluded.user_id,nonce=excluded.nonce,ciphertext=excluded.ciphertext,updated_at=CURRENT_TIMESTAMP`, userID, id, nonce, ciphertext); err != nil {
				return 0, err
			}
		}
		if item.SSHSecrets.Password != "" || item.SSHSecrets.PrivateKey != "" || item.SSHSecrets.Passphrase != "" {
			if len(key) == 0 {
				return 0, errors.New("vault is locked; unlock it before importing SSH credentials")
			}
			payload, err := json.Marshal(item.SSHSecrets)
			if err != nil {
				return 0, err
			}
			nonce, ciphertext, err := seal(key, payload)
			zeroBytes(payload)
			if err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO connection_ssh_secrets(user_id,connection_id,nonce,ciphertext) VALUES(?,?,?,?) ON CONFLICT(connection_id) DO UPDATE SET user_id=excluded.user_id,nonce=excluded.nonce,ciphertext=excluded.ciphertext,updated_at=CURRENT_TIMESTAMP`, userID, id, nonce, ciphertext); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(items), nil
}

func nullableText(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return strings.TrimSpace(v)
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Store) ListConnectionFolders(ctx context.Context, userID string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,COALESCE(parent_id,''),created_at,updated_at FROM connection_folders WHERE user_id=? ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, name, parent, created, updated string
		if err := rows.Scan(&id, &name, &parent, &created, &updated); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "name": name, "parent_id": parent, "created_at": created, "updated_at": updated})
	}
	return out, rows.Err()
}

func (s *Store) UpsertConnectionFolder(ctx context.Context, userID, id, name, parentID string) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("folder name is required")
	}
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO connection_folders(id,user_id,name,parent_id) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,parent_id=excluded.parent_id,updated_at=CURRENT_TIMESTAMP`, id, userID, name, nullableText(parentID))
	if err != nil {
		return nil, err
	}
	var created, updated, parent string
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(parent_id,''),created_at,updated_at FROM connection_folders WHERE user_id=? AND id=?`, userID, id).Scan(&parent, &created, &updated)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "name": name, "parent_id": parent, "created_at": created, "updated_at": updated}, nil
}

func (s *Store) DeleteConnectionFolder(ctx context.Context, userID, id string) (bool, error) {
	r, err := s.db.ExecContext(ctx, `DELETE FROM connection_folders WHERE user_id=? AND id=?`, userID, id)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

func (s *Store) UpdateConnection(ctx context.Context, userID, id, name, host string, port int, username, dbname, sslmode string) (ConnectionProfile, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE connection_profiles SET name=?,host=?,port=?,username=?,dbname=?,sslmode=?,updated_at=CURRENT_TIMESTAMP WHERE user_id=? AND id=?`, name, host, port, username, dbname, sslmode, userID, id)
	if err != nil {
		return ConnectionProfile{}, fmt.Errorf("update connection: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		if err != nil {
			return ConnectionProfile{}, err
		}
		return ConnectionProfile{}, sql.ErrNoRows
	}
	return s.GetConnection(ctx, userID, id)
}

func (s *Store) MarkConnectionUsed(ctx context.Context, userID, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE connection_profiles SET last_used_at=CURRENT_TIMESTAMP WHERE user_id=? AND id=?`, userID, id)
	return err
}
func (s *Store) DeleteConnectionByID(ctx context.Context, userID, id string) (bool, error) {
	r, err := s.db.ExecContext(ctx, `DELETE FROM connection_profiles WHERE user_id=? AND id=?`, userID, id)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}
