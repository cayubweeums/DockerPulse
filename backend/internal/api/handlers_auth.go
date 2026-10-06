package api

import (
	"fmt"
	"net/http"

	"github.com/dockpulse/dockmgr/internal/auth"
	"github.com/dockpulse/dockmgr/internal/database"
	"github.com/gin-gonic/gin"
)

func (s *Server) handleAuthStatus(c *gin.Context) {
	count, err := s.db.CountUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"initialized": count > 0,
	})
}

func (s *Server) handleSetup(c *gin.Context) {
	count, err := s.db.CountUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Application is already initialized"})
		return
	}

	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		BaseDir  string `json:"base_dir"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username and password required"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	user := &database.User{
		Username:     req.Username,
		PasswordHash: hash,
		Role:         database.RoleAdmin,
	}

	if err := s.db.CreateUser(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// Seed or update Local Server with custom docker path if provided
	baseDir := req.BaseDir
	if baseDir == "" {
		baseDir = "~/docker"
	}
	hosts, _ := s.db.ListHosts()
	if len(hosts) == 0 {
		localHost := &database.Host{
			Name:    "Local Server",
			Driver:  database.DriverSocket,
			Address: "local",
			BaseDir: baseDir,
			Status:  "online",
		}
		_ = s.db.CreateHost(localHost)
	} else if req.BaseDir != "" {
		for i := range hosts {
			if hosts[i].Driver == database.DriverSocket {
				hosts[i].BaseDir = req.BaseDir
				_ = s.db.UpdateHost(&hosts[i])
				break
			}
		}
	}

	token, _ := auth.GenerateToken(user, s.cfg.JWTSecret)
	c.SetCookie("dockpulse_token", token, 60*60*24*7, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

func (s *Server) handleLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	user, err := s.db.GetUserByUsername(req.Username)
	if err != nil || !auth.CheckPassword(req.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	token, err := auth.GenerateToken(user, s.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to sign token"})
		return
	}

	c.SetCookie("dockpulse_token", token, 60*60*24*7, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

func (s *Server) handleMe(c *gin.Context) {
	username, _ := c.Get("username")
	role, _ := c.Get("role")
	userID, _ := c.Get("user_id")

	displayName := ""
	avatar := ""
	theme := "dark"
	if u, err := s.db.GetUserByID(fmt.Sprint(userID)); err == nil && u != nil {
		displayName = u.DisplayName
		avatar = u.Avatar
		if u.Theme != "" {
			theme = u.Theme
		}
	}
	if displayName == "" && username != nil {
		displayName = fmt.Sprint(username)
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id":      userID,
		"username":     username,
		"display_name": displayName,
		"avatar":       avatar,
		"theme":        theme,
		"role":         role,
	})
}
