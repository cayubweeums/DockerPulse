package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/dockpulse/dockmgr/internal/auth"
	"github.com/dockpulse/dockmgr/internal/database"
	"github.com/gin-gonic/gin"
)

func (s *Server) checkAdmin(c *gin.Context) bool {
	role, _ := c.Get("role")
	if fmt.Sprint(role) != string(database.RoleAdmin) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Admin privileges required"})
		return false
	}
	return true
}

func (s *Server) handleListUsers(c *gin.Context) {
	if !s.checkAdmin(c) {
		return
	}

	users, err := s.db.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list users"})
		return
	}

	var sanitized []gin.H
	for _, u := range users {
		sanitized = append(sanitized, gin.H{
			"id":           u.ID,
			"username":     u.Username,
			"display_name": u.DisplayName,
			"avatar":       u.Avatar,
			"theme":        u.Theme,
			"role":         u.Role,
			"created_at":   u.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, sanitized)
}

func (s *Server) handleCreateUser(c *gin.Context) {
	if !s.checkAdmin(c) {
		return
	}

	var req struct {
		Username    string            `json:"username" binding:"required"`
		Password    string            `json:"password" binding:"required"`
		DisplayName string            `json:"display_name"`
		Role        database.UserRole `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username and password required"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	if req.Role != database.RoleAdmin && req.Role != database.RoleViewer {
		req.Role = database.RoleViewer
	}

	existing, _ := s.db.GetUserByUsername(req.Username)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Username already exists"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	newUser := &database.User{
		Username:     req.Username,
		DisplayName:  req.DisplayName,
		PasswordHash: hash,
		Role:         req.Role,
		Theme:        "dark",
	}

	if err := s.db.CreateUser(newUser); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           newUser.ID,
		"username":     newUser.Username,
		"display_name": newUser.DisplayName,
		"role":         newUser.Role,
		"created_at":   newUser.CreatedAt,
	})
}

func (s *Server) handleUpdateUser(c *gin.Context) {
	if !s.checkAdmin(c) {
		return
	}

	targetID := c.Param("id")
	targetUser, err := s.db.GetUserByID(targetID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	var req struct {
		DisplayName string            `json:"display_name"`
		Role        database.UserRole `json:"role"`
		Password    string            `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if req.DisplayName != "" {
		_ = s.db.UpdateUserProfile(targetID, req.DisplayName, targetUser.Username, targetUser.Avatar, targetUser.Theme)
	}

	if req.Role == database.RoleAdmin || req.Role == database.RoleViewer {
		_ = s.db.UpdateUserRole(targetID, req.Role)
	}

	if strings.TrimSpace(req.Password) != "" {
		hash, err := auth.HashPassword(req.Password)
		if err == nil {
			_ = s.db.UpdateUserPassword(targetID, hash)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "User updated successfully"})
}

func (s *Server) handleDeleteUser(c *gin.Context) {
	if !s.checkAdmin(c) {
		return
	}

	targetID := c.Param("id")
	callerID, _ := c.Get("user_id")

	if targetID == fmt.Sprint(callerID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot delete your own account"})
		return
	}

	// Verify we are not deleting the last remaining admin
	users, _ := s.db.ListUsers()
	adminCount := 0
	for _, u := range users {
		if u.Role == database.RoleAdmin && u.ID != targetID {
			adminCount++
		}
	}
	if adminCount == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete the only remaining admin account"})
		return
	}

	if err := s.db.DeleteUser(targetID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
}
