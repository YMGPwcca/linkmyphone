package main

import "testing"

func TestShortID(t *testing.T) {
	if got := shortID("12345678-aaaa-bbbb-cccc-123456789012"); got != "12345678...9012" {
		t.Fatalf("shortID=%q", got)
	}
	if got := shortID("short"); got != "short" {
		t.Fatalf("shortID=%q", got)
	}
}
