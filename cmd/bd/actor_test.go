package main

import (
	"strings"
	"testing"
)

// TestExecutorRefusal_NamesWhoMayAct covers R2's actor-gating class: the
// refusal must state who may perform the act, not just "refused".
func TestExecutorRefusal_NamesWhoMayAct(t *testing.T) {
	err := executorRefusal("executor:tester")
	if err == nil {
		t.Fatalf("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "BD_ACTOR=executor") {
		t.Fatalf("refusal should name the actor, got %q", msg)
	}
	if !strings.Contains(msg, "coordinator") || !strings.Contains(msg, "owner") {
		t.Fatalf("refusal should state who may perform the act (coordinator or owner), got %q", msg)
	}
	if msg == "BD_ACTOR=executor: refused" {
		t.Fatalf("refusal must be more than a bare 'refused'")
	}
}
