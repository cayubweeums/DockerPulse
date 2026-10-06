package api

import (
	"context"
	"io/fs"
	"net/http"
	"time"

	"github.com/dockpulse/dockmgr/internal/auth"
	"github.com/dockpulse/dockmgr/internal/config"
	"github.com/dockpulse/dockmgr/internal/database"
	"github.com/dockpulse/dockmgr/internal/driver"
	"github.com/dockpulse/dockmgr/internal/notifications"
	"github.com/dockpulse/dockmgr/internal/updater"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Server struct {
	cfg          *config.Config
	db           *database.DB
	agentManager *driver.AgentManager
	updater      *updater.UpdateChecker
	dispatcher   *notifications.Dispatcher
	scheduler    *notifications.Scheduler
	upgrader     websocket.Upgrader
	frontendFS   fs.FS
}

func NewServer(cfg *config.Config, db *database.DB, frontendFS fs.FS) *Server {
	dispatcher := notifications.NewDispatcher(db)
	updateChecker := updater.NewUpdateChecker(db)

	s := &Server{
		cfg:          cfg,
		db:           db,
		agentManager: driver.NewAgentManager(),
		updater:      updateChecker,
		dispatcher:   dispatcher,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Homelab / multi-origin friendly
			},
		},
		frontendFS: frontendFS,
	}

	s.scheduler = notifications.NewScheduler(db, dispatcher, updateChecker, s.GetDriver)
	s.scheduler.Start()

	return s
}

func (s *Server) GetDriver(ctx context.Context, host *database.Host) (driver.HostDriver, error) {
	switch host.Driver {
	case database.DriverAgent:
		return driver.NewAgentDriver(host.ID, s.agentManager), nil
	case database.DriverSSH:
		return driver.NewSSHDriver(host.Address, host.Port, host.SSHUser, host.SSHKey)
	case database.DriverSocket:
		return driver.NewSocketDriver(host.Address)
	default:
		return driver.NewSocketDriver("local")
	}
}

func (s *Server) SetupRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	// CORS for local dev Vite server (port 5173)
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", s.cfg.ProxyAuthHeader, s.cfg.ProxyEmailHeader},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Public Auth endpoints
	authGroup := r.Group("/api/auth")
	{
		authGroup.GET("/status", s.handleAuthStatus)
		authGroup.POST("/setup", s.handleSetup)
		authGroup.POST("/login", s.handleLogin)
	}

	// Remote Agent WebSocket listener
	r.GET("/ws/agent", s.handleAgentWS)

	// Public one-line installer script & compose template
	r.GET("/install-agent.sh", s.handleInstallAgentScript)
	r.GET("/docker-compose.agent.yml", s.handleAgentComposeTemplate)

	// Protected API endpoints
	api := r.Group("/api")
	api.Use(auth.AuthMiddleware(s.cfg, s.db))
	{
		api.GET("/auth/me", s.handleMe)
		api.GET("/agent/token", s.handleGetAgentToken)

		// Hosts
		api.GET("/hosts", s.handleListHosts)
		api.POST("/hosts", s.handleCreateHost)
		api.GET("/hosts/:id", s.handleGetHost)
		api.PUT("/hosts/:id", s.handleUpdateHost)
		api.DELETE("/hosts/:id", s.handleDeleteHost)
		api.GET("/hosts/:id/system", s.handleGetSystemInfo)

		// Containers
		api.GET("/hosts/:id/containers", s.handleListContainers)
		api.POST("/hosts/:id/containers/:cid/start", s.handleStartContainer)
		api.POST("/hosts/:id/containers/:cid/stop", s.handleStopContainer)
		api.POST("/hosts/:id/containers/:cid/restart", s.handleRestartContainer)
		api.DELETE("/hosts/:id/containers/:cid", s.handleRemoveContainer)
		api.GET("/hosts/:id/containers/:cid/logs", s.handleContainerLogsWS)
		api.GET("/hosts/:id/containers/:cid/terminal", s.handleContainerTerminalWS)

		// Stacks (Hybrid discovery & Compose)
		api.GET("/hosts/:id/stacks", s.handleListStacks)
		api.POST("/hosts/:id/stacks/discover", s.handleDiscoverStacks)
		api.GET("/hosts/:id/stacks/:sid/files", s.handleGetStackFiles)
		api.PUT("/hosts/:id/stacks/:sid/files", s.handleSaveStackFiles)
		api.DELETE("/hosts/:id/stacks/:sid", s.handleDeleteStack)
		api.POST("/hosts/:id/stacks/:sid/action", s.handleComposeAction)
		api.GET("/hosts/:id/stacks/:sid/revisions", s.handleListRevisions)

		// Networks
		api.GET("/hosts/:id/networks", s.handleListNetworks)
		api.POST("/hosts/:id/networks", s.handleCreateNetwork)
		api.DELETE("/hosts/:id/networks/:nid", s.handleRemoveNetwork)
		api.POST("/hosts/:id/networks/:nid/connect", s.handleConnectNetwork)
		api.POST("/hosts/:id/networks/:nid/disconnect", s.handleDisconnectNetwork)

		// Storage & Prune
		api.GET("/hosts/:id/storage", s.handleGetStorageUsage)
		api.POST("/hosts/:id/storage/prune", s.handlePruneStorage)

		// Image Updates
		api.GET("/hosts/:id/updates/check", s.handleCheckUpdates)

		// Notifications (In-App Drawer)
		api.GET("/notifications", s.handleListNotifications)
		api.GET("/notifications/unread-count", s.handleGetUnreadNotificationCount)
		api.POST("/notifications/:id/read", s.handleMarkNotificationRead)
		api.POST("/notifications/read-all", s.handleMarkAllNotificationsRead)
		api.DELETE("/notifications/:id", s.handleDeleteNotification)
		api.DELETE("/notifications", s.handleClearAllNotifications)

		// Settings: Profile & Account
		api.GET("/settings/profile", s.handleGetProfile)
		api.PUT("/settings/profile", s.handleUpdateProfile)
		api.PUT("/settings/password", s.handleChangePassword)

		// Settings: External Notification Services (ntfy, discord, signal)
		api.GET("/settings/notifications", s.handleGetNotificationConfigs)
		api.PUT("/settings/notifications/:service", s.handleSaveNotificationConfig)
		api.POST("/settings/notifications/:service/test", s.handleTestNotification)

		// Settings: Background Update Scheduler
		api.GET("/settings/scheduler", s.handleGetSchedulerConfig)
		api.PUT("/settings/scheduler", s.handleSaveSchedulerConfig)
		api.POST("/settings/scheduler/run", s.handleTriggerScheduler)

		// User Management (Admin Only)
		api.GET("/users", s.handleListUsers)
		api.POST("/users", s.handleCreateUser)
		api.PUT("/users/:id", s.handleUpdateUser)
		api.DELETE("/users/:id", s.handleDeleteUser)
	}

	// Serve Frontend SPA if embedded
	if s.frontendFS != nil {
		fileServer := http.FileServer(http.FS(s.frontendFS))
		r.NoRoute(func(c *gin.Context) {
			path := c.Request.URL.Path
			// If file exists in embedded assets, serve it
			f, err := s.frontendFS.Open(path[1:])
			if err == nil {
				f.Close()
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
			// Fallback to index.html for SPA client-side routing
			c.Request.URL.Path = "/"
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
	}

	return r
}
