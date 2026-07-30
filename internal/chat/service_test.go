package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-sync/agent-sync/internal/db"
)

type recordedRunner struct{ requests []RunRequest }

func (r *recordedRunner) Run(_ context.Context, request RunRequest) (RunResult, error) {
	r.requests = append(r.requests, request)
	return RunResult{Reply: "reply from " + string(request.Agent), NativeSessionID: string(request.Agent) + "-native"}, nil
}

func TestSwitchSendsOnlyUnseenWorkToReturningAgent(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "dragon-sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	runner := &recordedRunner{}
	service := NewService(database, runner)

	conversation, err := service.Start(t.TempDir(), AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Send(context.Background(), conversation.ID, "tool one user message"); err != nil {
		t.Fatal(err)
	}
	conversation, err = service.Switch(conversation.ID, AgentCodex)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Send(context.Background(), conversation.ID, "tool two user message"); err != nil {
		t.Fatal(err)
	}
	conversation, err = service.Switch(conversation.ID, AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Send(context.Background(), conversation.ID, "continue from there"); err != nil {
		t.Fatal(err)
	}

	if got := len(runner.requests); got != 3 {
		t.Fatalf("got %d calls, want 3", got)
	}
	firstTransfer := runner.requests[1].Prompt
	if !strings.Contains(firstTransfer, "tool one user message") || !strings.Contains(firstTransfer, "reply from claude") {
		t.Fatalf("new agent did not receive the first agent's work: %q", firstTransfer)
	}
	returnTransfer := runner.requests[2].Prompt
	if !strings.Contains(returnTransfer, "tool two user message") || !strings.Contains(returnTransfer, "reply from codex") {
		t.Fatalf("returning agent did not receive foreign work: %q", returnTransfer)
	}
	if strings.Contains(returnTransfer, "tool one user message") || strings.Contains(returnTransfer, "reply from claude") {
		t.Fatalf("returning agent received work it had already seen: %q", returnTransfer)
	}
	if got, want := runner.requests[2].NativeSessionID, "claude-native"; got != want {
		t.Fatalf("got resumed session %q, want %q", got, want)
	}
}

func TestConsecutiveMessagesDoNotCreateHandoff(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "dragon-sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	runner := &recordedRunner{}
	service := NewService(database, runner)
	conversation, err := service.Start(t.TempDir(), AgentGemini)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Send(context.Background(), conversation.ID, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Send(context.Background(), conversation.ID, "second"); err != nil {
		t.Fatal(err)
	}
	if got := runner.requests[1].Prompt; got != "second" {
		t.Fatalf("got prompt %q, want direct second message", got)
	}
}

func TestSwitchAddsVisibleBannerButDoesNotTransferIt(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "dragon-sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	runner := &recordedRunner{}
	service := NewService(database, runner)
	conversation, err := service.Start(t.TempDir(), AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Send(context.Background(), conversation.ID, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Switch(conversation.ID, AgentCodex); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Send(context.Background(), conversation.ID, "second"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(runner.requests[1].Prompt, "ACTIVE AGENT") {
		t.Fatalf("switch banner must not be sent as agent context: %q", runner.requests[1].Prompt)
	}
	messages, err := service.Messages(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range messages {
		if message.Role == "system" && strings.Contains(message.Content, "ACTIVE AGENT: CODEX") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected visible CODEX switch banner")
	}
}

func TestDurableQueueExcludesLaterQueuedMessagesFromCurrentPrompt(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "dragon-sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	runner := &recordedRunner{}
	service := NewService(database, runner)
	conversation, err := service.Start(t.TempDir(), AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := service.Enqueue(conversation.ID, "first queued message")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := service.Enqueue(conversation.ID, "second queued message")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunQueueItem(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if got := runner.requests[0].Prompt; got != "first queued message" {
		t.Fatalf("first prompt included later queue data: %q", got)
	}
	active, err := service.Queue(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != second.ID || active[0].Status != QueueQueued {
		t.Fatalf("unexpected active queue: %+v", active)
	}
	if _, err := service.RunQueueItem(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
	active, err = service.Queue(conversation.ID)
	if err != nil || len(active) != 0 {
		t.Fatalf("queue was not drained: %+v, %v", active, err)
	}
}

func TestNewServiceRecoversInterruptedQueueItem(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "dragon-sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store := NewStore(database)
	conversation, err := store.Create(t.TempDir(), AgentCodex)
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := store.Enqueue(conversation.ID, AgentCodex, "survive restart")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BeginQueueItem(item.ID); err != nil {
		t.Fatal(err)
	}
	service := NewService(database, &recordedRunner{})
	queue, err := service.Queue(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 || queue[0].Status != QueueQueued {
		t.Fatalf("interrupted queue was not recovered: %+v", queue)
	}
}
