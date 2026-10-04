package notifications

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestNotificationActionsOmitWhenUnsupported(t *testing.T) {
	actions, err := notificationActions([]DesktopAction{{ID: "reply", Label: "Reply"}}, false)
	if err != nil {
		t.Fatalf("notificationActions returned error: %v", err)
	}
	if actions != nil {
		t.Fatalf("unsupported action capability forwarded actions: %#v", actions)
	}
}

func TestNotificationActionsRejectEmptyName(t *testing.T) {
	_, err := notificationActions([]DesktopAction{{ID: "", Label: "Reply"}}, true)
	if err == nil {
		t.Fatal("notificationActions accepted an empty action id")
	}
}

func TestDecodeNotificationIconRejectsNonJPEG(t *testing.T) {
	_, err := decodeNotificationIcon([]byte("not an image"))
	if err == nil {
		t.Fatal("non-JPEG icon accepted")
	}
}

func TestDecodeNotificationIconProducesBoundedImageData(t *testing.T) {
	input := image.NewRGBA(image.Rect(0, 0, 2, 3))
	input.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, input, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	decoded, err := decodeNotificationIcon(encoded.Bytes())
	if err != nil {
		t.Fatalf("decodeNotificationIcon: %v", err)
	}
	if decoded.Width != 2 || decoded.Height != 3 || decoded.RowStride != 8 || !decoded.HasAlpha || decoded.BitsPerSample != 8 || decoded.Channels != 4 {
		t.Fatalf("unexpected image-data metadata: %#v", decoded)
	}
	if len(decoded.Data) != 2*3*4 {
		t.Fatalf("image-data length = %d, want %d", len(decoded.Data), 2*3*4)
	}
}

func TestValidateNotificationRejectsOversizedIcon(t *testing.T) {
	if err := validateNotification(DesktopNotification{AppName: "Phone", Icon: make([]byte, maxIconBytes+1)}); err == nil {
		t.Fatal("validateNotification accepted an oversized icon")
	}
}
