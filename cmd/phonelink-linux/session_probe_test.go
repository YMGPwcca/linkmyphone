package main

import (
	"reflect"
	"testing"

	"github.com/YMGPwcca/phonelink-linux/protocol/platform"
	sessionproto "github.com/YMGPwcca/phonelink-linux/protocol/sessionvalidation"
)

func TestSessionCapabilityNames(t *testing.T) {
	got := sessionCapabilityNames([]sessionproto.Capability{
		sessionproto.CapabilitySessionValidation,
		sessionproto.CapabilityPersistentMessageChannel,
	})
	want := []string{"SessionValidation", "PersistentMessageChannel"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestPlatformHeaderNamesSortedAndUnique(t *testing.T) {
	got := platformHeaderNames([]platform.Header{
		{Key: "_route", Value: "/internal/response"},
		{Key: "ms-content-type", Value: "application/x-binary"},
		{Key: "_route", Value: "/internal/response"},
	})
	want := []string{"_route", "ms-content-type"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}
