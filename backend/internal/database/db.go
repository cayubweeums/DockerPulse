package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func InitDB(dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "dockpulse.db")
	conn, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	conn.SetMaxOpenConns(1) // SQLite works best with 1 open writer in WAL mode

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'admin',
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS hosts (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		driver TEXT NOT NULL,
		address TEXT NOT NULL,
		port INTEGER NOT NULL DEFAULT 0,
		auth_token TEXT,
		ssh_user TEXT,
		ssh_key TEXT,
		base_dir TEXT NOT NULL DEFAULT '~/docker',
		status TEXT NOT NULL DEFAULT 'unknown',
		last_seen DATETIME NOT NULL,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS stacks (
		id TEXT PRIMARY KEY,
		host_id TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		path TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'unknown',
		auto_update BOOLEAN NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		UNIQUE(host_id, path)
	);

	CREATE TABLE IF NOT EXISTS stack_revisions (
		id TEXT PRIMARY KEY,
		stack_id TEXT NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
		revision_num INTEGER NOT NULL,
		compose_content TEXT NOT NULL,
		env_content TEXT NOT NULL,
		created_by TEXT NOT NULL,
		note TEXT,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS update_checks (
		id TEXT PRIMARY KEY,
		host_id TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		stack_id TEXT NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
		service_name TEXT NOT NULL,
		image_name TEXT NOT NULL,
		current_digest TEXT NOT NULL,
		remote_digest TEXT NOT NULL,
		has_update BOOLEAN NOT NULL DEFAULT 0,
		last_checked_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS audit_logs (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		username TEXT,
		action TEXT NOT NULL,
		target TEXT,
		details TEXT,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS notifications (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		message TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'info',
		host_id TEXT,
		read BOOLEAN NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS notification_configs (
		id TEXT PRIMARY KEY,
		enabled BOOLEAN NOT NULL DEFAULT 0,
		config_json TEXT NOT NULL DEFAULT '{}',
		status TEXT NOT NULL DEFAULT 'unconfigured',
		last_error TEXT NOT NULL DEFAULT '',
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS scheduler_config (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		enabled BOOLEAN NOT NULL DEFAULT 1,
		interval_minutes INTEGER NOT NULL DEFAULT 360,
		last_run DATETIME,
		next_run DATETIME
	);
	`
	_, err := db.conn.Exec(schema)
	if err != nil {
		return err
	}

	// Schema migrations for users table
	_, _ = db.conn.Exec("ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''")
	_, _ = db.conn.Exec("ALTER TABLE users ADD COLUMN avatar TEXT NOT NULL DEFAULT ''")
	_, _ = db.conn.Exec("ALTER TABLE users ADD COLUMN theme TEXT NOT NULL DEFAULT 'dark'")

	// Seed notification configs if missing
	_, _ = db.conn.Exec(`
		INSERT OR IGNORE INTO notification_configs (id, enabled, config_json, status, last_error, updated_at)
		VALUES
			('ntfy', 0, '{}', 'unconfigured', '', CURRENT_TIMESTAMP),
			('discord', 0, '{}', 'unconfigured', '', CURRENT_TIMESTAMP),
			('signal', 0, '{}', 'unconfigured', '', CURRENT_TIMESTAMP);
	`)

	// Seed scheduler config if missing
	_, _ = db.conn.Exec(`
		INSERT OR IGNORE INTO scheduler_config (id, enabled, interval_minutes, last_run, next_run)
		VALUES (1, 1, 360, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
	`)

	// Deduplicate any duplicate stacks on the same host (e.g. from previous container vs host path scans)
	// Prefer host path (not starting with /root/docker/ when alternative exists) or newer updated_at
	_, _ = db.conn.Exec(`
		DELETE FROM stacks
		WHERE id IN (
			SELECT s1.id FROM stacks s1
			JOIN stacks s2 ON s1.host_id = s2.host_id AND s1.name = s2.name AND s1.id != s2.id
			WHERE (s1.path LIKE '/root/docker/%' AND s2.path NOT LIKE '/root/docker/%')
			   OR (
			        NOT (s1.path LIKE '/root/docker/%' AND s2.path NOT LIKE '/root/docker/%')
			        AND NOT (s2.path LIKE '/root/docker/%' AND s1.path NOT LIKE '/root/docker/%')
			        AND (s1.updated_at < s2.updated_at OR (s1.updated_at = s2.updated_at AND s1.id < s2.id))
			   )
		)
	`)

	return nil
}

// User operations
func (db *DB) CountUsers() (int, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

func (db *DB) CreateUser(u *User) error {
	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	if u.DisplayName == "" {
		u.DisplayName = u.Username
	}
	if u.Theme == "" {
		u.Theme = "dark"
	}
	u.CreatedAt = time.Now().UTC()
	_, err := db.conn.Exec(
		"INSERT INTO users (id, username, password_hash, role, display_name, avatar, theme, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		u.ID, u.Username, u.PasswordHash, u.Role, u.DisplayName, u.Avatar, u.Theme, u.CreatedAt,
	)
	return err
}

func (db *DB) GetUserByUsername(username string) (*User, error) {
	u := &User{}
	var displayName, avatar, theme sql.NullString
	err := db.conn.QueryRow(
		"SELECT id, username, password_hash, role, display_name, avatar, theme, created_at FROM users WHERE username = ?",
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &displayName, &avatar, &theme, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	if displayName.Valid && displayName.String != "" {
		u.DisplayName = displayName.String
	} else {
		u.DisplayName = u.Username
	}
	if avatar.Valid {
		u.Avatar = avatar.String
	}
	if theme.Valid && theme.String != "" {
		u.Theme = theme.String
	} else {
		u.Theme = "dark"
	}
	return u, nil
}

func (db *DB) GetUserByID(id string) (*User, error) {
	u := &User{}
	var displayName, avatar, theme sql.NullString
	err := db.conn.QueryRow(
		"SELECT id, username, password_hash, role, display_name, avatar, theme, created_at FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &displayName, &avatar, &theme, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	if displayName.Valid && displayName.String != "" {
		u.DisplayName = displayName.String
	} else {
		u.DisplayName = u.Username
	}
	if avatar.Valid {
		u.Avatar = avatar.String
	}
	if theme.Valid && theme.String != "" {
		u.Theme = theme.String
	} else {
		u.Theme = "dark"
	}
	return u, nil
}

func (db *DB) ListUsers() ([]User, error) {
	rows, err := db.conn.Query("SELECT id, username, password_hash, role, display_name, avatar, theme, created_at FROM users ORDER BY created_at ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var displayName, avatar, theme sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &displayName, &avatar, &theme, &u.CreatedAt); err != nil {
			return nil, err
		}
		if displayName.Valid && displayName.String != "" {
			u.DisplayName = displayName.String
		} else {
			u.DisplayName = u.Username
		}
		if avatar.Valid {
			u.Avatar = avatar.String
		}
		if theme.Valid && theme.String != "" {
			u.Theme = theme.String
		} else {
			u.Theme = "dark"
		}
		users = append(users, u)
	}
	return users, nil
}

func (db *DB) UpdateUserProfile(id string, displayName, username, avatar, theme string) error {
	_, err := db.conn.Exec(
		"UPDATE users SET display_name = ?, username = ?, avatar = ?, theme = ? WHERE id = ?",
		displayName, username, avatar, theme, id,
	)
	return err
}

func (db *DB) UpdateUserPassword(id string, passwordHash string) error {
	_, err := db.conn.Exec("UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, id)
	return err
}

func (db *DB) UpdateUserRole(id string, role UserRole) error {
	_, err := db.conn.Exec("UPDATE users SET role = ? WHERE id = ?", role, id)
	return err
}

func (db *DB) DeleteUser(id string) error {
	_, err := db.conn.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

// Host operations
func (db *DB) ListHosts() ([]Host, error) {
	rows, err := db.conn.Query("SELECT id, name, driver, address, port, auth_token, ssh_user, base_dir, status, last_seen, created_at FROM hosts ORDER BY name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hosts := make([]Host, 0)
	for rows.Next() {
		var h Host
		var authToken, sshUser sql.NullString
		if err := rows.Scan(&h.ID, &h.Name, &h.Driver, &h.Address, &h.Port, &authToken, &sshUser, &h.BaseDir, &h.Status, &h.LastSeen, &h.CreatedAt); err != nil {
			return nil, err
		}
		if authToken.Valid {
			h.AuthToken = authToken.String
		}
		if sshUser.Valid {
			h.SSHUser = sshUser.String
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func (db *DB) GetHost(id string) (*Host, error) {
	h := &Host{}
	var authToken, sshUser, sshKey sql.NullString
	err := db.conn.QueryRow(
		"SELECT id, name, driver, address, port, auth_token, ssh_user, ssh_key, base_dir, status, last_seen, created_at FROM hosts WHERE id = ?",
		id,
	).Scan(&h.ID, &h.Name, &h.Driver, &h.Address, &h.Port, &authToken, &sshUser, &sshKey, &h.BaseDir, &h.Status, &h.LastSeen, &h.CreatedAt)
	if err != nil {
		return nil, err
	}
	if authToken.Valid {
		h.AuthToken = authToken.String
	}
	if sshUser.Valid {
		h.SSHUser = sshUser.String
	}
	if sshKey.Valid {
		h.SSHKey = sshKey.String
	}
	return h, nil
}

func (db *DB) CreateHost(h *Host) error {
	if h.ID == "" {
		h.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	h.CreatedAt = now
	h.LastSeen = now
	_, err := db.conn.Exec(
		"INSERT INTO hosts (id, name, driver, address, port, auth_token, ssh_user, ssh_key, base_dir, status, last_seen, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		h.ID, h.Name, h.Driver, h.Address, h.Port, h.AuthToken, h.SSHUser, h.SSHKey, h.BaseDir, h.Status, h.LastSeen, h.CreatedAt,
	)
	return err
}

func (db *DB) UpdateHost(h *Host) error {
	_, err := db.conn.Exec(
		"UPDATE hosts SET name = ?, base_dir = ?, address = ?, port = ?, auth_token = ?, ssh_user = ?, ssh_key = ? WHERE id = ?",
		h.Name, h.BaseDir, h.Address, h.Port, h.AuthToken, h.SSHUser, h.SSHKey, h.ID,
	)
	return err
}

func (db *DB) UpdateHostStatus(id, status string) error {
	_, err := db.conn.Exec("UPDATE hosts SET status = ?, last_seen = ? WHERE id = ?", status, time.Now().UTC(), id)
	return err
}

func (db *DB) DeleteHost(id string) error {
	_, err := db.conn.Exec("DELETE FROM hosts WHERE id = ?", id)
	return err
}

// Stack operations
func (db *DB) ListStacks(hostID string) ([]Stack, error) {
	query := "SELECT id, host_id, name, path, status, auto_update, created_at, updated_at FROM stacks"
	var rows *sql.Rows
	var err error
	if hostID != "" {
		query += " WHERE host_id = ? ORDER BY name ASC"
		rows, err = db.conn.Query(query, hostID)
	} else {
		query += " ORDER BY name ASC"
		rows, err = db.conn.Query(query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stacks := make([]Stack, 0)
	for rows.Next() {
		var s Stack
		if err := rows.Scan(&s.ID, &s.HostID, &s.Name, &s.Path, &s.Status, &s.AutoUpdate, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		stacks = append(stacks, s)
	}
	return stacks, nil
}

func (db *DB) GetStack(id string) (*Stack, error) {
	s := &Stack{}
	err := db.conn.QueryRow(
		"SELECT id, host_id, name, path, status, auto_update, created_at, updated_at FROM stacks WHERE id = ?",
		id,
	).Scan(&s.ID, &s.HostID, &s.Name, &s.Path, &s.Status, &s.AutoUpdate, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (db *DB) UpsertStack(s *Stack) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	s.CreatedAt = now
	s.UpdatedAt = now

	// 1. Check if a stack already exists with the exact same (host_id, path)
	var exactMatchID string
	err := db.conn.QueryRow(
		"SELECT id FROM stacks WHERE host_id = ? AND path = ?",
		s.HostID, s.Path,
	).Scan(&exactMatchID)

	if err == nil {
		s.ID = exactMatchID
		_, updateErr := db.conn.Exec(`
			UPDATE stacks
			SET name = ?, status = ?, updated_at = ?
			WHERE id = ?
		`, s.Name, s.Status, s.UpdatedAt, exactMatchID)
		if updateErr != nil {
			return updateErr
		}
		// Delete any stale duplicate stacks with the same name on this host
		_, _ = db.conn.Exec("DELETE FROM stacks WHERE host_id = ? AND name = ? AND id != ?", s.HostID, s.Name, exactMatchID)
		return nil
	}

	// 2. Check if a stack exists with the same (host_id, name) but different path (e.g. path migration)
	var nameMatchID string
	err = db.conn.QueryRow(
		"SELECT id FROM stacks WHERE host_id = ? AND name = ?",
		s.HostID, s.Name,
	).Scan(&nameMatchID)

	if err == nil {
		s.ID = nameMatchID
		_, updateErr := db.conn.Exec(`
			UPDATE stacks
			SET path = ?, status = ?, updated_at = ?
			WHERE id = ?
		`, s.Path, s.Status, s.UpdatedAt, nameMatchID)
		if updateErr != nil {
			return updateErr
		}
		// Delete any lingering duplicates
		_, _ = db.conn.Exec("DELETE FROM stacks WHERE host_id = ? AND name = ? AND id != ?", s.HostID, s.Name, nameMatchID)
		return nil
	}

	// 3. New stack insert
	_, insertErr := db.conn.Exec(`
		INSERT INTO stacks (id, host_id, name, path, status, auto_update, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, s.ID, s.HostID, s.Name, s.Path, s.Status, s.AutoUpdate, s.CreatedAt, s.UpdatedAt)
	return insertErr
}

func (db *DB) DeleteStack(id string) error {
	_, err := db.conn.Exec("DELETE FROM stacks WHERE id = ?", id)
	return err
}

// Revision operations
func (db *DB) CreateRevision(r *StackRevision) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.CreatedAt = time.Now().UTC()

	var nextRev int
	err := db.conn.QueryRow("SELECT COALESCE(MAX(revision_num), 0) + 1 FROM stack_revisions WHERE stack_id = ?", r.StackID).Scan(&nextRev)
	if err != nil {
		nextRev = 1
	}
	r.RevisionNum = nextRev

	_, err = db.conn.Exec(`
		INSERT INTO stack_revisions (id, stack_id, revision_num, compose_content, env_content, created_by, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.StackID, r.RevisionNum, r.ComposeContent, r.EnvContent, r.CreatedBy, r.Note, r.CreatedAt)
	return err
}

func (db *DB) ListRevisions(stackID string) ([]StackRevision, error) {
	rows, err := db.conn.Query(`
		SELECT id, stack_id, revision_num, compose_content, env_content, created_by, note, created_at
		FROM stack_revisions WHERE stack_id = ? ORDER BY revision_num DESC
	`, stackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	revs := make([]StackRevision, 0)
	for rows.Next() {
		var r StackRevision
		var note sql.NullString
		if err := rows.Scan(&r.ID, &r.StackID, &r.RevisionNum, &r.ComposeContent, &r.EnvContent, &r.CreatedBy, &note, &r.CreatedAt); err != nil {
			return nil, err
		}
		if note.Valid {
			r.Note = note.String
		}
		revs = append(revs, r)
	}
	return revs, nil
}

// Notification operations
func (db *DB) CreateNotification(n *Notification) error {
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}
	_, err := db.conn.Exec(`
		INSERT INTO notifications (id, title, message, type, host_id, read, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, n.ID, n.Title, n.Message, n.Type, n.HostID, n.Read, n.CreatedAt)
	return err
}

func (db *DB) ListNotifications(limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.conn.Query(`
		SELECT id, title, message, type, COALESCE(host_id, ''), read, created_at
		FROM notifications
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notifs := make([]Notification, 0)
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Title, &n.Message, &n.Type, &n.HostID, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
		}
		notifs = append(notifs, n)
	}
	return notifs, nil
}

func (db *DB) GetUnreadNotificationCount() (int, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM notifications WHERE read = 0").Scan(&count)
	return count, err
}

func (db *DB) MarkNotificationRead(id string) error {
	_, err := db.conn.Exec("UPDATE notifications SET read = 1 WHERE id = ?", id)
	return err
}

func (db *DB) MarkAllNotificationsRead() error {
	_, err := db.conn.Exec("UPDATE notifications SET read = 1")
	return err
}

func (db *DB) DeleteNotification(id string) error {
	_, err := db.conn.Exec("DELETE FROM notifications WHERE id = ?", id)
	return err
}

func (db *DB) ClearAllNotifications() error {
	_, err := db.conn.Exec("DELETE FROM notifications")
	return err
}

// NotificationConfig operations
func (db *DB) GetNotificationConfigs() ([]NotificationConfig, error) {
	rows, err := db.conn.Query("SELECT id, enabled, config_json, status, last_error, updated_at FROM notification_configs ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	configs := make([]NotificationConfig, 0)
	for rows.Next() {
		var c NotificationConfig
		if err := rows.Scan(&c.ID, &c.Enabled, &c.ConfigJSON, &c.Status, &c.LastError, &c.UpdatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, nil
}

func (db *DB) GetNotificationConfig(id string) (*NotificationConfig, error) {
	c := &NotificationConfig{}
	err := db.conn.QueryRow(
		"SELECT id, enabled, config_json, status, last_error, updated_at FROM notification_configs WHERE id = ?",
		id,
	).Scan(&c.ID, &c.Enabled, &c.ConfigJSON, &c.Status, &c.LastError, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (db *DB) SaveNotificationConfig(cfg *NotificationConfig) error {
	cfg.UpdatedAt = time.Now().UTC()
	_, err := db.conn.Exec(`
		INSERT INTO notification_configs (id, enabled, config_json, status, last_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			enabled = excluded.enabled,
			config_json = excluded.config_json,
			status = excluded.status,
			last_error = excluded.last_error,
			updated_at = excluded.updated_at
	`, cfg.ID, cfg.Enabled, cfg.ConfigJSON, cfg.Status, cfg.LastError, cfg.UpdatedAt)
	return err
}

// SchedulerConfig operations
func (db *DB) GetSchedulerConfig() (*SchedulerConfig, error) {
	c := &SchedulerConfig{}
	var lastRun, nextRun sql.NullTime
	err := db.conn.QueryRow("SELECT enabled, interval_minutes, last_run, next_run FROM scheduler_config WHERE id = 1").
		Scan(&c.Enabled, &c.IntervalMinutes, &lastRun, &nextRun)
	if err != nil {
		// Fallback default
		return &SchedulerConfig{
			Enabled:         true,
			IntervalMinutes: 360,
			LastRun:         time.Now().UTC(),
			NextRun:         time.Now().UTC().Add(6 * time.Hour),
		}, nil
	}
	if lastRun.Valid {
		c.LastRun = lastRun.Time
	}
	if nextRun.Valid {
		c.NextRun = nextRun.Time
	}
	return c, nil
}

func (db *DB) SaveSchedulerConfig(cfg *SchedulerConfig) error {
	_, err := db.conn.Exec(`
		INSERT INTO scheduler_config (id, enabled, interval_minutes, last_run, next_run)
		VALUES (1, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			enabled = excluded.enabled,
			interval_minutes = excluded.interval_minutes,
			last_run = excluded.last_run,
			next_run = excluded.next_run
	`, cfg.Enabled, cfg.IntervalMinutes, cfg.LastRun, cfg.NextRun)
	return err
}
