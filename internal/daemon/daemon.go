package daemon

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	agentsync "github.com/agent-sync/agent-sync/internal/sync"
	"github.com/agent-sync/agent-sync/pkg/types"
	"github.com/fsnotify/fsnotify"
)

type Daemon struct {
	registry     *agentsync.Registry
	db           *db.DB
	interval     time.Duration
	watchPaths   []string
	stopCh       chan struct{}
	doneCh       chan struct{}
	onSync       func(provider string, stats *types.SyncStats)
	onError      func(err error)
}

type Option func(*Daemon)

func WithInterval(d time.Duration) Option {
	return func(dd *Daemon) {
		dd.interval = d
	}
}

func WithWatchPaths(paths []string) Option {
	return func(dd *Daemon) {
		dd.watchPaths = paths
	}
}

func WithSyncCallback(fn func(provider string, stats *types.SyncStats)) Option {
	return func(dd *Daemon) {
		dd.onSync = fn
	}
}

func WithErrorCallback(fn func(err error)) Option {
	return func(dd *Daemon) {
		dd.onError = fn
	}
}

func New(registry *agentsync.Registry, database *db.DB, opts ...Option) *Daemon {
	d := &Daemon{
		registry:   registry,
		db:         database,
		interval:   30 * time.Minute,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		onSync:     func(provider string, stats *types.SyncStats) {},
		onError:    func(err error) { log.Printf("daemon error: %v", err) },
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

func (d *Daemon) Start() {
	go d.runLoop()
}

func (d *Daemon) Stop() {
	close(d.stopCh)
	<-d.doneCh
}

func (d *Daemon) runLoop() {
	defer close(d.doneCh)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if len(d.watchPaths) > 0 {
		go d.watchFiles()
	}

	d.syncAll()

	for {
		select {
		case <-ticker.C:
			d.syncAll()
		case <-d.stopCh:
			return
		case sig := <-sigCh:
			log.Printf("daemon received signal %v, stopping", sig)
			return
		}
	}
}

func (d *Daemon) syncAll() {
	providers := d.registry.List()
	for _, p := range providers {
		ok, err := p.Detect()
		if err != nil || !ok {
			continue
		}
		stats, err := p.Sync()
		if err != nil {
			d.onError(fmt.Errorf("%s: %w", p.Name(), err))
			continue
		}
		d.onSync(p.Name(), stats)
	}
}

func (d *Daemon) watchFiles() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		d.onError(fmt.Errorf("file watcher: %w", err))
		return
	}
	defer watcher.Close()

	for _, path := range d.watchPaths {
		if err := watcher.Add(path); err != nil {
			d.onError(fmt.Errorf("watch %s: %w", path, err))
			continue
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
				if err != nil || !fi.IsDir() {
					return nil
				}
				watcher.Add(p)
				return nil
			})
		}
	}

	debounce := make(map[string]time.Time)
	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
				debounce[event.Name] = time.Now()
				debounceTimer.Reset(5 * time.Second)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			d.onError(fmt.Errorf("watch error: %w", err))
		case <-debounceTimer.C:
			now := time.Now()
			for path, t := range debounce {
				if now.Sub(t) >= 5*time.Second {
					delete(debounce, path)
				}
			}
			if len(debounce) > 0 {
				debounceTimer.Reset(5 * time.Second)
			}
		}
	}
}

func WriteSystemdService(binPath, dataDir string) string {
	return fmt.Sprintf(`[Unit]
Description=agent-sync daemon — AI chat history sync service
After=network.target

[Service]
Type=simple
ExecStart=%s daemon
Restart=on-failure
RestartSec=10
Environment=AGENT_SYNC_DATA_DIR=%s

[Install]
WantedBy=default.target
`, binPath, dataDir)
}

func WriteLaunchdPlist(binPath, dataDir, label string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>daemon</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>AGENT_SYNC_DATA_DIR</key>
        <string>%s</string>
    </dict>
    <key>KeepAlive</key>
    <true/>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/agent-sync-daemon.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/agent-sync-daemon.log</string>
</dict>
</plist>`, label, binPath, dataDir)
}

func WriteCrontab(binPath, interval string) string {
	return fmt.Sprintf(`# agent-sync auto-sync — added by agent-sync daemon install
%s %s sync
`, interval, binPath)
}

func DefaultInterval() time.Duration {
	return 30 * time.Minute
}
