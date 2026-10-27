package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticLogClearRemovesCurrentAndRotatedFiles(t *testing.T) {
	log := newDiagnosticLog(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(log.path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".2", ".1", ""} {
		name := log.path + suffix
		if err := os.WriteFile(name, []byte("old diagnostic "+suffix), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.clear(); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".2", ".1", ""} {
		name := log.path + suffix
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("diagnostic file %q remains after clear: %v", name, err)
		}
	}
	if err := log.write(diagnosticEvent{Level: "info", Event: "fresh_after_clear", Message: "new session"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(log.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "fresh_after_clear") || strings.Contains(string(body), "old diagnostic") {
		t.Fatalf("diagnostics did not restart cleanly after clear: %s", body)
	}
}
