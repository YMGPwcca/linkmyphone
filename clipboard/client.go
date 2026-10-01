// Package clipboard connects the reverse-engineered clipboard protocol to the
// reassembled DCG PLATFORM transport.
package clipboard

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	proto "github.com/YMGPwcca/phonelink-linux/protocol/clipboard"
	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/protocol/msaep"
	"github.com/YMGPwcca/phonelink-linux/protocol/platform"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
)

const (
	DefaultRequestTimeout       = 10 * time.Second
	publishedSnapshotTTL        = 2 * time.Minute
	maxPublishedSnapshotCount   = 64
	incomingRequestQueueSize    = 16
)

var ErrPublicationSuperseded = errors.New("clipboard: local publication superseded")

type publishedSnapshot struct {
	text       string
	createdAt  time.Time
	generation uint64
	versioned  bool
	superseded bool
}

type Relay interface {
	Send(context.Context, string, string, dcg.TransportMessageType, []byte) error
	Received() <-chan relay.Received
}

type Local interface {
	ReadText(context.Context) (string, error)
	WriteText(context.Context, string) error
}

type RemoteApplyEvent struct {
	Generation uint64
	Bytes      int
}

type Config struct {
	Target          string
	SessionID       string
	SelfDcgClientID string
	RequestTimeout  time.Duration
	OnRemoteApplied func(RemoteApplyEvent)
}

type pendingResponse struct {
	response proto.DeviceResourceResponse
	err      error
}

type phonePublication struct {
	correlationID string
	generation    uint64
}

type incomingRequest struct {
	msg relay.Received
	pm  platform.Message
}

type Client struct {
	relay Relay
	local Local
	cfg   Config

	mu               sync.Mutex
	pending          map[string]chan pendingResponse
	published        map[string]publishedSnapshot
	publicationFloor uint64
	generationMu     sync.Mutex
	generation       atomic.Uint64
	asyncErr         chan error
	incomingRequests chan incomingRequest

	publicationMu   sync.Mutex
	publications    chan phonePublication
	phonePullCancel context.CancelFunc
}

func New(r Relay, local Local, cfg Config) *Client {
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
	return &Client{
		relay: r,
		local: local,
		cfg: cfg,
		pending:          make(map[string]chan pendingResponse),
		published:        make(map[string]publishedSnapshot),
		asyncErr:         make(chan error, 8),
		incomingRequests: make(chan incomingRequest, incomingRequestQueueSize),
		publications:     make(chan phonePublication, 1),
	}
}

// Run dispatches PLATFORM messages into request responses and incoming
// /clipboard requests. The underlying relay Run loop must also be running.
func (c *Client) Run(ctx context.Context) error {
	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()
	go c.runPhonePublicationWorker(workerCtx)
	go c.runIncomingRequestWorker(workerCtx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-c.asyncErr:
			if err != nil {
				return err
			}
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
	return c.pullToLocal(ctx, "", 0)
}

// PublishLocalChange mirrors the Windows SignalRContextProvider cloud publish
// path for clipboard tag 9:
// ClipboardResponseMessage -> PubSubPayload -> MsaepMessage -> /Context/Publish.
// The platform request is one-way; relay.Send still waits for the DCG fragment ACK.
func (c *Client) PublishLocalChange(ctx context.Context, correlationID string) (string, error) {
	if c.cfg.Target == "" {
		return "", errors.New("clipboard: target is required")
	}
	if c.cfg.SelfDcgClientID == "" {
		return "", errors.New("clipboard: self DCG client id is required for PubSub")
	}
	if correlationID == "" {
		correlationID = newID()
	}
	pubsub := proto.MarshalPubSubPayload(proto.NewPCClipboardChangePublication(correlationID))
	envelope := msaep.New(newID(), c.cfg.SelfDcgClientID, int32(proto.ClipboardMessageTag), pubsub)
	pm := platform.NewContextPublish(msaep.Marshal(envelope), newID())
	wire, err := platform.Marshal(pm)
	if err != nil {
		return "", err
	}
	if err := c.relay.Send(ctx, c.cfg.Target, c.cfg.SessionID, dcg.TransportMessageTypePlatform, wire); err != nil {
		return "", err
	}
	return correlationID, nil
}

// PublishLocalText snapshots the text under the publication correlation id
// before advertising CLIPBOARD_CHANGE. A later CONTENT request carrying that
// correlation id receives the exact advertised snapshot even if the desktop
// clipboard changes again in the meantime.
func (c *Client) PublishLocalText(ctx context.Context, text, correlationID string) (string, error) {
	return c.publishLocalText(ctx, text, correlationID, 0, false)
}

// PublishLocalTextGeneration binds an outbound snapshot to a local sync
// generation. A later phone-originated generation can supersede older local
// publications without losing the normal unversioned API used by probes/tests.
func (c *Client) PublishLocalTextGeneration(
	ctx context.Context,
	text,
	correlationID string,
	generation uint64,
) (string, error) {
	if generation == 0 {
		return "", errors.New("clipboard: publication generation must be positive")
	}
	c.observeGeneration(generation)
	return c.publishLocalText(ctx, text, correlationID, generation, true)
}

// ReserveLocalGeneration records a newly observed local clipboard change.
// Reserving immediately (before the publisher worker sends it) gives local and
// phone changes one ordering domain. It deliberately keeps older local
// snapshots alive because Android may still request their correlation IDs.
func (c *Client) ReserveLocalGeneration() uint64 {
	c.generationMu.Lock()
	defer c.generationMu.Unlock()
	return c.generation.Add(1)
}

// CurrentGeneration returns the latest observed clipboard generation. It is
// intended for diagnostics; ordering decisions must reserve/compare inside the
// client rather than deriving a new generation from this value.
func (c *Client) CurrentGeneration() uint64 {
	return c.generation.Load()
}

// SupersedeLocalPublications tombstones versioned local snapshots older than
// generation. If an old Context/Publish is already in flight, a later CONTENT
// request is rejected instead of being answered with unrelated current text.
func (c *Client) SupersedeLocalPublications(generation uint64) {
	if generation == 0 {
		return
	}
	c.generationMu.Lock()
	defer c.generationMu.Unlock()
	c.observeGenerationLocked(generation)
	c.advancePublicationFloor(generation)
}

func (c *Client) observeGeneration(generation uint64) {
	c.generationMu.Lock()
	defer c.generationMu.Unlock()
	c.observeGenerationLocked(generation)
}

func (c *Client) observeGenerationLocked(generation uint64) {
	for {
		current := c.generation.Load()
		if generation <= current {
			return
		}
		if c.generation.CompareAndSwap(current, generation) {
			return
		}
	}
}

func (c *Client) reserveRemoteGeneration() uint64 {
	c.generationMu.Lock()
	defer c.generationMu.Unlock()
	generation := c.generation.Add(1)
	c.advancePublicationFloor(generation)
	return generation
}

func (c *Client) advancePublicationFloor(generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation <= c.publicationFloor {
		return
	}
	c.publicationFloor = generation
	for correlationID, snapshot := range c.published {
		if snapshot.versioned && snapshot.generation < generation {
			snapshot.superseded = true
			c.published[correlationID] = snapshot
		}
	}
}

func (c *Client) publishLocalText(
	ctx context.Context,
	text,
	correlationID string,
	generation uint64,
	versioned bool,
) (string, error) {
	if correlationID == "" {
		correlationID = newID()
	}
	now := time.Now()
	c.mu.Lock()
	c.prunePublishedLocked(now)
	if versioned && generation < c.publicationFloor {
		c.mu.Unlock()
		return "", ErrPublicationSuperseded
	}
	c.published[correlationID] = publishedSnapshot{
		text:       text,
		createdAt:  now,
		generation: generation,
		versioned:  versioned,
	}
	c.mu.Unlock()

	publishedID, err := c.PublishLocalChange(ctx, correlationID)
	if err != nil {
		c.mu.Lock()
		if snapshot, exists := c.published[correlationID]; exists &&
			snapshot.generation == generation &&
			snapshot.versioned == versioned {
			delete(c.published, correlationID)
		}
		c.mu.Unlock()
		return "", err
	}

	c.mu.Lock()
	snapshot := c.published[correlationID]
	superseded := versioned && generation < c.publicationFloor
	if superseded && snapshot.versioned && snapshot.generation == generation {
		snapshot.superseded = true
		c.published[correlationID] = snapshot
	}
	c.mu.Unlock()
	if superseded {
		return "", ErrPublicationSuperseded
	}
	return publishedID, nil
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
	generation := c.reserveRemoteGeneration()
	return c.pullToLocal(ctx, correlationID, generation)
}

func (c *Client) prunePublishedLocked(now time.Time) {
	for correlationID, snapshot := range c.published {
		if now.Sub(snapshot.createdAt) > publishedSnapshotTTL {
			delete(c.published, correlationID)
		}
	}
	for len(c.published) >= maxPublishedSnapshotCount {
		var oldestID string
		var oldestTime time.Time
		for correlationID, snapshot := range c.published {
			if oldestID == "" || snapshot.createdAt.Before(oldestTime) {
				oldestID = correlationID
				oldestTime = snapshot.createdAt
			}
		}
		if oldestID == "" {
			break
		}
		delete(c.published, oldestID)
	}
}

func (c *Client) pullToLocal(ctx context.Context, correlationID string, generation uint64) error {
	text, err := c.GetText(ctx, correlationID)
	if err != nil {
		return err
	}
	if c.local == nil {
		return errors.New("clipboard: no local clipboard backend")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if generation != 0 {
		c.generationMu.Lock()
		defer c.generationMu.Unlock()
		if c.generation.Load() != generation {
			return nil
		}
	}

	// Android may publish the same text back after applying a desktop-originated
	// clipboard update. Avoid rewriting an already-identical local clipboard:
	// it is both unnecessary and can otherwise look like a phone-originated
	// change to the local watcher.
	current, readErr := c.local.ReadText(ctx)
	if readErr == nil && current == text {
		return nil
	}
	if err := c.local.WriteText(ctx, text); err != nil {
		return err
	}
	if c.cfg.OnRemoteApplied != nil {
		c.cfg.OnRemoteApplied(RemoteApplyEvent{
			Generation: generation,
			Bytes:      len([]byte(text)),
		})
	}
	return nil
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
	if c.cfg.Target == "" {
		return proto.Response{}, errors.New("clipboard: target is required")
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
		response, err := proto.UnmarshalResponse(got.response.Payload)
		if err != nil {
			return proto.Response{}, err
		}
		if req.CorrelationID != "" && response.CorrelationID != req.CorrelationID {
			return proto.Response{}, fmt.Errorf(
				"clipboard: response correlation id %q does not match request %q",
				response.CorrelationID,
				req.CorrelationID,
			)
		}
		return response, nil
	case <-waitCtx.Done():
		return proto.Response{}, waitCtx.Err()
	}
}

func (c *Client) handlePlatform(ctx context.Context, msg relay.Received) error {
	if c.cfg.Target != "" && msg.Source != c.cfg.Target {
		return nil
	}
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
			select {
			case ch <- pendingResponse{response: response, err: err}:
			default:
			}
		}
		return nil

	case platform.RouteDeviceResourceManager:
		return c.enqueueIncomingRequest(msg, pm)
	case platform.RouteContextPublish:
		return c.handleIncomingPublication(ctx, msg, pm)
	default:
		return nil
	}
}

func (c *Client) handleIncomingPublication(
	ctx context.Context,
	msg relay.Received,
	pm platform.Message,
) error {
	envelope, err := msaep.Unmarshal(pm.Payload)
	if err != nil {
		return err
	}
	if envelope.MessageTag != int32(proto.ClipboardMessageTag) {
		return nil
	}
	publication, err := proto.UnmarshalPubSubPayload(envelope.Payload)
	if err != nil {
		return err
	}
	correlationID, err := proto.ParsePhoneClipboardChangePublication(publication)
	if err != nil {
		return err
	}
	generation := c.reserveRemoteGeneration()

	c.enqueuePhonePublication(phonePublication{
		correlationID: correlationID,
		generation:    generation,
	})
	return nil
}

func (c *Client) enqueuePhonePublication(publication phonePublication) {
	c.publicationMu.Lock()
	if c.phonePullCancel != nil {
		c.phonePullCancel()
	}
	c.publicationMu.Unlock()

	select {
	case c.publications <- publication:
		return
	default:
	}

	select {
	case <-c.publications:
	default:
	}
	select {
	case c.publications <- publication:
	default:
	}
}

func (c *Client) runPhonePublicationWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case publication := <-c.publications:
			pullCtx, cancel := context.WithCancel(ctx)
			c.publicationMu.Lock()
			c.phonePullCancel = cancel
			c.publicationMu.Unlock()

			err := c.pullToLocal(
				pullCtx,
				publication.correlationID,
				publication.generation,
			)
			cancel()

			c.publicationMu.Lock()
			c.phonePullCancel = nil
			c.publicationMu.Unlock()

			if err == nil {
				continue
			}
			if errors.Is(err, context.Canceled) && ctx.Err() == nil {
				continue
			}
			select {
			case c.asyncErr <- err:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (c *Client) enqueueIncomingRequest(msg relay.Received, pm platform.Message) error {
	request := incomingRequest{msg: msg, pm: pm}
	select {
	case c.incomingRequests <- request:
		return nil
	default:
		return errors.New("clipboard: incoming request queue is full")
	}
}

func (c *Client) runIncomingRequestWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-c.incomingRequests:
			if err := c.handleIncomingRequest(ctx, request.msg, request.pm); err != nil {
				select {
				case c.asyncErr <- err:
				case <-ctx.Done():
					return
				}
			}
		}
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
		return c.sendResponse(ctx, msg.Source, requestID, proto.DeviceResourceResponse{
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
		c.mu.Lock()
		c.prunePublishedLocked(time.Now())
		snapshot, published := c.published[req.CorrelationID]
		c.mu.Unlock()
		if published && snapshot.superseded {
			response = proto.Response{
				Status:        proto.ResponseInvalidContent,
				CorrelationID: req.CorrelationID,
				ErrorType:     proto.ErrorReject,
				ErrorDetail:   "clipboard publication was superseded by a newer remote change",
			}
		} else if published {
			response = proto.NewTextResponse(req.CorrelationID, snapshot.text, nil)
		} else if c.local == nil {
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
	return c.sendResponse(ctx, msg.Source, requestID, proto.DeviceResourceResponse{
		ResponseType: proto.DeviceResourceResponseSuccess,
		Payload:      proto.MarshalResponse(response),
	})
}

func (c *Client) sendResponse(ctx context.Context, target, requestID string, response proto.DeviceResourceResponse) error {
	pm := platform.NewInternalResponse(proto.MarshalDeviceResourceResponse(response), requestID)
	wire, err := platform.Marshal(pm)
	if err != nil {
		return err
	}
	return c.relay.Send(ctx, target, "", dcg.TransportMessageTypePlatform, wire)
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
