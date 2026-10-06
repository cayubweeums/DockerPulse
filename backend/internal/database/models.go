package database

import (
	"time"
)

type DriverType string

const (
	DriverAgent  DriverType = "agent"
	DriverSSH    DriverType = "ssh"
	DriverSocket DriverType = "socket"
)

type UserRole string

const (
	RoleAdmin  UserRole = "admin"
	RoleViewer UserRole = "viewer"
)

type Host struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Driver    DriverType `json:"driver"`
	Address   string     `json:"address"`
	Port      int        `json:"port"`
	AuthToken string     `json:"auth_token,omitempty"`
	SSHUser   string     `json:"ssh_user,omitempty"`
	SSHKey    string     `json:"ssh_key,omitempty"`
	BaseDir   string     `json:"base_dir"`
	Status    string     `json:"status"` // "online", "offline", "unreachable"
	LastSeen  time.Time  `json:"last_seen"`
	CreatedAt time.Time  `json:"created_at"`
}

type Stack struct {
	ID         string    `json:"id"`
	HostID     string    `json:"host_id"`
	Name       string    `json:"name"`
	Path       string    `json:"path"` // Host filesystem directory path
	Status     string    `json:"status"` // "running", "partial", "stopped", "unknown"
	AutoUpdate bool      `json:"auto_update"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type StackRevision struct {
	ID             string    `json:"id"`
	StackID        string    `json:"stack_id"`
	RevisionNum    int       `json:"revision_num"`
	ComposeContent string    `json:"compose_content"`
	EnvContent     string    `json:"env_content"`
	CreatedBy      string    `json:"created_by"`
	Note           string    `json:"note"`
	CreatedAt      time.Time `json:"created_at"`
}

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         UserRole  `json:"role"`
	DisplayName  string    `json:"display_name"`
	Avatar       string    `json:"avatar"`
	Theme        string    `json:"theme"`
	CreatedAt    time.Time `json:"created_at"`
}

type Notification struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Type      string    `json:"type"` // "update", "info", "warning", "error"
	HostID    string    `json:"host_id,omitempty"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

type NotificationConfig struct {
	ID         string    `json:"id"` // "ntfy", "discord", "signal"
	Enabled    bool      `json:"enabled"`
	ConfigJSON string    `json:"config_json"`
	Status     string    `json:"status"` // "unconfigured", "enabled", "error"
	LastError  string    `json:"last_error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type SchedulerConfig struct {
	Enabled         bool      `json:"enabled"`
	IntervalMinutes int       `json:"interval_minutes"`
	LastRun         time.Time `json:"last_run"`
	NextRun         time.Time `json:"next_run"`
}

type UpdateCheck struct {
	ID            string    `json:"id"`
	HostID        string    `json:"host_id"`
	StackID       string    `json:"stack_id"`
	ServiceName   string    `json:"service_name"`
	ImageName     string    `json:"image_name"`
	CurrentDigest string    `json:"current_digest"`
	RemoteDigest  string    `json:"remote_digest"`
	HasUpdate     bool      `json:"has_update"`
	LastCheckedAt time.Time `json:"last_checked_at"`
}

type AuditLog struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}
