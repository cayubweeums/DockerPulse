package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/dockpulse/dockmgr/internal/agent"
	"github.com/dockpulse/dockmgr/internal/api"
	"github.com/dockpulse/dockmgr/internal/config"
	"github.com/dockpulse/dockmgr/internal/database"
)

func main() {
	// If the container entrypoint already invoked 'dockerpulse' and the command also starts with 'dockerpulse', strip it
	if len(os.Args) >= 2 && (os.Args[1] == "dockerpulse" || os.Args[1] == "/dockerpulse" || os.Args[1] == "/usr/local/bin/dockerpulse") {
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	mode := os.Args[1]
	switch mode {
	case "server":
		runServer()
	case "agent":
		runAgent()
	case "version":
		fmt.Println("DockerPulse v0.9.00")
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`DockerPulse - Multi-Server Docker & Compose Fleet Manager

Usage:
  dockerpulse <command> [arguments]

Commands:
  server    Start the DockerPulse central management server & web dashboard
  agent     Start the lightweight DockerPulse agent on a remote Docker host
  version   Show version information`)
}

func runServer() {
	serverCmd := flag.NewFlagSet("server", flag.ExitOnError)
	port := serverCmd.Int("port", 8080, "Web server port")
	dataDir := serverCmd.String("data-dir", "./data", "Directory for SQLite database and state")
	_ = serverCmd.Parse(os.Args[2:])

	cfg := config.LoadConfig()
	if *port != 8080 {
		cfg.Port = *port
	}
	if *dataDir != "./data" {
		cfg.DataDir = *dataDir
	}

	db, err := database.InitDB(cfg.DataDir)
	if err != nil {
		log.Fatalf("[DockPulse] Failed to initialize database: %v", err)
	}
	defer db.Close()

	feFS, err := api.GetEmbeddedFrontend()
	if err != nil {
		log.Printf("[DockPulse] Warning: embedded frontend not found: %v", err)
	}
	srv := api.NewServer(cfg, db, feFS)
	router := srv.SetupRouter()

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("[DockPulse] Server listening on http://0.0.0.0%s", addr)
	log.Printf("[DockPulse] Data directory: %s", cfg.DataDir)
	log.Printf("[DockPulse] Agent Join Secret: %s", cfg.AgentSecret)

	if err := router.Run(addr); err != nil {
		log.Fatalf("[DockPulse] Server error: %v", err)
	}
}

func runAgent() {
	agentCmd := flag.NewFlagSet("agent", flag.ExitOnError)
	serverURL := agentCmd.String("server", "ws://localhost:8080/ws/agent", "DockPulse Server WebSocket URL")
	token := agentCmd.String("token", "", "DockPulse Agent Secret Token")
	hostID := agentCmd.String("host-id", "", "Unique Host ID registered in DockPulse")
	baseDir := agentCmd.String("base-dir", "~/docker", "Base directory where stacks are located")
	_ = agentCmd.Parse(os.Args[2:])

	if *token == "" || *hostID == "" {
		fmt.Println("Error: --token and --host-id are required to run in agent mode")
		agentCmd.Usage()
		os.Exit(1)
	}

	cfg := agent.Config{
		ServerURL: *serverURL,
		Token:     *token,
		HostID:    *hostID,
		BaseDir:   *baseDir,
	}

	ag, err := agent.NewAgent(cfg)
	if err != nil {
		log.Fatalf("[DockPulse Agent] Initialization failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("[DockPulse Agent] Shutting down...")
		cancel()
	}()

	log.Printf("[DockPulse Agent] Starting agent for host '%s', base dir: %s", *hostID, *baseDir)
	if err := ag.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("[DockPulse Agent] Fatal error: %v", err)
	}
}
