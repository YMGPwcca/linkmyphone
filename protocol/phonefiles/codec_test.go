package phonefiles

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMarshalFirstBatch(t *testing.T) {
	wire, err := MarshalFirstBatch("request-123")
	if err != nil { t.Fatal(err) }
	var full, hasCursor, batched, request bool
	if err := walk(wire, func(field, wt int, data []byte, _ uint64) error {
		switch field {
		case 2:
			if wt != 2 { t.Fatal("wrong sync wire type") }
			return walk(data, func(field, wt int, _ []byte, number uint64) error {
				if field==1 && wt==0 && number==1 { full = true }
				if field==2 && wt==0 && number==^uint64(0) { hasCursor = true }
				if field==4 && wt==0 && number==1 { batched = true }
				return nil
			})
		case 4: request = string(data)=="request-123"
		}
		return nil
	}); err != nil { t.Fatal(err) }
	if !full || !hasCursor || !batched || !request {
		t.Fatalf("incomplete request: %t %t %t %t",full,hasCursor,batched,request)
	}
}

func TestParseResponse(t *testing.T) {
	full := appendBytesField(nil, 1, []byte{0x0a, 0x02, 'h', 'i'})
	full = appendBytesField(full, 1, []byte{0x0a, 0x01, 'x'})
	full = appendVarintField(full, 4, 1)
	full = appendVarintField(full, 7, 900)
	body := appendVarintField(nil, 1, 1)
	body = appendBytesField(body, 3, appendBytesField(nil, 1, full))
	body = appendBytesField(body, 5, []byte("request-123"))
	result, err := ParseResponse(body, "request-123")
	if err != nil { t.Fatal(err) }
	if result.Status != 1 || result.Files != 2 || !result.HasMoreBatches || result.EstimatedTotalItems != 900 {
		t.Fatalf("unexpected summary: %+v", result)
	}
	if _,err := ParseResponse(body,"wrong");err==nil { t.Fatal("accepted mismatched request") }
	if _,err := ParseResponse(body[:len(body)-1],"request-123");err==nil { t.Fatal("accepted truncated response") }
}

func TestStatusOnly(t *testing.T) {
	body := appendVarintField(nil, 1, 8)
	result, err := ParseResponse(body,"request-123")
	if err != nil || result.Status != 8 { t.Fatalf("status-only: %+v, %v",result,err) }
}

func TestBounds(t *testing.T) {
	if _,err:=ParseResponse(bytes.Repeat([]byte{0},MaxResponseBytes+1),"x");err==nil { t.Fatal("accepted oversized response") }
	var oversized []byte
	for i:=0;i<=MaxMetadataItems;i++ { oversized=appendBytesField(oversized,1,[]byte{1}) }
	response:=appendVarintField(nil,1,1)
	response=appendBytesField(response,3,appendBytesField(nil,1,oversized))
	if _,err:=ParseResponse(response,"x");err==nil { t.Fatal("accepted too many files") }
	malformed:=bytes.Repeat([]byte{0xff},binary.MaxVarintLen64+1)
	if _,err:=ParseResponse(malformed,"x");err==nil { t.Fatal("accepted malformed varint") }
}
