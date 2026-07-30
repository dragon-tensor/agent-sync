package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/agent-sync/agent-sync/internal/chat"
	"github.com/agent-sync/agent-sync/internal/config"
	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/internal/tui"
)

const version = "0.1.0-dev"

// Run starts the local Dragon Sync workspace and its terminal surface.
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()
	service := chat.NewService(database, nil)
	defer service.Close()
	program := tui.NewProgram(service)
	_, err = program.Run()
	return err
}

// Main provides the small command-line boundary shared by both executable
// names: dragon-sync and sync.
func Main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--version", "-v":
			fmt.Printf("dragon-sync %s\n", version)
			return
		case "--help", "-h":
			fmt.Println("dragon-sync — universal AI workspace")
			fmt.Println("\nLaunches the Dragon Sync terminal interface.")
			return
		}
	}

	if err := Run(); err != nil {
		fmt.Fprintf(os.Stderr, "dragon-sync: %v\n", err)
		os.Exit(1)
	}
}
