package controlplane

import (
	"testing"
	"time"
)

func TestSwitchHandlerWaitsForOldGeneration(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	switcher := NewSwitchHandler(HandlerFunc(func(Request) Response {
		close(started)
		<-release
		response := Success()
		response.Error = "old generation"
		return response
	}))
	handled := make(chan Response, 1)
	go func() { handled <- switcher.HandleControl(Request{}) }()
	<-started
	switched := make(chan struct{})
	go func() {
		switcher.Set(HandlerFunc(func(Request) Response { response := Success(); response.Error = "new generation"; return response }))
		close(switched)
	}()
	select {
	case <-switched:
		t.Fatal("switched while old handler could still mutate state")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	select {
	case <-switched:
	case <-time.After(time.Second):
		t.Fatal("handoff did not complete")
	}
	if got := <-handled; got.Error != "old generation" {
		t.Fatal("old handler did not finish")
	}
	if got := switcher.HandleControl(Request{}); got.Error != "new generation" {
		t.Fatal("new handler not installed")
	}
}
