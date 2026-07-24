package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/pkg/types"
)

type Plugin interface {
	Name() string
	Type() types.ProviderType
	Detect(path string) (bool, error)
	Sync(db *db.DB, stats *types.SyncStats) error
	ListSessions() ([]*types.Session, error)
	GetSessionMessages(sessionID string) ([]*types.Message, error)
}

type PluginManifest struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Executable  string `json:"executable"`
	Enabled     bool   `json:"enabled"`
	InstalledAt string `json:"installed_at"`
}

type PluginRegistry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
	dir     string
}

func NewRegistry(pluginDir string) *PluginRegistry {
	return &PluginRegistry{
		plugins: make(map[string]Plugin),
		dir:     pluginDir,
	}
}

func (r *PluginRegistry) Register(p Plugin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[string(p.Type())] = p
}

func (r *PluginRegistry) Get(pt types.ProviderType) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[string(pt)]
	return p, ok
}

func (r *PluginRegistry) List() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var list []Plugin
	for _, p := range r.plugins {
		list = append(list, p)
	}
	return list
}

func (r *PluginRegistry) ScanDir() error {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !isExecutable(e.Name()) {
			continue
		}
		fpath := filepath.Join(r.dir, e.Name())
		manifest := r.probePlugin(fpath)
		if manifest == nil {
			continue
		}
		sp := &SubprocessPlugin{
			execPath: fpath,
			name:     manifest.Name,
			ptype:    types.ProviderType(manifest.Type),
		}
		r.Register(sp)
	}
	return nil
}

func (r *PluginRegistry) Install(execPath string) (*PluginManifest, error) {
	manifest := r.probePlugin(execPath)
	if manifest == nil {
		return nil, fmt.Errorf("not a valid agent-sync plugin: %s", execPath)
	}
	dest := filepath.Join(r.dir, filepath.Base(execPath))
	data, err := os.ReadFile(execPath)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(dest, data, 0755); err != nil {
		return nil, err
	}
	manifest.Executable = dest
	manifest.InstalledAt = time.Now().Format(time.RFC3339)
	return manifest, nil
}

func ProbePlugin(path string) *PluginManifest {
	r := &PluginRegistry{}
	return r.probePlugin(path)
}

func (r *PluginRegistry) probePlugin(path string) *PluginManifest {
	cmd := exec.Command(path, "--manifest")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var manifest PluginManifest
	if err := json.Unmarshal(out, &manifest); err != nil {
		return nil
	}
	if manifest.Name == "" || manifest.Type == "" {
		return nil
	}
	return &manifest
}

func isExecutable(name string) bool {
	info, err := os.Stat(name)
	if err != nil {
		return false
	}
	return info.Mode()&0111 != 0
}

type SubprocessPlugin struct {
	execPath string
	name     string
	ptype    types.ProviderType
}

func (p *SubprocessPlugin) Name() string { return p.name }

func (p *SubprocessPlugin) Type() types.ProviderType { return p.ptype }

func (p *SubprocessPlugin) call(method string, args map[string]interface{}) (json.RawMessage, error) {
	payload := map[string]interface{}{
		"method": method,
		"args":   args,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(p.execPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	go func() {
		stdin.Write(data)
		stdin.Close()
	}()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("plugin %s %s: %w", p.name, method, err)
	}
	return out, nil
}

func (p *SubprocessPlugin) Detect(rawpath string) (bool, error) {
	out, err := p.call("detect", map[string]interface{}{"path": rawpath})
	if err != nil {
		return false, err
	}
	var result struct {
		Detected bool   `json:"detected"`
		Error    string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return false, err
	}
	if result.Error != "" {
		return false, fmt.Errorf("plugin error: %s", result.Error)
	}
	return result.Detected, nil
}

func (p *SubprocessPlugin) Sync(database *db.DB, stats *types.SyncStats) error {
	prov, err := database.GetProvider(string(p.ptype))
	if err != nil {
		return err
	}
	out, err := p.call("sync", map[string]interface{}{
		"provider_id": prov.ID,
		"path":        prov.Path,
	})
	if err != nil {
		return err
	}
	var result struct {
		Stats types.SyncStats `json:"stats"`
		Error string          `json:"error,omitempty"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return err
	}
	if result.Error != "" {
		return fmt.Errorf("plugin error: %s", result.Error)
	}
	*stats = result.Stats
	return nil
}

func (p *SubprocessPlugin) ListSessions() ([]*types.Session, error) {
	out, err := p.call("list_sessions", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Sessions []*types.Session `json:"sessions"`
		Error    string           `json:"error,omitempty"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}
	if result.Error != "" {
		return nil, fmt.Errorf("plugin error: %s", result.Error)
	}
	return result.Sessions, nil
}

func (p *SubprocessPlugin) GetSessionMessages(sessionID string) ([]*types.Message, error) {
	out, err := p.call("get_session_messages", map[string]interface{}{
		"session_id": sessionID,
	})
	if err != nil {
		return nil, err
	}
	var result struct {
		Messages []*types.Message `json:"messages"`
		Error    string           `json:"error,omitempty"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}
	if result.Error != "" {
		return nil, fmt.Errorf("plugin error: %s", result.Error)
	}
	return result.Messages, nil
}

func (r *PluginRegistry) ListManifests() []PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var manifests []PluginManifest
	for _, p := range r.plugins {
		sp, ok := p.(*SubprocessPlugin)
		if !ok {
			continue
		}
		manifests = append(manifests, PluginManifest{
			Name:       sp.name,
			Type:       string(sp.ptype),
			Executable: sp.execPath,
			Enabled:    true,
		})
	}
	return manifests
}

var adapterPlugins []Plugin

func RegisterAdapterPlugin(p Plugin) {
	adapterPlugins = append(adapterPlugins, p)
}

func AdapterPlugins() []Plugin {
	return adapterPlugins
}

func init() {
	home, _ := os.UserHomeDir()
	defaultDir := filepath.Join(home, ".agent-sync", "plugins")
	os.MkdirAll(defaultDir, 0755)
}

func DefaultPluginDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agent-sync", "plugins")
}

type WasmPlugin struct {
	wasmPath string
	name     string
	ptype    types.ProviderType
}

func NewWasmPlugin(wasmPath string) (*WasmPlugin, error) {
	return &WasmPlugin{wasmPath: wasmPath}, nil
}

func (p *WasmPlugin) Name() string { return p.name }

func (p *WasmPlugin) Type() types.ProviderType { return p.ptype }

func (p *WasmPlugin) Detect(path string) (bool, error) {
	return false, fmt.Errorf("wasm plugins require wasmtime runtime: not yet implemented")
}

func (p *WasmPlugin) Sync(database *db.DB, stats *types.SyncStats) error {
	return fmt.Errorf("wasm plugins require wasmtime runtime: not yet implemented")
}

func (p *WasmPlugin) ListSessions() ([]*types.Session, error) {
	return nil, fmt.Errorf("wasm plugins require wasmtime runtime: not yet implemented")
}

func (p *WasmPlugin) GetSessionMessages(sessionID string) ([]*types.Message, error) {
	return nil, fmt.Errorf("wasm plugins require wasmtime runtime: not yet implemented")
}

func PluginCLIDescription() string {
	return `Manage agent-sync plugins.

Plugins are standalone executables that implement the agent-sync adapter protocol.
They communicate via JSON over stdin/stdout.

Commands:
  list          List installed plugins
  install <path>  Install a plugin from an executable path
  probe <path>    Probe an executable to check if it's a valid plugin

Usage:
  agent-sync plugin [command]`
}

func PluginListCommand(reg *PluginRegistry) string {
	manifests := reg.ListManifests()
	if len(manifests) == 0 {
		return "No plugins installed.\n\nInstall plugins from the registry or build your own:\n  agent-sync plugin install <path>"
	}
	var lines []string
	lines = append(lines, "Installed plugins:")
	for _, m := range manifests {
		status := "✓"
		if !m.Enabled {
			status = "✗"
		}
		lines = append(lines, fmt.Sprintf("  %s %s (%s)", status, m.Name, m.Type))
		lines = append(lines, fmt.Sprintf("     %s", m.Executable))
	}
	return strings.Join(lines, "\n")
}
