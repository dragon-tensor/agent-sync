package agenthost

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCommandForSupportedAgents(t *testing.T) {
	command, args, err := commandFor(OpenCode)
	if err != nil || command != "opencode" || len(args) != 1 || args[0] != "acp" {
		t.Fatalf("got %q %q %v", command, args, err)
	}
	command, args, err = commandFor(Gemini)
	if err != nil || command != "gemini" || len(args) != 1 || args[0] != "--acp" {
		t.Fatalf("got %q %q %v", command, args, err)
	}
}

func TestAgentRequestIsAnsweredWithOriginalRPCID(t *testing.T) {
	writer := &channelWriteCloser{writes: make(chan []byte, 1)}
	host := &ACPHost{
		stdin:   writer,
		pending: map[int64]chan rpcResponse{},
		events:  make(chan Event, 1),
	}
	host.SetRequestHandler(func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method != "session/request_permission" {
			t.Fatalf("unexpected method %q", method)
		}
		return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "allow"}}, nil
	})
	host.read(strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"session/request_permission","params":{"sessionId":"s1"}}` + "\n"))
	select {
	case raw := <-writer.writes:
		var response map[string]any
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		if response["id"] != float64(7) {
			t.Fatalf("response lost request id: %s", raw)
		}
	case <-time.After(time.Second):
		t.Fatal("agent request was not answered")
	}
}

type channelWriteCloser struct{ writes chan []byte }

func (w *channelWriteCloser) Write(value []byte) (int, error) {
	w.writes <- append([]byte(nil), value...)
	return len(value), nil
}

func (*channelWriteCloser) Close() error { return nil }
