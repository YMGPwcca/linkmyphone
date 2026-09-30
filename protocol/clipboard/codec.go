package clipboard

func MarshalRequest(m Request) []byte {
	var out []byte
	out = appendVarintField(out, 1, uint64(m.Type))
	out = appendStringField(out, 2, m.CorrelationID)
	return out
}

func UnmarshalRequest(b []byte) (Request, error) {
	var m Request
	for i := 0; i < len(b); {
		key, err := readVarint(b, &i)
		if err != nil {
			return m, err
		}
		field, wire := key>>3, key&7
		switch field {
		case 1:
			if wire != 0 {
				return m, ErrMalformed
			}
			v, err := readVarint(b, &i)
			if err != nil {
				return m, err
			}
			m.Type = RequestType(v)
		case 2:
			if wire != 2 {
				return m, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return m, err
			}
			m.CorrelationID = string(v)
		default:
			if err := skipField(b, &i, wire); err != nil {
				return m, err
			}
		}
	}
	return m, nil
}

func marshalTimestamp(t *Timestamp) []byte {
	if t == nil {
		return nil
	}
	var out []byte
	out = appendVarintField(out, 1, uint64(t.Seconds))
	out = appendVarintField(out, 2, uint64(uint32(t.Nanos)))
	return out
}

func unmarshalTimestamp(b []byte) (*Timestamp, error) {
	t := &Timestamp{}
	for i := 0; i < len(b); {
		key, err := readVarint(b, &i)
		if err != nil {
			return nil, err
		}
		field, wire := key>>3, key&7
		if wire != 0 {
			if err := skipField(b, &i, wire); err != nil {
				return nil, err
			}
			continue
		}
		v, err := readVarint(b, &i)
		if err != nil {
			return nil, err
		}
		switch field {
		case 1:
			t.Seconds = int64(v)
		case 2:
			t.Nanos = int32(v)
		}
	}
	return t, nil
}

func marshalItem(it Item) []byte {
	var out []byte
	out = appendVarintField(out, 1, uint64(it.Type))
	if it.Text != nil {
		out = appendStringField(out, 2, *it.Text)
	}
	out = appendBytesField(out, 3, it.ImageBytes)
	if it.CreatedTime != nil {
		out = appendBytesField(out, 4, marshalTimestamp(it.CreatedTime))
	}
	return out
}

func unmarshalItem(b []byte) (Item, error) {
	var it Item
	for i := 0; i < len(b); {
		key, err := readVarint(b, &i)
		if err != nil {
			return it, err
		}
		field, wire := key>>3, key&7
		switch field {
		case 1:
			if wire != 0 {
				return it, ErrMalformed
			}
			v, err := readVarint(b, &i)
			if err != nil {
				return it, err
			}
			it.Type = ItemType(v)
		case 2:
			if wire != 2 {
				return it, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return it, err
			}
			s := string(v)
			it.Text = &s
		case 3:
			if wire != 2 {
				return it, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return it, err
			}
			it.ImageBytes = append([]byte(nil), v...)
		case 4:
			if wire != 2 {
				return it, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return it, err
			}
			t, err := unmarshalTimestamp(v)
			if err != nil {
				return it, err
			}
			it.CreatedTime = t
		default:
			if err := skipField(b, &i, wire); err != nil {
				return it, err
			}
		}
	}
	return it, nil
}

func MarshalResponse(m Response) []byte {
	var out []byte
	for _, it := range m.Items {
		out = appendBytesField(out, 1, marshalItem(it))
	}
	out = appendVarintField(out, 2, uint64(m.Status))
	out = appendStringField(out, 3, m.CorrelationID)
	out = appendVarintField(out, 4, uint64(m.ErrorType))
	out = appendStringField(out, 5, m.ErrorDetail)
	return out
}

func UnmarshalResponse(b []byte) (Response, error) {
	var m Response
	for i := 0; i < len(b); {
		key, err := readVarint(b, &i)
		if err != nil {
			return m, err
		}
		field, wire := key>>3, key&7
		switch field {
		case 1:
			if wire != 2 {
				return m, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return m, err
			}
			it, err := unmarshalItem(v)
			if err != nil {
				return m, err
			}
			m.Items = append(m.Items, it)
		case 2:
			if wire != 0 {
				return m, ErrMalformed
			}
			v, err := readVarint(b, &i)
			if err != nil {
				return m, err
			}
			m.Status = ResponseStatus(v)
		case 3:
			if wire != 2 {
				return m, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return m, err
			}
			m.CorrelationID = string(v)
		case 4:
			if wire != 0 {
				return m, ErrMalformed
			}
			v, err := readVarint(b, &i)
			if err != nil {
				return m, err
			}
			m.ErrorType = ErrorType(v)
		case 5:
			if wire != 2 {
				return m, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return m, err
			}
			m.ErrorDetail = string(v)
		default:
			if err := skipField(b, &i, wire); err != nil {
				return m, err
			}
		}
	}
	return m, nil
}

func MarshalPubSubPayload(m PubSubPayload) []byte {
	var out []byte
	out = appendBytesField(out, 1, m.Data)
	out = appendBytesField(out, 2, m.Additional)
	return out
}

func UnmarshalPubSubPayload(b []byte) (PubSubPayload, error) {
	var m PubSubPayload
	for i := 0; i < len(b); {
		key, err := readVarint(b, &i)
		if err != nil {
			return m, err
		}
		field, wire := key>>3, key&7
		if wire != 2 {
			if err := skipField(b, &i, wire); err != nil {
				return m, err
			}
			continue
		}
		v, err := readBytes(b, &i)
		if err != nil {
			return m, err
		}
		switch field {
		case 1:
			m.Data = append([]byte(nil), v...)
		case 2:
			m.Additional = append([]byte(nil), v...)
		}
	}
	return m, nil
}

func MarshalDeviceResourceMessage(m DeviceResourceMessage) []byte {
	var out []byte
	out = appendVarintField(out, 1, uint64(m.ResourceType))
	out = appendVarintField(out, 2, uint64(m.RequestType))
	out = appendBytesField(out, 3, m.Payload)
	out = appendStringField(out, 4, m.ResourcePath)
	return out
}

func UnmarshalDeviceResourceMessage(b []byte) (DeviceResourceMessage, error) {
	var m DeviceResourceMessage
	for i := 0; i < len(b); {
		key, err := readVarint(b, &i)
		if err != nil {
			return m, err
		}
		field, wire := key>>3, key&7
		switch field {
		case 1:
			if wire != 0 {
				return m, ErrMalformed
			}
			v, err := readVarint(b, &i)
			if err != nil {
				return m, err
			}
			m.ResourceType = DeviceResourceType(v)
		case 2:
			if wire != 0 {
				return m, ErrMalformed
			}
			v, err := readVarint(b, &i)
			if err != nil {
				return m, err
			}
			m.RequestType = DeviceResourceRequestType(v)
		case 3:
			if wire != 2 {
				return m, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return m, err
			}
			m.Payload = append([]byte(nil), v...)
		case 4:
			if wire != 2 {
				return m, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return m, err
			}
			m.ResourcePath = string(v)
		default:
			if err := skipField(b, &i, wire); err != nil {
				return m, err
			}
		}
	}
	return m, nil
}

const ClipboardMessageTag = 9

func NewClipboardChange(correlationID string) Response {
	return Response{Status: ResponseClipboardChange, CorrelationID: correlationID}
}

// NewClipboardChangePublication builds the exact PubSubPayload used by the
// Windows CrossDevice clipboard publisher: field 1 (Data) contains a serialized
// ClipboardResponseMessage with status ClipboardChange.
func NewClipboardChangePublication(correlationID string) PubSubPayload {
	return PubSubPayload{Data: MarshalResponse(NewClipboardChange(correlationID))}
}

func NewStatusRequest(correlationID string) Request {
	return Request{Type: RequestStatus, CorrelationID: correlationID}
}

func NewContentRequest(correlationID string) Request {
	return Request{Type: RequestContent, CorrelationID: correlationID}
}

func NewFeatureOnResponse(correlationID string) Response {
	return Response{Status: ResponseFeatureOn, CorrelationID: correlationID}
}

func NewTextResponse(correlationID, text string, ts *Timestamp) Response {
	return Response{Items: []Item{{Type: ItemTextPlain, Text: &text, CreatedTime: ts}}, Status: ResponseOK, CorrelationID: correlationID}
}

func WrapClipboardRequest(req Request) DeviceResourceMessage {
	return DeviceResourceMessage{ResourceType: DeviceResourceTypeUnknown, RequestType: DeviceResourceRequestGET, Payload: MarshalRequest(req), ResourcePath: ResourcePath}
}


func MarshalDeviceResourceResponse(m DeviceResourceResponse) []byte {
	var out []byte
	out = appendBytesField(out, 1, m.Payload)
	out = appendVarintField(out, 2, uint64(m.ResponseType))
	return out
}

func UnmarshalDeviceResourceResponse(b []byte) (DeviceResourceResponse, error) {
	var m DeviceResourceResponse
	for i := 0; i < len(b); {
		key, err := readVarint(b, &i)
		if err != nil {
			return m, err
		}
		field, wire := key>>3, key&7
		switch field {
		case 1:
			if wire != 2 {
				return m, ErrMalformed
			}
			v, err := readBytes(b, &i)
			if err != nil {
				return m, err
			}
			m.Payload = append([]byte(nil), v...)
		case 2:
			if wire != 0 {
				return m, ErrMalformed
			}
			v, err := readVarint(b, &i)
			if err != nil {
				return m, err
			}
			m.ResponseType = DeviceResourceResponseType(v)
		default:
			if err := skipField(b, &i, wire); err != nil {
				return m, err
			}
		}
	}
	return m, nil
}
