package dcg

// MessageType values match both the DCG Hub Relay packet enum and
// ProtoDcgMessageType.
type MessageType int

const (
	MessageTypeUnspecified          MessageType = 0
	MessageTypeAcknowledgement      MessageType = 1
	MessageTypeFragment             MessageType = 2
	MessageTypePresenceAnnouncement MessageType = 3
	MessageTypePresenceRequest      MessageType = 4
	MessageTypePresenceResponse     MessageType = 5
)

// TransportMessageType here models the Hub Relay multiplex-packet value used by
// DCGFragmentMessage.ToHubRelayMultiplexPacket, not ProtoTransportMessageType.
//
// Windows has two distinct enums:
//   TransportMessageType:      App=0, Platform=1, Unknown=2
//   ProtoTransportMessageType: Unspecified=0, App=1, Platform=2
//
// SignalR/Hub Relay packets use the first enum. Proto values must not be copied
// directly into MultiplexPacket.Properties["MessageType"].
type TransportMessageType int

const (
	TransportMessageTypeApp      TransportMessageType = 0
	TransportMessageTypePlatform TransportMessageType = 1
	TransportMessageTypeUnknown  TransportMessageType = 2
)
