package main

import (
	"strings"
	"testing"

	servicedcg "github.com/YMGPwcca/linkmyphone/services/dcg"
)

func TestSelectPeerDefaultsToSoleAndroid(t *testing.T) {
	yes := true
	got, err := selectPeer([]servicedcg.DeviceInfo{
		{ID: "self", IsLinked: &yes, OSName: "Windows", Name: "Linux"},
		{ID: "pc", IsLinked: &yes, OSName: "Windows", Name: "PC"},
		{ID: "phone", IsLinked: &yes, OSName: "Android", Name: "S23"},
	}, "self", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "phone" {
		t.Fatalf("got=%#v", got)
	}
}

func TestSelectPeerByName(t *testing.T) {
	yes := true
	got, err := selectPeer([]servicedcg.DeviceInfo{
		{ID: "phone", IsLinked: &yes, OSName: "Android", Name: "Pwcca's S23"},
		{ID: "pc", IsLinked: &yes, OSName: "Windows", Name: "PwccaPC"},
	}, "self", "s23")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "phone" {
		t.Fatalf("got=%#v", got)
	}
}

func TestSelectPeerRequiresSelectorForMultipleAndroids(t *testing.T) {
	yes := true
	_, err := selectPeer([]servicedcg.DeviceInfo{
		{ID: "phone-a", IsLinked: &yes, OSName: "Android", Name: "A"},
		{ID: "phone-b", IsLinked: &yes, OSName: "Android", Name: "B"},
	}, "self", "")
	if err == nil || !strings.Contains(err.Error(), "--target") {
		t.Fatalf("err=%v", err)
	}
}
