package dcgheaders

import (
	"crypto/rand"
	"errors"
	"fmt"
)

type Profile string

const (
	ProfileCrossDevice     Profile = "crossdevice"
	ProfilePhoneLink       Profile = "phonelink"
	CrossDeviceAppID               = "MicrosoftWindows.CrossDevice_cw5n1h2txyewy"
	PhoneLinkAppID                 = "Microsoft.YourPhone_8wekyb3d8bbwe"
	CrossDeviceMSAClientID         = "ca3b40e4-3001-4842-8f21-49c0045404f8"
	PhoneLinkMSAClientID           = "8CF55838-E496-42C5-829B-F8D6945288F3"
)

func (p Profile) Canonical() Profile {
	if p == "" {
		return ProfileCrossDevice
	}
	return p
}

func (p Profile) Validate() error {
	switch p.Canonical() {
	case ProfileCrossDevice, ProfilePhoneLink:
		return nil
	default:
		return fmt.Errorf("dcgheaders: unknown client profile %q", p)
	}
}

func (p Profile) ClientType() string {
	if p.Canonical() == ProfilePhoneLink {
		return "PL"
	}
	return "WEA"
}

func (p Profile) MSAClientID() string {
	if p.Canonical() == ProfilePhoneLink {
		return PhoneLinkMSAClientID
	}
	return CrossDeviceMSAClientID
}

func (p Profile) AppID() string {
	if p.Canonical() == ProfilePhoneLink {
		return PhoneLinkAppID
	}
	return CrossDeviceAppID
}

// NewClientInfo selects the enrolled app identity without changing its keys.
// A profile is persisted at enrollment; it must not be switched on resume.
func NewClientInfo(profile Profile, logicalDeviceID, appVersion, ringName, osVersion string) (ClientInfo, error) {
	if err := profile.Validate(); err != nil {
		return ClientInfo{}, err
	}
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
		AppID:           profile.AppID(),
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
