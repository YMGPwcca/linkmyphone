package features

import (
	"testing"
)

func TestFindRejectsUnknownFeature(t *testing.T) {
	if _, err := Find("linkmyphone.missing"); err == nil {
		t.Fatal("expected unknown feature error")
	}
}
