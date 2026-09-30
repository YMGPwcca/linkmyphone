package dcg

// MessageType values are read directly from ProtoDcgMessageType in the APK.
type MessageType int

const (
	MessageTypeUnspecified          MessageType = 0
	MessageTypeAcknowledgement      MessageType = 1
	MessageTypeFragment             MessageType = 2
	MessageTypePresenceAnnouncement MessageType = 3
	MessageTypePresenceRequest      MessageType = 4
	MessageTypePresenceResponse     MessageType = 5
)

// TransportMessageType values are read directly from ProtoTransportMessageType.
type TransportMessageType int

const (
	TransportMessageTypeUnspecified TransportMessageType = 0
	TransportMessageTypeApp         TransportMessageType = 1
	TransportMessageTypePlatform    TransportMessageType = 2
)
