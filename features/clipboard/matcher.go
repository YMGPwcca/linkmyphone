package clipboard

import (
	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/protocol/msaep"
	clipproto "github.com/YMGPwcca/phonelink-linux/protocol/clipboard"
	"github.com/YMGPwcca/phonelink-linux/protocol/platform"
	"github.com/YMGPwcca/phonelink-linux/runtime/phonehost"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
)

func matcherForTarget(target string) phonehost.Matcher {
	return func(message relay.Received) bool {
		if target != "" && message.Source != target {
			return false
		}
		return matchesMessage(message)
	}
}

func matchesMessage(message relay.Received) bool {
	if message.TransportMessageType != dcg.TransportMessageTypePlatform {
		return false
	}
	pm, err := platform.Unmarshal(message.Payload)
	if err != nil {
		return false
	}
	route, _ := pm.Header(platform.HeaderRoute)
	switch route {
	case platform.RouteInternalResponse:
		// Request IDs are owned inside clipboard.Client. Internal responses are
		// safe to fan out; clients ignore response IDs they do not own.
		return true
	case platform.RouteDeviceResourceManager:
		drm, err := clipproto.UnmarshalDeviceResourceMessage(pm.Payload)
		return err == nil && drm.ResourcePath == clipproto.ResourcePath
	case platform.RouteContextPublish:
		envelope, err := msaep.Unmarshal(pm.Payload)
		return err == nil && envelope.MessageTag == int32(clipproto.ClipboardMessageTag)
	default:
		return false
	}
}
