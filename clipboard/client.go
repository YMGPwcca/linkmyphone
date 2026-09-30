// Package clipboard connects the reverse-engineered clipboard protocol to the
// reassembled DCG PLATFORM transport.
package clipboard

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	proto "github.com/YMGPwcca/phonelink-linux/protocol/clipboard"
	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/protocol/platform"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
)

const DefaultRequestTimeout = 10 * time.Second

type Relay interface {
	Send(context.Context, string, string, dcg.TransportMessageType, []byte) error
	Received() <-chan relay.Received
}

type Local interface {
	ReadText(context.Context) (string, error)
	WriteText(context.Context, string) error
}

type Config struct {
	Target         string
	SessionID      string
	RequestTimeout time.Duration
}

type pendingResponse struct {
	response proto.DeviceResourceResponse
	err      error
}

type Client struct {
	relay Relay
	local Local
	cfg   Config

	mu      sync.Mutex
	pending map[string]chan pendingResponse
}

func New(r Relay, local Local, cfg Config) *Client {
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
	return &Client{relay: r, local: local, cfg: cfg, pending: make(map[string]chan pendingResponse)}
}

// Run dispatches PLATFORM messages into request responses and incoming
// /clipboard requests. The underlying relay Run loop must also be running.
func (c *Client) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-c.relay.Received():
			if !ok {
				return errors.New("clipboard: relay closed")
			}
			if msg.TransportMessageType != dcg.TransportMessageTypePlatform {
				continue
			}
			if err := c.handlePlatform(ctx, msg); err != nil {
				return err
			}
		}
	}
}

func (c *Client) GetText(ctx context.Context, correlationID string) (string, error) {
	if correlationID == "" {
		correlationID = newID()
	}
	resp, err := c.request(ctx, proto.NewContentRequest(correlationID))
	if err != nil {
		return "", err
	}
	if resp.Status != proto.ResponseOK {
		return "", fmt.Errorf("clipboard: content response status %d: %s", resp.Status, resp.ErrorDetail)
	}
	for _, item := range resp.Items {
		if item.Type == proto.ItemTextPlain && item.Text != nil {
			return *item.Text, nil
		}
	}
	return "", errors.New("clipboard: response contains no plain text item")
}

func (c *Client) Status(ctx context.Context, correlationID string) (proto.ResponseStatus, error) {
	if correlationID == "" {
		correlationID = newID()
	}
	resp, err := c.request(ctx, proto.NewStatusRequest(correlationID))
	if err != nil {
		return proto.ResponseUnspecified, err
	}
	return resp.Status, nil
}

func (c *Client) PullToLocal(ctx context.Context) error {
	return c.pullToLocal(ctx, "")
}

// HandlePhoneClipboardPublication implements the Windows receive-side clipboard
// state machine: parse the phone publication from PubSubPayload.Additional,
// preserve its correlation ID, request CONTENT immediately, then write the
// returned text to the local clipboard.
//
// Windows does not issue a STATUS request here; feature state is synchronized
// separately with FEATURE_ON/OFF/DISABLE requests.
func (c *Client) HandlePhoneClipboardPublication(ctx context.Context, publication proto.PubSubPayload) error {
	correlationID, err := proto.ParsePhoneClipboardChangePublication(publication)
	if err != nil {
		return err
	}
	return c.pullToLocal(ctx, correlationID)
}

func (c *Client) pullToLocal(ctx context.Context, correlationID string) error {
	text, err := c.GetText(ctx, correlationID)
	if err != nil {
		return err
	}
	if c.local == nil {
		return errors.New("clipboard: no local clipboard backend")
	}
	return c.local.WriteText(ctx, text)
}

// PushFeatureState mirrors the Windows per-device feature synchronization.
// The normal enabled state uses RequestFeatureOn; callers may also advertise
// RequestFeatureOff or RequestFeatureDisable.
func (c *Client) PushFeatureState(ctx context.Context, state proto.RequestType) (proto.ResponseStatus, error) {
	switch state {
	case proto.RequestFeatureOn, proto.RequestFeatureOff, proto.RequestFeatureDisable:
	default:
		return proto.ResponseUnspecified, fmt.Errorf("clipboard: invalid feature state request %d", state)
	}
	resp, err := c.request(ctx, proto.Request{Type: state})
	if err != nil {
		return proto.ResponseUnspecified, err
	}
	return resp.Status, nil
}

func (c *Client) request(ctx context.Context, req proto.Request) (proto.Response, error) {
	if c.cfg.Target == "" || c.cfg.SessionID == "" {
		return proto.Response{}, errors.New("clipboard: target and session id are required")
	}
	requestID := newID()
	inner := proto.MarshalDeviceResourceMessage(proto.WrapClipboardRequest(req))
	pm := platform.NewDeviceResourceRequest(inner, requestID)
	wire, err := platform.Marshal(pm)
	if err != nil {
		return proto.Response{}, err
	}
	ch := make(chan pendingResponse, 1)
	c.mu.Lock()
	c.pending[requestID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, requestID)
		c.mu.Unlock()
	}()

	if err := c.relay.Send(ctx, c.cfg.Target, c.cfg.SessionID, dcg.TransportMessageTypePlatform, wire); err != nil {
		return proto.Response{}, err
	}
	waitCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, c.cfg.RequestTimeout)
		defer cancel()
	}
	select {
	case got := <-ch:
		if got.err != nil {
			return proto.Response{}, got.err
		}
		if got.response.ResponseType != proto.DeviceResourceResponseSuccess {
			return proto.Response{}, fmt.Errorf("clipboard: DRM response type %d", got.response.ResponseType)
		}
		return proto.UnmarshalResponse(got.response.Payload)
	case <-waitCtx.Done():
		return proto.Response{}, waitCtx.Err()
	}
}

func (c *Client) handlePlatform(ctx context.Context, msg relay.Received) error {
	pm, err := platform.Unmarshal(msg.Payload)
	if err != nil {
		return err
	}
	route, _ := pm.Header(platform.HeaderRoute)
	switch route {
	case platform.RouteInternalResponse:
		original, ok := pm.Header(platform.HeaderOriginalRequestID)
		if !ok || original == "" {
			return errors.New("clipboard: internal response missing original request id")
		}
		response, err := proto.UnmarshalDeviceResourceResponse(pm.Payload)
		c.mu.Lock()
		ch := c.pending[original]
		c.mu.Unlock()
		if ch != nil {
			ch <- pendingResponse{response: response, err: err}
		}
		return nil

	case platform.RouteDeviceResourceManager:
		return c.handleIncomingRequest(ctx, msg, pm)
	default:
		return nil
	}
}

func (c *Client) handleIncomingRequest(ctx context.Context, msg relay.Received, pm platform.Message) error {
	requestID, ok := pm.Header(platform.HeaderRequestID)
	if !ok || requestID == "" {
		return errors.New("clipboard: incoming request missing request id")
	}
	drm, err := proto.UnmarshalDeviceResourceMessage(pm.Payload)
	if err != nil {
		return err
	}
	if drm.ResourcePath != proto.ResourcePath || drm.RequestType != proto.DeviceResourceRequestGET {
		return c.sendResponse(ctx, msg.Source, msg.SessionID, requestID, proto.DeviceResourceResponse{
			ResponseType: proto.DeviceResourceResponseResourceHandlerNotRegistered,
		})
	}
	req, err := proto.UnmarshalRequest(drm.Payload)
	if err != nil {
		return err
	}
	var response proto.Response
	switch req.Type {
	case proto.RequestStatus:
		response = proto.NewFeatureOnResponse(req.CorrelationID)
	case proto.RequestContent:
		if c.local == nil {
			response = proto.Response{Status: proto.ResponseInvalidContent, CorrelationID: req.CorrelationID, ErrorType: proto.ErrorFail, ErrorDetail: "local clipboard unavailable"}
		} else {
			text, err := c.local.ReadText(ctx)
			if err != nil {
				response = proto.Response{Status: proto.ResponseInvalidContent, CorrelationID: req.CorrelationID, ErrorType: proto.ErrorFail, ErrorDetail: "local clipboard read failed"}
			} else {
				response = proto.NewTextResponse(req.CorrelationID, text, nil)
			}
		}
	default:
		response = proto.Response{Status: proto.ResponseInvalidClipboardRequestType, CorrelationID: req.CorrelationID, ErrorType: proto.ErrorReject}
	}
	return c.sendResponse(ctx, msg.Source, msg.SessionID, requestID, proto.DeviceResourceResponse{
		ResponseType: proto.DeviceResourceResponseSuccess,
		Payload:      proto.MarshalResponse(response),
	})
}

func (c *Client) sendResponse(ctx context.Context, target, sessionID, requestID string, response proto.DeviceResourceResponse) error {
	pm := platform.NewInternalResponse(proto.MarshalDeviceResourceResponse(response), requestID)
	wire, err := platform.Marshal(pm)
	if err != nil {
		return err
	}
	return c.relay.Send(ctx, target, sessionID, dcg.TransportMessageTypePlatform, wire)
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
