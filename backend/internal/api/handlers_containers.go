package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/dockpulse/dockmgr/internal/driver"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func (s *Server) handleListContainers(c *gin.Context) {
	hostID := c.Param("id")
	host, err := s.db.GetHost(hostID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	d, err := s.GetDriver(ctx, host)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	defer d.Close()

	containers, err := d.ListContainers(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, containers)
}

func (s *Server) handleStartContainer(c *gin.Context) {
	s.execContainerOp(c, func(ctx context.Context, d driver.HostDriver, cid string) error {
		return d.StartContainer(ctx, cid)
	})
}

func (s *Server) handleStopContainer(c *gin.Context) {
	s.execContainerOp(c, func(ctx context.Context, d driver.HostDriver, cid string) error {
		return d.StopContainer(ctx, cid)
	})
}

func (s *Server) handleRestartContainer(c *gin.Context) {
	s.execContainerOp(c, func(ctx context.Context, d driver.HostDriver, cid string) error {
		return d.RestartContainer(ctx, cid)
	})
}

func (s *Server) handleRemoveContainer(c *gin.Context) {
	force, _ := strconv.ParseBool(c.DefaultQuery("force", "false"))
	s.execContainerOp(c, func(ctx context.Context, d driver.HostDriver, cid string) error {
		return d.RemoveContainer(ctx, cid, force)
	})
}

func (s *Server) execContainerOp(c *gin.Context, op func(context.Context, driver.HostDriver, string) error) {
	hostID := c.Param("id")
	cid := c.Param("cid")

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

	if err := op(ctx, d, cid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Log streaming WebSocket
func (s *Server) handleContainerLogsWS(c *gin.Context) {
	hostID := c.Param("id")
	cid := c.Param("cid")
	follow, _ := strconv.ParseBool(c.DefaultQuery("follow", "true"))
	tail := c.DefaultQuery("tail", "200")

	host, err := s.db.GetHost(hostID)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	ws, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	d, err := s.GetDriver(c.Request.Context(), host)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("[DockPulse] Error connecting to host: "+err.Error()))
		return
	}
	defer d.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	// Client close listener
	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	writer := &wsWriter{ws: ws}
	_ = d.StreamLogs(ctx, cid, follow, tail, writer)
}

// Terminal interactive PTY WebSocket
func (s *Server) handleContainerTerminalWS(c *gin.Context) {
	hostID := c.Param("id")
	cid := c.Param("cid")

	host, err := s.db.GetHost(hostID)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	ws, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	d, err := s.GetDriver(c.Request.Context(), host)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("[DockPulse] Host unreachable: "+err.Error()))
		return
	}
	defer d.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	resizeChan := make(chan driver.TerminalSize, 10)
	inReader, inWriter := io.Pipe()
	outWriter := &wsWriter{ws: ws}

	// Read input from WebSocket
	go func() {
		defer inWriter.Close()
		for {
			msgType, data, err := ws.ReadMessage()
			if err != nil {
				cancel()
				return
			}
			if msgType == websocket.TextMessage {
				// Check for resize JSON payload
				var resize struct {
					Type string `json:"type"`
					Rows uint16 `json:"rows"`
					Cols uint16 `json:"cols"`
				}
				if err := json.Unmarshal(data, &resize); err == nil && resize.Type == "resize" {
					resizeChan <- driver.TerminalSize{Rows: resize.Rows, Cols: resize.Cols}
					continue
				}
			}
			_, _ = inWriter.Write(data)
		}
	}()

	shell := c.DefaultQuery("shell", "/bin/sh")
	cmd := []string{shell}
	_ = d.ExecShell(ctx, cid, cmd, inReader, outWriter, resizeChan)
}

type wsWriter struct {
	mu sync.Mutex
	ws *websocket.Conn
}

func (w *wsWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	err = w.ws.WriteMessage(websocket.BinaryMessage, p)
	return len(p), err
}
