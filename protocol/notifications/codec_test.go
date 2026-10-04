package notifications

import (
	"encoding/json"
	"github.com/YMGPwcca/linkmyphone/protocol/app"
	"testing"
)

func TestBatchRemovalWithoutJSONAndOriginalActionIndex(t *testing.T) {
	body := `{"key":"current","postTime":1700000000123,"isClearable":true,"notificationActions":[{"actionName":"Reply","isActionInlineReply":true,"actionIndex":4}]}`
	batch, err := DecodeBatch(app.ValueSet{"contentType": "notifications", "notificationKeys": []string{"current", "gone"}, "operations": []int32{1, 2}, "notifications": []string{body, ""}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Operations[1].Type != OperationRemove || batch.Operations[1].Key != "gone" || batch.Operations[1].Item != nil {
		t.Fatalf("removal=%#v", batch.Operations[1])
	}
	item := batch.Operations[0].Item
	if item.PostTime != 1700000000123 || !item.IsClearable || len(item.Actions) != 1 || item.Actions[0].Index != 4 || !item.Actions[0].InlineReply {
		t.Fatalf("item=%#v", item)
	}
}

func TestMalformedBatchCannotCrossCorrelatedIdentities(t *testing.T) {
	for _, values := range []app.ValueSet{
		{"contentType": "notifications", "notificationKeys": []string{"a"}, "operations": []int32{1, 2}, "notifications": []string{`{"key":"a"}`}},
		{"contentType": "notifications", "notificationKeys": []string{"a"}, "operations": []int32{1}, "notifications": []string{`{"key":"b"}`}},
		{"contentType": "notifications", "notificationKeys": []string{"a"}, "operations": []int64{1}, "notifications": []string{`{"key":"a"}`}},
		{"contentType": "notifications", "notificationKeys": []string{"a"}, "operations": []int32{3}, "notifications": []string{"null"}},
	} {
		if _, err := DecodeBatch(values); err == nil {
			t.Fatalf("accepted invalid batch: %#v", values)
		}
	}
}

func TestMutationRequestsCannotClearAllOrLoseReplyUnicode(t *testing.T) {
	if _, err := ClearRequest(LocalInfo{}, "cv", nil); err == nil {
		t.Fatal("empty clear would become cancelAllNotifications")
	}
	if _, err := ReconcileRequest(LocalInfo{}, "cv", []string{"key"}, nil); err == nil {
		t.Fatal("unpaired postTimes accepted")
	}
	text := "Xin chào 👋\nSecond line"
	values, err := ActionRequest(LocalInfo{}, "cv", "key", 4, &text)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := app.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := app.Unmarshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if decoded["operation"] != int32(5) || decoded["actionIndex"] != int32(4) || decoded["inlineReplyMessage"] != text {
		b, _ := json.Marshal(decoded)
		t.Fatalf("reply contract=%s", b)
	}
}
