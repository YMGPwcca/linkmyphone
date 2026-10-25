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
