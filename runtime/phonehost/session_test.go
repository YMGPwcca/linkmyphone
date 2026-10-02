package phonehost

import (
	"testing"

	servicedcg "github.com/YMGPwcca/linkmyphone/services/dcg"
)

func TestSelectPeerPrefersSoleAndroid(t *testing.T) {
	yes := true
	devices := []servicedcg.DeviceInfo{
		{ID: "self", Name: "Linux", OSName: "Windows", IsLinked: &yes},
		{ID: "phone", Name: "S23", OSName: "Android", IsLinked: &yes},
		{ID: "pc", Name: "PC", OSName: "Windows", IsLinked: &yes},
	}
	got, err := SelectPeer(devices, "self", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "phone" {
		t.Fatalf("peer=%#v", got)
	}
}

func TestSelectPeerByPartialName(t *testing.T) {
	yes := true
	devices := []servicedcg.DeviceInfo{
		{ID: "self", Name: "Linux", IsLinked: &yes},
		{ID: "phone", Name: "Pwcca's S23", ModelName: "SM-S911B", OSName: "Android", IsLinked: &yes},
	}
	got, err := SelectPeer(devices, "self", "S23")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "phone" {
		t.Fatalf("peer=%#v", got)
	}
}
