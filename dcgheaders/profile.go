package dcgheaders

import (
	"crypto/rand"
	"errors"
	"fmt"
)

const CrossDeviceAppID = "MicrosoftWindows.CrossDevice_cw5n1h2txyewy"

// NewCrossDeviceClientInfo builds the header identity used by the Windows
// CrossDevice package. App version, ring and OS version stay explicit because
// they are deployment/runtime values rather than protocol constants.
func NewCrossDeviceClientInfo(logicalDeviceID, appVersion, ringName, osVersion string) (ClientInfo, error) {
	if logicalDeviceID == "" {
		return ClientInfo{}, errors.New("dcgheaders: logical device id is required")
	}
	if appVersion == "" {
		return ClientInfo{}, errors.New("dcgheaders: app version is required")
	}
	if ringName == "" {
		return ClientInfo{}, errors.New("dcgheaders: ring name is required")
	}
	if osVersion == "" {
		return ClientInfo{}, errors.New("dcgheaders: OS version is required")
	}
	sessionID, err := randomUUID()
	if err != nil {
		return ClientInfo{}, err
	}
	return ClientInfo{
		LogicalDeviceID: logicalDeviceID,
		AppVersion:      appVersion,
		AppID:           CrossDeviceAppID,
		SessionID:       sessionID,
		RingName:        ringName,
		OS:              "Windows",
		OSVersion:       osVersion,
	}, nil
}

func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
