// Package agenthost owns long-lived local agent processes. It deliberately
// speaks newline-delimited JSON-RPC directly so Dragon Sync stays independent
// of editor-specific ACP SDKs.
package agenthost

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

type Kind string

const (
	OpenCode Kind = "opencode"
	Gemini   Kind = "gemini"
)

type Event struct {
	SessionID string
	Method    string
	Params    json.RawMessage
}

type ACPHost struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	mu      sync.Mutex
	nextID  atomic.Int64
	pending map[int64]chan rpcResponse
	events  chan Event
	done    chan error
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type Session struct {
	ID            string          `json:"sessionId"`
	Modes         json.RawMessage `json:"modes"`
	ConfigOptions json.RawMessage `json:"configOptions"`
	Raw           json.RawMessage `json:"-"`
}

func Start(ctx context.Context, kind Kind, cwd string) (*ACPHost, error) {
	command, args, err := commandFor(kind)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = cwd
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	host := &ACPHost{cmd: cmd, stdin: stdin, pending: map[int64]chan rpcResponse{}, events: make(chan Event, 128), done: make(chan error, 1)}
	go host.read(stdout)
	go func() { host.done <- cmd.Wait(); close(host.events) }()
	if err := host.Initialize(ctx); err != nil {
		_ = host.Close()
		return nil, err
	}
	return host, nil
}

func (h *ACPHost) Events() <-chan Event { return h.events }
func (h *ACPHost) Done() <-chan error   { return h.done }

func (h *ACPHost) Initialize(ctx context.Context) error {
	var result json.RawMessage
	return h.Call(ctx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
		"clientInfo": map[string]string{"name": "Dragon Sync", "version": "0.1"},
	}, &result)
}

func (h *ACPHost) NewSession(ctx context.Context, cwd string) (Session, error) {
	var session Session
	err := h.Call(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}, &session)
	return session, err
}

func (h *ACPHost) Prompt(ctx context.Context, sessionID, text string) error {
	var result json.RawMessage
	return h.Call(ctx, "session/prompt", map[string]any{"sessionId": sessionID, "prompt": []map[string]string{{"type": "text", "text": text}}}, &result)
}

func (h *ACPHost) Cancel(sessionID string) error {
	return h.notify("session/cancel", map[string]string{"sessionId": sessionID})
}

func (h *ACPHost) SetMode(ctx context.Context, sessionID, mode string) error {
	var result json.RawMessage
	return h.Call(ctx, "session/set_mode", map[string]string{"sessionId": sessionID, "modeId": mode}, &result)
}

func (h *ACPHost) SetConfigOption(ctx context.Context, sessionID, optionID string, value any) error {
	var result json.RawMessage
	return h.Call(ctx, "session/set_config_option", map[string]any{"sessionId": sessionID, "configId": optionID, "value": value}, &result)
}

func (h *ACPHost) Call(ctx context.Context, method string, params any, result any) error {
	id := h.nextID.Add(1)
	response := make(chan rpcResponse, 1)
	h.mu.Lock()
	h.pending[id] = response
	h.mu.Unlock()
	if err := h.write(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		h.remove(id)
		return err
	}
	select {
	case received := <-response:
		if received.Error != nil {
			return fmt.Errorf("%s: %s", method, received.Error.Message)
		}
		if result != nil && len(received.Result) > 0 {
			return json.Unmarshal(received.Result, result)
		}
		return nil
	case <-ctx.Done():
		h.remove(id)
		return ctx.Err()
	}
}

func (h *ACPHost) Close() error {
	_ = h.stdin.Close()
	if h.cmd.Process != nil {
		return h.cmd.Process.Kill()
	}
	return nil
}

func (h *ACPHost) read(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var message envelope
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		if len(message.ID) > 0 && message.Method == "" {
			var id int64
			if json.Unmarshal(message.ID, &id) == nil {
				h.deliver(id, rpcResponse{JSONRPC: message.JSONRPC, ID: id, Result: message.Result, Error: message.Error})
			}
			continue
		}
		if message.Method != "" {
			h.events <- Event{Method: message.Method, Params: message.Params}
		}
	}
}

func (h *ACPHost) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err = h.stdin.Write(append(data, '\n'))
	return err
}
func (h *ACPHost) deliver(id int64, response rpcResponse) {
	h.mu.Lock()
	ch := h.pending[id]
	delete(h.pending, id)
	h.mu.Unlock()
	if ch != nil {
		ch <- response
	}
}
func (h *ACPHost) remove(id int64) { h.mu.Lock(); delete(h.pending, id); h.mu.Unlock() }

func (h *ACPHost) notify(method string, params any) error {
	return h.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func commandFor(kind Kind) (string, []string, error) {
	switch kind {
	case OpenCode:
		return "opencode", []string{"acp"}, nil
	case Gemini:
		return "gemini", []string{"--acp"}, nil
	default:
		return "", nil, fmt.Errorf("%q does not support ACP", kind)
	}
}
