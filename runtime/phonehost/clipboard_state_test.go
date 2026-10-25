package phonehost

import (
	"testing"
)

func TestSessionClipboardStateTracksChanges(t *testing.T) {
	s := &Session{}
	enabled, changed := s.ClipboardState()
	if enabled {
		t.Fatal("clipboard starts enabled")
	}
	s.SetClipboardEnabled(true)
	select {
	case <-changed:
	default:
		t.Fatal("clipboard state change was not signaled")
	}
	enabled, changed = s.ClipboardState()
	if !enabled {
		t.Fatal("clipboard was not enabled")
	}
	s.SetClipboardEnabled(true)
	select {
	case <-changed:
		t.Fatal("unchanged clipboard state was signaled")
	default:
	}
	s.SetClipboardEnabled(false)
	select {
	case <-changed:
	default:
		t.Fatal("clipboard disable was not signaled")
	}
}

func TestSessionClipboardReadyUsesCurrentConnectionState(t *testing.T) {
	s := &Session{}
	s.SetClipboardEnabled(true)
	_, firstState := s.ClipboardState()
	firstReady := s.ClipboardReady()
	select {
	case <-firstReady:
		t.Fatal("clipboard ready before phone connection")
	default:
	}
	s.MarkClipboardConnected(firstState)
	select {
	case <-firstReady:
	default:
		t.Fatal("phone connection did not make clipboard ready")
	}

	s.SetClipboardEnabled(false)
	s.SetClipboardEnabled(true)
	_, currentState := s.ClipboardState()
	currentReady := s.ClipboardReady()
	s.MarkClipboardConnected(firstState)
	select {
	case <-currentReady:
		t.Fatal("stale phone connection made new clipboard generation ready")
	default:
	}
	s.MarkClipboardConnected(currentState)
	select {
	case <-currentReady:
	default:
		t.Fatal("current phone connection did not make clipboard ready")
	}
}
