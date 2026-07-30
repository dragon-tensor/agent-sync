package agenthost

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

// PTYHost runs an unmodified interactive CLI behind a virtual terminal. The
// PTY remains alive while users move between Dragon Sync agent tabs.
type PTYHost struct {
	cmd   *exec.Cmd
	file  *os.File
	state *vt10x.State
	vt    *vt10x.VT

	writeMu   sync.Mutex
	closeOnce sync.Once
	updates   chan struct{}
	done      chan error
}

func StartPTY(command string, args []string, cwd string, cols, rows int) (*PTYHost, error) {
	if cols < 20 {
		cols = 80
	}
	if rows < 8 {
		rows = 24
	}
	cmd := exec.Command(command, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, fmt.Errorf("start %s terminal: %w", command, err)
	}
	state := &vt10x.State{}
	terminal, err := vt10x.Create(state, file)
	if err != nil {
		_ = file.Close()
		_ = cmd.Process.Kill()
		return nil, err
	}
	terminal.Resize(cols, rows)
	host := &PTYHost{
		cmd:     cmd,
		file:    file,
		state:   state,
		vt:      terminal,
		updates: make(chan struct{}, 1),
		done:    make(chan error, 1),
	}
	go host.parse()
	go func() {
		err := cmd.Wait()
		host.done <- err
		close(host.done)
	}()
	return host, nil
}

func (h *PTYHost) Updates() <-chan struct{} { return h.updates }
func (h *PTYHost) Done() <-chan error       { return h.done }

func (h *PTYHost) SendText(value string) error {
	// Bracketed paste prevents newlines and slash commands from being
	// interpreted as keystrokes while the text is entering the native TUI.
	return h.Write([]byte("\x1b[200~" + value + "\x1b[201~\r"))
}

func (h *PTYHost) Write(value []byte) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	_, err := h.file.Write(value)
	return err
}

func (h *PTYHost) Cancel() error {
	// Both Claude Code and Codex use escape as their least destructive turn
	// interruption. Sending it twice mirrors Dragon Sync's own gesture.
	return h.Write([]byte{0x1b, 0x1b})
}

func (h *PTYHost) Resize(cols, rows int) error {
	if cols < 20 || rows < 8 {
		return nil
	}
	if err := pty.Setsize(h.file, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		return err
	}
	h.vt.Resize(cols, rows)
	return nil
}

func (h *PTYHost) Snapshot() string {
	value := h.state.String()
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " \t\x00")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func (h *PTYHost) Close() error {
	var result error
	h.closeOnce.Do(func() {
		_ = h.Write([]byte{0x03})
		_ = h.file.Close()
		select {
		case result = <-h.done:
		case <-time.After(750 * time.Millisecond):
			if h.cmd.Process != nil {
				result = h.cmd.Process.Kill()
			}
		}
	})
	return result
}

func (h *PTYHost) parse() {
	defer close(h.updates)
	for {
		if err := h.vt.Parse(); err != nil {
			if err != io.EOF && !strings.Contains(strings.ToLower(err.Error()), "input/output error") {
				select {
				case h.updates <- struct{}{}:
				default:
				}
			}
			return
		}
		select {
		case h.updates <- struct{}{}:
		default:
		}
	}
}
