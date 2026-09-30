package msa

import "testing"

func TestFullScope(t *testing.T) {
	got := FullScope("service::msatoken.dcg.microsoft.com::MBI_SSL")
	want := "service::msatoken.dcg.microsoft.com::MBI_SSL&api-version=2.0&clientid=ca3b40e4-3001-4842-8f21-49c0045404f8"
	if got != want {
		t.Fatalf("FullScope=%q want=%q", got, want)
	}
}
