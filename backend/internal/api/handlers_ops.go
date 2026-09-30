package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/dockpulse/dockmgr/internal/database"
	"github.com/gin-gonic/gin"
)

func (s *Server) handleListNetworks(c *gin.Context) {
	hostID := c.Param("id")
	host, err := s.db.GetHost(hostID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 6*time.Second)
	defer cancel()

	d, err := s.GetDriver(ctx, host)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	defer d.Close()

	nets, err := d.ListNetworks(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, nets)
}

func (s *Server) handleCreateNetwork(c *gin.Context) {
	hostID := c.Param("id")
	var req struct {
		Name   string `json:"name" binding:"required"`
		Driver string `json:"driver"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	host, err := s.db.GetHost(hostID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	d, err := s.GetDriver(ctx, host)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	defer d.Close()

	if err := d.CreateNetwork(ctx, req.Name, req.Driver); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true})
}

func (s *Server) handleRemoveNetwork(c *gin.Context) {
	hostID := c.Param("id")
	nid := c.Param("nid")

	host, err := s.db.GetHost(hostID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	d, err := s.GetDriver(ctx, host)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	defer d.Close()

	if err := d.RemoveNetwork(ctx, nid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (s *Server) handleConnectNetwork(c *gin.Context) {
	hostID := c.Param("id")
	nid := c.Param("nid")
	var req struct {
		ContainerID string `json:"container_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	host, err := s.db.GetHost(hostID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	d, err := s.GetDriver(ctx, host)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	defer d.Close()

	if err := d.ConnectNetwork(ctx, nid, req.ContainerID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (s *Server) handleDisconnectNetwork(c *gin.Context) {
	hostID := c.Param("id")
	nid := c.Param("nid")
	var req struct {
		ContainerID string `json:"container_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	host, err := s.db.GetHost(hostID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	d, err := s.GetDriver(ctx, host)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	defer d.Close()

	if err := d.DisconnectNetwork(ctx, nid, req.ContainerID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (s *Server) handleGetStorageUsage(c *gin.Context) {
	hostID := c.Param("id")
	host, err := s.db.GetHost(hostID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	d, err := s.GetDriver(ctx, host)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	defer d.Close()

	usage, err := d.GetDiskUsage(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, usage)
}

func (s *Server) handlePruneStorage(c *gin.Context) {
	hostID := c.Param("id")
	pruneAll, _ := strconv.ParseBool(c.DefaultQuery("all", "false"))

	host, err := s.db.GetHost(hostID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	d, err := s.GetDriver(ctx, host)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	defer d.Close()

	report, err := d.PruneResources(ctx, pruneAll)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}

func (s *Server) handleCheckUpdates(c *gin.Context) {
	hostID := c.Param("id")
	host, err := s.db.GetHost(hostID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	d, err := s.GetDriver(ctx, host)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	defer d.Close()

	results, err := s.updater.CheckHostContainers(ctx, d, hostID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, results)
}

func (s *Server) handleAgentWS(c *gin.Context) {
	token := c.Query("token")
	hostID := c.Query("host_id")

	if token != s.cfg.AgentSecret || hostID == "" {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	ws, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	// Auto-register host in database if not present, or set status to online
	h, err := s.db.GetHost(hostID)
	if err != nil || h == nil {
		newHost := &database.Host{
			ID:       hostID,
			Name:     hostID,
			Driver:   database.DriverAgent,
			Address:  c.ClientIP(),
			BaseDir:  "~/docker",
			Status:   "online",
			LastSeen: time.Now().UTC(),
		}
		_ = s.db.CreateHost(newHost)
	} else {
		_ = s.db.UpdateHostStatus(hostID, "online")
	}

	session := s.agentManager.Register(hostID, ws)

	defer func() {
		s.agentManager.Unregister(hostID)
		_ = s.db.UpdateHostStatus(hostID, "offline")
		ws.Close()
	}()

	session.Wait()
}

func (s *Server) handleGetAgentToken(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"token": s.cfg.AgentSecret,
		"image": s.cfg.Image,
	})
}

func (s *Server) handleInstallAgentScript(c *gin.Context) {
	host := c.Request.Host
	scheme := "ws"
	httpScheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "wss"
		httpScheme = "https"
	}

	token := c.DefaultQuery("token", s.cfg.AgentSecret)
	hostID := c.DefaultQuery("id", "remote-node")

	wsURL := fmt.Sprintf("%s://%s/ws/agent", scheme, host)
	serverURL := fmt.Sprintf("%s://%s", httpScheme, host)

	script := fmt.Sprintf(`#!/usr/bin/env bash
set -e

echo "======================================================"
echo "      DockerPulse Agent One-Line Installer"
echo "======================================================"

if ! command -v docker >/dev/null 2>&1; then
  echo "[Error] Docker is not installed on this host."
  echo "Please install Docker and Docker Compose before running this installer."
  exit 1
fi

HOST_ID="%s"
TOKEN="%s"
WS_URL="%s"
SERVER_URL="%s"

ACTUAL_USER="$USER"
if [ "$EUID" -eq 0 ] && [ -n "$SUDO_USER" ]; then
  ACTUAL_USER="$SUDO_USER"
fi
USER_HOME=$(getent passwd "$ACTUAL_USER" 2>/dev/null | cut -d: -f6)
if [ -z "$USER_HOME" ]; then
  USER_HOME="$HOME"
fi
TARGET_DIR="${TARGET_DIR:-$USER_HOME/docker/dockerpulse-agent}"

echo "[DockerPulse] Host ID:     $HOST_ID"
echo "[DockerPulse] Server URL:  $SERVER_URL"
echo "[DockerPulse] Agent Dir:   $TARGET_DIR"

mkdir -p "$TARGET_DIR"
cd "$TARGET_DIR"

echo "[DockerPulse] Writing docker-compose.yml..."
cat << 'EOF' > docker-compose.yml
services:
  dockerpulse-agent:
    image: %s
    container_name: dockerpulse-agent
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ${HOME}/docker:/root/docker
    environment:
      - HOST_BASE_DIR=${HOME}/docker
    command: >
      agent
      --server %s
      --token %s
      --host-id %s
      --base-dir /root/docker
EOF

echo "[DockerPulse] Pulling and starting DockerPulse agent container..."
docker compose pull || true
docker compose up -d

echo ""
echo "======================================================"
echo " DockerPulse Agent container is now running!"
echo " Host '$HOST_ID' connected to $SERVER_URL"
echo "======================================================"
`, hostID, token, wsURL, serverURL, s.cfg.Image, wsURL, token, hostID)

	c.Header("Content-Type", "text/x-shellscript; charset=utf-8")
	c.String(http.StatusOK, script)
}

func (s *Server) handleAgentComposeTemplate(c *gin.Context) {
	host := c.Request.Host
	scheme := "ws"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "wss"
	}

	token := c.DefaultQuery("token", s.cfg.AgentSecret)
	hostID := c.DefaultQuery("id", "node-1")
	wsURL := fmt.Sprintf("%s://%s/ws/agent", scheme, host)

	template := fmt.Sprintf(`services:
  dockerpulse-agent:
    image: %s
    container_name: dockerpulse-agent
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ${HOME}/docker:/root/docker
    environment:
      - HOST_BASE_DIR=${HOME}/docker
    command: >
      agent
      --server %s
      --token %s
      --host-id %s
      --base-dir /root/docker
`, s.cfg.Image, wsURL, token, hostID)

	c.Header("Content-Type", "text/yaml; charset=utf-8")
	c.String(http.StatusOK, template)
}
