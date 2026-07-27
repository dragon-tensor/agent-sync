package app

import (
	"fmt"
	"os"

	"github.com/agent-sync/agent-sync/internal/tui"
)

const version = "0.1.0-dev"

// Run starts the Dragon Sync terminal surface. Product actions will be wired
// into this boundary after the visual shell is approved.
func Run() error {
	program := tui.NewProgram()
	_, err := program.Run()
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
