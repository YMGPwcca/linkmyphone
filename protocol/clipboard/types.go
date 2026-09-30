package clipboard

// RequestType mirrors clipboard.v1.ClipboardRequestType.
type RequestType uint64

const (
	RequestUnspecified    RequestType = 0
	RequestContent        RequestType = 1
	RequestFeatureOn      RequestType = 2
	RequestFeatureOff     RequestType = 3
	RequestFeatureDisable RequestType = 4
	RequestStatus         RequestType = 5
)

// ResponseStatus mirrors clipboard.v1.ClipboardResponseStatus.
type ResponseStatus uint64

const (
	ResponseUnspecified                      ResponseStatus = 0
	ResponseOK                               ResponseStatus = 1
	ResponseInvalidDeviceResourceRequestType ResponseStatus = 2
	ResponseInvalidDeviceResourceRequestPath ResponseStatus = 3
	ResponseInvalidClipboardRequestType      ResponseStatus = 4
	ResponseUnrecognizedPayload              ResponseStatus = 5
	ResponseClipboardChange                  ResponseStatus = 6
	ResponseInvalidContent                   ResponseStatus = 7
	ResponseFeatureOn                        ResponseStatus = 8
	ResponseFeatureOff                       ResponseStatus = 9
	ResponseFeatureDisable                   ResponseStatus = 10
)

// ItemType mirrors clipboard.v1.ClipboardItemType.
type ItemType uint64

const (
	ItemUnspecified ItemType = 0
	ItemImage       ItemType = 1
	ItemTextPlain   ItemType = 2
	ItemTextHTML    ItemType = 3
)

// ErrorType mirrors clipboard.v1.ClipboardErrorType.
type ErrorType uint64

const (
	ErrorUnspecified ErrorType = 0
	ErrorReject      ErrorType = 1
	ErrorFail        ErrorType = 2
)

// Request is the clipboard.v1 ClipboardRequestMessage payload.
type Request struct {
	Type          RequestType
	CorrelationID string
}

// Timestamp is a minimal protobuf Timestamp representation.
type Timestamp struct {
	Seconds int64
	Nanos   int32
}

// Item is the clipboard.v1 ClipboardItem payload.
type Item struct {
	Type        ItemType
	Text        *string
	ImageBytes  []byte
	CreatedTime *Timestamp
}

// Response is the clipboard.v1 ClipboardResponseMessage payload.
type Response struct {
	Items         []Item
	Status        ResponseStatus
	CorrelationID string
	ErrorType     ErrorType
	ErrorDetail   string
}

// PubSubPayload is the two-field wrapper used by the platform PubSub layer.
type PubSubPayload struct {
	Data       []byte
	Additional []byte
}

// DeviceResourceRequestType mirrors the DRM request enum used for /clipboard.
type DeviceResourceRequestType uint64

const (
	DeviceResourceRequestUnspecified DeviceResourceRequestType = 0
	DeviceResourceRequestGET         DeviceResourceRequestType = 1
	DeviceResourceRequestUPDATE      DeviceResourceRequestType = 2
	DeviceResourceRequestDELETE      DeviceResourceRequestType = 3
	DeviceResourceRequestSYNC        DeviceResourceRequestType = 4
)

// DeviceResourceType mirrors the generic resource enum value used by clipboard.
type DeviceResourceType uint64

const (
	DeviceResourceTypeUnspecified DeviceResourceType = 0
	DeviceResourceTypeUnknown     DeviceResourceType = 4
)

const ResourcePath = "/clipboard"

// DeviceResourceMessage is the protobuf wrapper sent via /DeviceResourceManager.
type DeviceResourceMessage struct {
	ResourceType DeviceResourceType
	RequestType  DeviceResourceRequestType
	Payload      []byte
	ResourcePath string
}
