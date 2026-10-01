package signalr

import "testing"

func TestParseCompletionVoid(t *testing.T) {
	body := []byte{0x94, 0x03, 0x80, 0xa1, '7', 0x02}
	got, err := ParseCompletion(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.InvocationID != "7" || got.ResultKind != CompletionResultVoid || got.Error != "" {
		t.Fatalf("got=%#v", got)
	}
}

func TestParseCompletionError(t *testing.T) {
	p := &packer{}
	p.array(5)
	p.integer(HubMessageTypeCompletion)
	p.mapLen(0)
	p.str("9")
	p.integer(CompletionResultError)
	p.str("target unavailable")
	got, err := ParseCompletion(p.b)
	if err != nil {
		t.Fatal(err)
	}
	if got.InvocationID != "9" || got.ResultKind != CompletionResultError || got.Error != "target unavailable" {
		t.Fatalf("got=%#v", got)
	}
}
