//go:build !windows

package agenthost

import (
	"strings"
	"testing"
	"time"
)

func TestPTYHostRendersOutputAndForwardsInput(t *testing.T) {
	host, err := StartPTY("sh", []string{"-c", "printf 'READY\\n'; read line; printf 'ECHO:%s\\n' \"$line\"; sleep 1"}, t.TempDir(), 60, 12)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Close() })
	waitForSnapshot(t, host, "READY")
	if err := host.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	waitForSnapshot(t, host, "ECHO:hello")
}

func waitForSnapshot(t *testing.T, host *PTYHost, expected string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(host.Snapshot(), expected) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("terminal did not render %q; snapshot:\n%s", expected, host.Snapshot())
}
