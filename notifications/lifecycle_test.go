package notifications

import (
	"context"
	"strings"
	"testing"
)

func TestDesktopLossKeepsStateAndRebindReplaysSilently(t *testing.T) {
	c, native, _ := newFixtureClient(t)
	apply(t, c, 1, item("one", "before restart", 10))
	oldRevision := c.records["one"].revision
	if err := c.resetDesktop(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	apply(t, c, 1, item("two", "while absent", 20))
	if native.renderCount != 1 {
		t.Fatal("service loss attempted to render through absent owner")
	}
	if err := c.resetDesktop(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if native.renderCount != 3 {
		t.Fatalf("rebind did not replay both visible/pending records: %d", native.renderCount)
	}
	if c.records["one"].revision == oldRevision {
		t.Fatal("service restart retained stale desktop action generation")
	}
	for _, notification := range native.visible {
		if notification.Body == "while absent" && !notification.Silent {
			t.Fatal("service replay re-alerted a recovered item")
		}
	}
}

func TestNativeOverflowReportsFailureInsteadOfDroppingUserAction(t *testing.T) {
	backend := &desktopBackend{events: make(chan NativeEvent, 1)}
	backend.emit(NativeEvent{Kind: NativeAction, ID: 1, Action: "old"})
	backend.emit(NativeEvent{Kind: NativeClosed, ID: 2, Reason: 2})
	event := <-backend.events
	if event.Kind != NativeFailure || event.Err == nil {
		t.Fatalf("overflow silently continued: %#v", event)
	}
}

func TestReplyOutputEscapingFitsAcceptedTextBudget(t *testing.T) {
	// Control characters need six JSON bytes each; the accepted UTF-8 text
	// budget and serialized output budget must remain distinct.
	escaped := strings.Repeat(`\u0001`, maxReplyTextBytes)
	if len(escaped)+len(`{"submitted":true,"text":""}`) > maxReplyOutputJSONBytes {
		t.Fatal("accepted reply exceeds escaped output budget")
	}
}

func TestQueuedMutationRejectsSupersededRevision(t *testing.T) {
	c, _, _ := newFixtureClient(t)
	c.ready = true
	apply(t, c, 1, item("one", "original", 10))
	guard := mutationGuard{key: "one", revision: c.records["one"].revision}
	apply(t, c, 1, item("one", "updated", 11))
	if err := c.checkGuards([]mutationGuard{guard}); err != ErrStaleAction {
		t.Fatalf("queued stale action guard=%v", err)
	}
}
