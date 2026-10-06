package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/dockpulse/dockmgr/internal/auth"
	"github.com/dockpulse/dockmgr/internal/database"
	"github.com/gin-gonic/gin"
)

// Profile endpoints
func (s *Server) handleGetProfile(c *gin.Context) {
	userID, _ := c.Get("user_id")
	user, err := s.db.GetUserByID(fmt.Sprint(userID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           user.ID,
		"username":     user.Username,
		"display_name": user.DisplayName,
		"avatar":       user.Avatar,
		"theme":        user.Theme,
		"role":         user.Role,
	})
}

func (s *Server) handleUpdateProfile(c *gin.Context) {
	userID, _ := c.Get("user_id")
	var req struct {
		DisplayName string `json:"display_name"`
		Username    string `json:"username" binding:"required"`
		Avatar      string `json:"avatar"`
		Theme       string `json:"theme"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username is required"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	if req.Theme == "" {
		req.Theme = "dark"
	}

	// Check if username is being changed to one that already exists
	existing, err := s.db.GetUserByUsername(req.Username)
	if err == nil && existing != nil && existing.ID != fmt.Sprint(userID) {
		c.JSON(http.StatusConflict, gin.H{"error": "Username is already taken"})
		return
	}

	if err := s.db.UpdateUserProfile(fmt.Sprint(userID), req.DisplayName, req.Username, req.Avatar, req.Theme); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "Profile updated successfully",
		"display_name": req.DisplayName,
		"username":     req.Username,
		"avatar":       req.Avatar,
		"theme":        req.Theme,
	})
}

func (s *Server) handleChangePassword(c *gin.Context) {
	userID, _ := c.Get("user_id")
	var req struct {
		CurrentPassword string `json:"current_password" binding:"required"`
		NewPassword     string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Current and new password required"})
		return
	}

	user, err := s.db.GetUserByID(fmt.Sprint(userID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	if !auth.CheckPassword(req.CurrentPassword, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Current password is incorrect"})
		return
	}

	if len(req.NewPassword) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "New password must be at least 6 characters"})
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process password"})
		return
	}

	if err := s.db.UpdateUserPassword(user.ID, hash); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}

// Notification settings
func (s *Server) handleGetNotificationConfigs(c *gin.Context) {
	configs, err := s.db.GetNotificationConfigs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch notification configurations"})
		return
	}
	c.JSON(http.StatusOK, configs)
}

func (s *Server) handleSaveNotificationConfig(c *gin.Context) {
	serviceID := strings.ToLower(c.Param("service"))
	var req struct {
		Enabled    bool   `json:"enabled"`
		ConfigJSON string `json:"config_json"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	existing, _ := s.db.GetNotificationConfig(serviceID)
	status := "unconfigured"
	if req.Enabled {
		status = "enabled"
	}
	lastError := ""
	if existing != nil && existing.LastError != "" && !req.Enabled {
		lastError = existing.LastError
	}

	cfg := &database.NotificationConfig{
		ID:         serviceID,
		Enabled:    req.Enabled,
		ConfigJSON: req.ConfigJSON,
		Status:     status,
		LastError:  lastError,
	}

	if err := s.db.SaveNotificationConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save configuration"})
		return
	}

	c.JSON(http.StatusOK, cfg)
}

func (s *Server) handleTestNotification(c *gin.Context) {
	serviceID := strings.ToLower(c.Param("service"))
	var req struct {
		ConfigJSON string `json:"config_json"`
	}
	_ = c.ShouldBindJSON(&req)

	configJSON := req.ConfigJSON
	if strings.TrimSpace(configJSON) == "" || configJSON == "{}" {
		existing, err := s.db.GetNotificationConfig(serviceID)
		if err == nil {
			configJSON = existing.ConfigJSON
		}
	}

	if strings.TrimSpace(configJSON) == "" || configJSON == "{}" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Configuration is empty. Please enter your settings first."})
		return
	}

	if err := s.dispatcher.TestService(c.Request.Context(), serviceID, configJSON); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Test alert sent successfully to %s!", strings.ToUpper(serviceID))})
}

// Scheduler settings
func (s *Server) handleGetSchedulerConfig(c *gin.Context) {
	cfg, err := s.db.GetSchedulerConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch scheduler config"})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

func (s *Server) handleSaveSchedulerConfig(c *gin.Context) {
	var req struct {
		Enabled         bool `json:"enabled"`
		IntervalMinutes int  `json:"interval_minutes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if req.IntervalMinutes <= 0 {
		req.IntervalMinutes = 360
	}

	cfg, _ := s.db.GetSchedulerConfig()
	cfg.Enabled = req.Enabled
	cfg.IntervalMinutes = req.IntervalMinutes

	if err := s.db.SaveSchedulerConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save scheduler config"})
		return
	}

	c.JSON(http.StatusOK, cfg)
}

func (s *Server) handleTriggerScheduler(c *gin.Context) {
	if s.scheduler != nil {
		s.scheduler.TriggerNow()
	}
	c.JSON(http.StatusOK, gin.H{"message": "Update check triggered in background"})
}
