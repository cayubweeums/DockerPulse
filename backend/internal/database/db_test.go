package database

import (
	"testing"
	"time"
)

func TestDeduplicateStacks(t *testing.T) {
	tmpDir := t.TempDir()

	db, err := InitDB(tmpDir)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	host := &Host{
		Name:    "Test Host",
		Driver:  DriverSocket,
		Address: "local",
		BaseDir: "/root/docker",
		Status:  "online",
	}
	if err := db.CreateHost(host); err != nil {
		t.Fatalf("failed to create host: %v", err)
	}

	// Insert duplicate stacks: one legacy container path, one host path
	s1 := &Stack{
		HostID: host.ID,
		Name:   "emby",
		Path:   "/root/docker/emby",
		Status: "discovered",
	}
	if err := db.UpsertStack(s1); err != nil {
		t.Fatalf("failed to insert s1: %v", err)
	}

	s2 := &Stack{
		HostID: host.ID,
		Name:   "emby",
		Path:   "/home/farmers00/docker/emby",
		Status: "discovered",
	}
	if err := db.UpsertStack(s2); err != nil {
		t.Fatalf("failed to upsert s2: %v", err)
	}

	// Verify only 1 stack remains and its path is the host path
	stacks, err := db.ListStacks(host.ID)
	if err != nil {
		t.Fatalf("failed to list stacks: %v", err)
	}
	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack after upsert, got %d", len(stacks))
	}
	if stacks[0].Path != "/home/farmers00/docker/emby" {
		t.Fatalf("expected path /home/farmers00/docker/emby, got %s", stacks[0].Path)
	}
}

func TestInitDBDeduplication(t *testing.T) {
	tmpDir := t.TempDir()

	db, err := InitDB(tmpDir)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}

	host := &Host{
		Name:    "Test Host",
		Driver:  DriverSocket,
		Address: "local",
		BaseDir: "/root/docker",
		Status:  "online",
	}
	_ = db.CreateHost(host)

	now := time.Now().UTC()
	// Manually insert two duplicates bypassing UpsertStack
	_, _ = db.conn.Exec(`
		INSERT INTO stacks (id, host_id, name, path, status, auto_update, created_at, updated_at)
		VALUES ('id1', ?, 'homepage', '/root/docker/homepage', 'discovered', 0, ?, ?)
	`, host.ID, now, now)

	_, _ = db.conn.Exec(`
		INSERT INTO stacks (id, host_id, name, path, status, auto_update, created_at, updated_at)
		VALUES ('id2', ?, 'homepage', '/home/farmers00/docker/homepage', 'discovered', 0, ?, ?)
	`, host.ID, now, now)

	db.Close()

	// Re-open DB to trigger InitDB deduplication migration
	db2, err := InitDB(tmpDir)
	if err != nil {
		t.Fatalf("failed to re-open db: %v", err)
	}
	defer db2.Close()

	stacks, err := db2.ListStacks(host.ID)
	if err != nil {
		t.Fatalf("failed to list stacks: %v", err)
	}
	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack after InitDB deduplication, got %d", len(stacks))
	}
	if stacks[0].Path != "/home/farmers00/docker/homepage" {
		t.Fatalf("expected path /home/farmers00/docker/homepage, got %s", stacks[0].Path)
	}
}

func TestNotificationsAndSettings(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := InitDB(tmpDir)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	// 1. Notification configs should be pre-seeded
	configs, err := db.GetNotificationConfigs()
	if err != nil {
		t.Fatalf("failed to get notification configs: %v", err)
	}
	if len(configs) < 3 {
		t.Fatalf("expected at least 3 configs (ntfy, discord, signal), got %d", len(configs))
	}

	// Update discord config
	discordCfg := &NotificationConfig{
		ID:         "discord",
		Enabled:    true,
		ConfigJSON: `{"webhook_url":"https://discord.com/api/webhooks/test"}`,
		Status:     "enabled",
		LastError:  "",
	}
	if err := db.SaveNotificationConfig(discordCfg); err != nil {
		t.Fatalf("failed to save notification config: %v", err)
	}
	retrieved, err := db.GetNotificationConfig("discord")
	if err != nil || !retrieved.Enabled || retrieved.Status != "enabled" {
		t.Fatalf("failed to retrieve updated config: %+v", retrieved)
	}

	// 2. Scheduler config
	sched, err := db.GetSchedulerConfig()
	if err != nil {
		t.Fatalf("failed to get scheduler config: %v", err)
	}
	sched.IntervalMinutes = 120
	sched.Enabled = true
	if err := db.SaveSchedulerConfig(sched); err != nil {
		t.Fatalf("failed to save scheduler config: %v", err)
	}
	sched2, _ := db.GetSchedulerConfig()
	if sched2.IntervalMinutes != 120 {
		t.Fatalf("expected interval 120, got %d", sched2.IntervalMinutes)
	}

	// 3. Notifications CRUD
	n1 := &Notification{
		Title:   "Update Available",
		Message: "jellyfin:latest has a new release",
		Type:    "update",
		HostID:  "host-1",
	}
	if err := db.CreateNotification(n1); err != nil {
		t.Fatalf("failed to create notification: %v", err)
	}

	count, err := db.GetUnreadNotificationCount()
	if err != nil || count != 1 {
		t.Fatalf("expected unread count 1, got %d (err: %v)", count, err)
	}

	list, err := db.ListNotifications(10)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(list))
	}

	if err := db.MarkNotificationRead(list[0].ID); err != nil {
		t.Fatalf("failed to mark read: %v", err)
	}
	count, _ = db.GetUnreadNotificationCount()
	if count != 0 {
		t.Fatalf("expected unread count 0, got %d", count)
	}

	if err := db.DeleteNotification(list[0].ID); err != nil {
		t.Fatalf("failed to delete notification: %v", err)
	}
	list, _ = db.ListNotifications(10)
	if len(list) != 0 {
		t.Fatalf("expected 0 notifications after delete, got %d", len(list))
	}
}

func TestUserManagement(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := InitDB(tmpDir)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	u := &User{
		Username:     "admin",
		PasswordHash: "hash123",
		Role:         RoleAdmin,
		DisplayName:  "Administrator",
	}
	if err := db.CreateUser(u); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	retrieved, err := db.GetUserByUsername("admin")
	if err != nil || retrieved.DisplayName != "Administrator" {
		t.Fatalf("expected display name Administrator, got %q", retrieved.DisplayName)
	}

	if err := db.UpdateUserProfile(u.ID, "Admin Super", "admin", "data:image/png;base64,abc", "dark"); err != nil {
		t.Fatalf("failed to update profile: %v", err)
	}
	u2, _ := db.GetUserByID(u.ID)
	if u2.DisplayName != "Admin Super" || u2.Avatar != "data:image/png;base64,abc" {
		t.Fatalf("failed to verify profile update: %+v", u2)
	}

	users, err := db.ListUsers()
	if err != nil || len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
}
