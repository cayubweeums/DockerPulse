package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleListNotifications(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "100")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 100
	}

	notifs, err := s.db.ListNotifications(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch notifications"})
		return
	}
	c.JSON(http.StatusOK, notifs)
}

func (s *Server) handleGetUnreadNotificationCount(c *gin.Context) {
	count, err := s.db.GetUnreadNotificationCount()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count unread notifications"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"unread_count": count})
}

func (s *Server) handleMarkNotificationRead(c *gin.Context) {
	id := c.Param("id")
	if err := s.db.MarkNotificationRead(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to mark notification as read"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Notification marked as read"})
}

func (s *Server) handleMarkAllNotificationsRead(c *gin.Context) {
	if err := s.db.MarkAllNotificationsRead(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to mark all notifications as read"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "All notifications marked as read"})
}

func (s *Server) handleDeleteNotification(c *gin.Context) {
	id := c.Param("id")
	if err := s.db.DeleteNotification(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete notification"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Notification dismissed"})
}

func (s *Server) handleClearAllNotifications(c *gin.Context) {
	if err := s.db.ClearAllNotifications(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear notifications"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "All notifications dismissed"})
}
