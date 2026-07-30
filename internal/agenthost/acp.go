// Package agenthost owns long-lived local agent processes. It speaks
// newline-delimited JSON-RPC directly so Dragon Sync stays independent of
// editor-specific ACP SDKs.
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
	"time"
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

type RequestHandler func(context.Context, string, json.RawMessage) (any, error)

type ACPHost struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	cancel context.CancelFunc

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	nextID    atomic.Int64
	pending   map[int64]chan rpcResponse

	handlerMu sync.RWMutex
	handler   RequestHandler

	events       chan Event
	done         chan error
	capabilities json.RawMessage
	closeOnce    sync.Once
	readers      sync.WaitGroup
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

func Start(parent context.Context, kind Kind, cwd string) (*ACPHost, error) {
	command, args, err := commandFor(kind)
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(lifetime, command, args...)
	cmd.Dir = cwd
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	host := &ACPHost{
		cmd:     cmd,
		stdin:   stdin,
		cancel:  cancel,
		pending: map[int64]chan rpcResponse{},
		events:  make(chan Event, 256),
		done:    make(chan error, 1),
	}
	host.readers.Add(2)
	go func() {
		defer host.readers.Done()
		host.read(stdout)
	}()
	go func() {
		defer host.readers.Done()
		host.readStderr(stderr)
	}()
	go func() {
		host.readers.Wait()
		close(host.events)
	}()
	go func() {
		err := cmd.Wait()
		host.failPending(err)
		host.done <- err
		close(host.done)
	}()
	if err := host.Initialize(parent); err != nil {
		_ = host.Close()
		return nil, err
	}
	return host, nil
}

func (h *ACPHost) Events() <-chan Event          { return h.events }
func (h *ACPHost) Done() <-chan error            { return h.done }
func (h *ACPHost) Capabilities() json.RawMessage { return cloneRaw(h.capabilities) }
func (h *ACPHost) SetRequestHandler(v RequestHandler) {
	h.handlerMu.Lock()
	h.handler = v
	h.handlerMu.Unlock()
}

func (h *ACPHost) Initialize(ctx context.Context) error {
	var result json.RawMessage
	err := h.Call(ctx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
		"clientInfo": map[string]string{"name": "Dragon Sync", "version": "0.1"},
	}, &result)
	if err == nil {
		h.capabilities = cloneRaw(result)
	}
	return err
}

func (h *ACPHost) NewSession(ctx context.Context, cwd string) (Session, error) {
	var session Session
	err := h.Call(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}, &session)
	return session, err
}

func (h *ACPHost) LoadSession(ctx context.Context, sessionID, cwd string) (Session, error) {
	params := map[string]any{"sessionId": sessionID, "cwd": cwd, "mcpServers": []any{}}
	var session Session
	if err := h.Call(ctx, "session/load", params, &session); err == nil {
		if session.ID == "" {
			session.ID = sessionID
		}
		return session, nil
	}
	// Newer agents may expose resume instead of load.
	if err := h.Call(ctx, "session/resume", params, &session); err != nil {
		return Session{}, err
	}
	if session.ID == "" {
		session.ID = sessionID
	}
	return session, nil
}

func (h *ACPHost) Prompt(ctx context.Context, sessionID, text string) error {
	_, err := h.PromptResult(ctx, sessionID, text)
	return err
}

func (h *ACPHost) PromptResult(ctx context.Context, sessionID, text string) (json.RawMessage, error) {
	var result json.RawMessage
	err := h.Call(ctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []map[string]string{{"type": "text", "text": text}},
	}, &result)
	return result, err
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

func (h *ACPHost) Call(ctx context.Context, method string, params, result any) error {
	id := h.nextID.Add(1)
	response := make(chan rpcResponse, 1)
	h.pendingMu.Lock()
	h.pending[id] = response
	h.pendingMu.Unlock()
	if err := h.write(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		h.remove(id)
		return err
	}
	select {
	case received := <-response:
		if received.Error != nil {
			return fmt.Errorf("%s: %s", method, received.Error.Message)
		}
		if result != nil && len(received.Result) > 0 && string(received.Result) != "null" {
			return json.Unmarshal(received.Result, result)
		}
		return nil
	case <-ctx.Done():
		h.remove(id)
		return ctx.Err()
	}
}

func (h *ACPHost) Close() error {
	var result error
	h.closeOnce.Do(func() {
		_ = h.stdin.Close()
		select {
		case result = <-h.done:
		case <-time.After(750 * time.Millisecond):
			if h.cmd.Process != nil {
				result = h.cmd.Process.Kill()
			}
		}
		h.cancel()
	})
	return result
}

func (h *ACPHost) read(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var message envelope
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		if hasID(message.ID) && message.Method == "" {
			var id int64
			if json.Unmarshal(message.ID, &id) == nil {
				h.deliver(id, rpcResponse{JSONRPC: message.JSONRPC, ID: id, Result: message.Result, Error: message.Error})
			}
			continue
		}
		if message.Method == "" {
			continue
		}
		if hasID(message.ID) {
			go h.handleRequest(message)
			continue
		}
		h.emit(Event{SessionID: sessionIDFrom(message.Params), Method: message.Method, Params: message.Params})
	}
}

func (h *ACPHost) readStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 16*1024), 1024*1024)
	for scanner.Scan() {
		params, _ := json.Marshal(map[string]string{"message": scanner.Text()})
		h.emit(Event{Method: "agent/stderr", Params: params})
	}
}

func (h *ACPHost) handleRequest(message envelope) {
	h.handlerMu.RLock()
	handler := h.handler
	h.handlerMu.RUnlock()
	var result any
	var err error
	if handler == nil {
		err = fmt.Errorf("Dragon Sync does not implement %s", message.Method)
	} else {
		result, err = handler(context.Background(), message.Method, message.Params)
	}
	response := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(message.ID)}
	if err != nil {
		response["error"] = rpcError{Code: -32601, Message: err.Error()}
	} else {
		if result == nil {
			result = map[string]any{}
		}
		response["result"] = result
	}
	_ = h.write(response)
}

func (h *ACPHost) emit(event Event) {
	select {
	case h.events <- event:
	default:
		// Runtime state updates are lossy; request/response traffic is not.
	}
}

func (h *ACPHost) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	_, err = h.stdin.Write(append(data, '\n'))
	return err
}

func (h *ACPHost) deliver(id int64, response rpcResponse) {
	h.pendingMu.Lock()
	ch := h.pending[id]
	delete(h.pending, id)
	h.pendingMu.Unlock()
	if ch != nil {
		ch <- response
	}
}

func (h *ACPHost) remove(id int64) {
	h.pendingMu.Lock()
	delete(h.pending, id)
	h.pendingMu.Unlock()
}

func (h *ACPHost) failPending(cause error) {
	if cause == nil {
		cause = io.EOF
	}
	h.pendingMu.Lock()
	pending := h.pending
	h.pending = map[int64]chan rpcResponse{}
	h.pendingMu.Unlock()
	for id, ch := range pending {
		ch <- rpcResponse{ID: id, Error: &rpcError{Code: -32000, Message: cause.Error()}}
	}
}

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

func hasID(id json.RawMessage) bool {
	return len(id) > 0 && string(id) != "null"
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}

func sessionIDFrom(raw json.RawMessage) string {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	for _, key := range []string{"sessionId", "session_id"} {
		if id, ok := value[key].(string); ok {
			return id
		}
	}
	return ""
}
