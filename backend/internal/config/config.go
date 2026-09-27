package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port              int
	DataDir           string
	JWTSecret         string
	AgentSecret       string
	ProxyAuthHeader   string // e.g. "Remote-User" or "X-Forwarded-User"
	ProxyEmailHeader  string // e.g. "Remote-Email"
	UpdateIntervalMin int
	Image             string
}

func LoadConfig() *Config {
	port := 8080
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil && p > 0 {
		port = p
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = generateRandomSecret(32)
	}

	agentSecret := os.Getenv("AGENT_SECRET")
	if agentSecret == "" {
		agentSecret = generateRandomSecret(24)
	}

	proxyAuthHeader := os.Getenv("PROXY_AUTH_HEADER")
	if proxyAuthHeader == "" {
		proxyAuthHeader = "Remote-User"
	}

	proxyEmailHeader := os.Getenv("PROXY_EMAIL_HEADER")
	if proxyEmailHeader == "" {
		proxyEmailHeader = "Remote-Email"
	}

	updateInterval := 60
	if u, err := strconv.Atoi(os.Getenv("UPDATE_INTERVAL_MINUTES")); err == nil && u > 0 {
		updateInterval = u
	}

	image := "ghcr.io/farmers00/dockerpulse:latest"
	if repo := os.Getenv("GITHUB_REPOSITORY"); repo != "" {
		image = "ghcr.io/" + strings.ToLower(repo) + ":latest"
	}

	return &Config{
		Port:              port,
		DataDir:           dataDir,
		JWTSecret:         jwtSecret,
		AgentSecret:       agentSecret,
		ProxyAuthHeader:   proxyAuthHeader,
		ProxyEmailHeader:  proxyEmailHeader,
		UpdateIntervalMin: updateInterval,
		Image:             image,
	}
}

func generateRandomSecret(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "dockpulse-default-fallback-secret-2026"
	}
	return hex.EncodeToString(bytes)
}
