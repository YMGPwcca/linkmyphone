package notifications

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/YMGPwcca/linkmyphone/protocol/app"
	"github.com/YMGPwcca/linkmyphone/protocol/dcg"
	wire "github.com/YMGPwcca/linkmyphone/protocol/notifications"
	"github.com/YMGPwcca/linkmyphone/protocol/platform"
	"github.com/YMGPwcca/linkmyphone/transport/relay"
)

var (
	ErrNotReady    = errors.New("notifications: phone notification session is not ready")
	ErrPermission  = errors.New("notifications: notification access is not granted on the phone")
	ErrStaleAction = errors.New("notifications: notification or action is no longer current")
	ErrReceiveOnly = errors.New("notifications: remote actions are disabled")
)

type Transport interface {
	Send(context.Context, string, string, dcg.TransportMessageType, []byte) error
	Received() <-chan relay.Received
}

type Config struct {
	Target         string
	Info           wire.LocalInfo
	RequestTimeout time.Duration
	RemoteActions  bool
	ShowExisting   bool
	ReplyEnabled   bool
	Prompt         func(context.Context, ReplyPrompt) (string, bool, error)
	OnEvent        func(string, map[string]string)
}

type ConnectStatus struct{ ECR bool }
type requestResult struct {
	values app.ValueSet
	err    error
}
type sendJob struct {
	payload   []byte
	sessionID string
	ctx       context.Context
	requestID string
	guards    []mutationGuard
}

type mutationGuard struct {
	key       string
	revision  uint64
	clearable bool
}

type Client struct {
	transport        Transport
	native           NativeBackend
	cfg              Config
	sends            chan sendJob
	jobs             chan NativeEvent
	pendingMu        sync.Mutex
	pending          map[string]chan requestResult
	stateMu          sync.Mutex
	ready            bool
	records          map[string]*record
	desktopIDs       map[uint32]string
	stateBytes       int
	revision         uint64
	dedupe           map[string]int32
	dedupeOrder      []string
	failures         chan error
	prompts          sync.WaitGroup
	promptSlots      chan struct{}
	desktopAvailable bool
	closed           chan struct{}
	runOnce          sync.Once
}

func New(transport Transport, native NativeBackend, cfg Config) (*Client, error) {
	if transport == nil || native == nil || cfg.Target == "" {
		return nil, errors.New("notifications: transport, desktop and target are required")
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 10 * time.Second
	}
	if cfg.Prompt == nil {
		cfg.Prompt = PromptReply
	}
	return &Client{transport: transport, native: native, cfg: cfg, sends: make(chan sendJob, 128), jobs: make(chan NativeEvent, 64), pending: make(map[string]chan requestResult), records: make(map[string]*record), desktopIDs: make(map[uint32]string), dedupe: make(map[string]int32), closed: make(chan struct{}), failures: make(chan error, 1), promptSlots: make(chan struct{}, 4), desktopAvailable: true}, nil
}

// Run owns receive processing and all worker lifetimes. A new host generation
// gets a new client; pending mutations are never replayed into its successor.
func (c *Client) Run(ctx context.Context) error {
	started := false
	c.runOnce.Do(func() { started = true })
	if !started {
		return errors.New("notifications: client already ran")
	}
	runCtx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	failures := c.failures
	workers.Add(2)
	go func() { defer workers.Done(); c.sendLoop(runCtx, failures) }()
	go func() { defer workers.Done(); c.actionLoop(runCtx) }()
	defer func() {
		c.stateMu.Lock()
		c.ready = false
		c.stateMu.Unlock()
		cancel()
		c.failPending(context.Canceled)
		workers.Wait()
		c.prompts.Wait()
		c.closeLocal()
		_ = c.native.Close()
		close(c.closed)
	}()
	for {
		select {
		case <-runCtx.Done():
			return runCtx.Err()
		case err := <-failures:
			return err
		case received, ok := <-c.transport.Received():
			if !ok {
				return errors.New("notifications: transport subscription closed")
			}
			if received.Source != c.cfg.Target || received.TransportMessageType != dcg.TransportMessageTypeApp {
				continue
			}
			envelope, err := app.UnmarshalEnvelope(received.Payload)
			if err != nil {
				c.event("malformed APP envelope", map[string]string{"stage": "frame", "reason": err.Error()})
				continue
			}
			route, _ := envelope.Header(platform.HeaderRoute)
			if route == platform.RouteInternalResponse {
				id, _ := envelope.Header(platform.HeaderOriginalRequestID)
				c.pendingMu.Lock()
				_, pending := c.pending[id]
				c.pendingMu.Unlock()
				if !pending {
					continue
				}
			} else if route != wire.RoutePhoneContent {
				continue
			}
			values, err := app.Unmarshal(envelope.Payload)
			if err != nil {
				c.event("malformed APP envelope", map[string]string{"stage": "values", "reason": err.Error()})
				continue
			}
			if route == platform.RouteInternalResponse {
				id, _ := envelope.Header(platform.HeaderOriginalRequestID)
				c.complete(id, requestResult{values: values})
			} else if values["contentType"] == wire.ContentType {
				if err := c.handleBatch(runCtx, received, app.Message{Headers: envelope.Headers, Values: values}); err != nil {
					return err
				}
			}
		case event, ok := <-c.native.Events():
			if !ok {
				return errors.New("notifications: desktop event stream closed")
			}
			switch event.Kind {
			case NativeFailure:
				return fmt.Errorf("notifications: desktop failed: %w", event.Err)
			case NativeReset, NativeUnavailable:
				available := event.Kind == NativeReset
				if err := c.resetDesktop(runCtx, available); err != nil {
					return err
				}
			case NativeActivationToken:
				c.stateMu.Lock()
				if key, ok := c.desktopIDs[event.ID]; ok {
					c.records[key].activationToken = event.ActivationToken
				}
				c.stateMu.Unlock()
			default:
				select {
				case c.jobs <- event:
				default:
					return errors.New("notifications: desktop action queue overflow")
				}
			}
		}
	}
}

func (c *Client) sendLoop(ctx context.Context, failures chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-c.sends:
			if err := c.checkGuards(job.guards); err != nil {
				c.complete(job.requestID, requestResult{err: err})
				continue
			}
			sendCtx, cancel := context.WithTimeout(job.ctx, c.cfg.RequestTimeout)
			stop := context.AfterFunc(ctx, cancel)
			err := c.transport.Send(sendCtx, c.cfg.Target, job.sessionID, dcg.TransportMessageTypeApp, job.payload)
			stop()
			cancel()
			if err != nil {
				if job.requestID != "" {
					c.complete(job.requestID, requestResult{err: err})
				} else {
					select {
					case failures <- fmt.Errorf("notifications: application response send failed: %w", err):
					default:
					}
					return
				}
			}
		}
	}
}

func (c *Client) complete(id string, result requestResult) {
	c.pendingMu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- result
	}
}
func (c *Client) failPending(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- requestResult{err: err}
	}
}

func (c *Client) request(ctx context.Context, route string, values app.ValueSet, guards []mutationGuard) (app.ValueSet, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	id, err := requestID()
	if err != nil {
		return nil, err
	}
	payload, err := app.MarshalMessage(app.NewRequest(route, id, values))
	if err != nil {
		return nil, err
	}
	ch := make(chan requestResult, 1)
	c.pendingMu.Lock()
	if len(c.pending) >= 64 {
		c.pendingMu.Unlock()
		return nil, errors.New("notifications: too many pending requests")
	}
	c.pending[id] = ch
	c.pendingMu.Unlock()
	defer func() { c.pendingMu.Lock(); delete(c.pending, id); c.pendingMu.Unlock() }()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, ErrNotReady
	case c.sends <- sendJob{payload: payload, ctx: ctx, requestID: id, guards: guards}:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, ErrNotReady
	case result := <-ch:
		if result.err != nil {
			return nil, result.err
		}
		status, ok := result.values["result"].(int32)
		if !ok {
			return nil, errors.New("notifications: response lacks typed Int32 result")
		}
		if status == 7 {
			c.stateMu.Lock()
			c.ready = false
			c.stateMu.Unlock()
			select {
			case c.failures <- ErrPermission:
			default:
			}
			return nil, ErrPermission
		}
		if status != 0 {
			return nil, fmt.Errorf("notifications: phone rejected request (result %d)", status)
		}
		return result.values, nil
	}
}

func (c *Client) Connect(ctx context.Context) (ConnectStatus, error) {
	keys, times := c.reconcileState()
	cv, err := requestID()
	if err != nil {
		return ConnectStatus{}, err
	}
	values, err := wire.ConnectRequest(c.cfg.Info, cv, keys, times)
	if err != nil {
		return ConnectStatus{}, err
	}
	response, err := c.request(ctx, wire.RouteConnect, values, nil)
	if err != nil {
		return ConnectStatus{}, err
	}
	permissions, ok := response["permissions"].(app.ValueSet)
	if !ok || permissions["notifications"] != true {
		return ConnectStatus{}, ErrPermission
	}
	caps, ok := response["capabilities"].(app.ValueSet)
	if !ok {
		return ConnectStatus{}, errors.New("notifications: connect response lacks capabilities")
	}
	notificationCaps, ok := caps["notifications"].(int32)
	if !ok {
		return ConnectStatus{}, errors.New("notifications: connect response lacks typed notification capabilities")
	}
	status := ConnectStatus{ECR: notificationCaps&2 != 0}
	if !status.ECR {
		if err := c.reconcile(ctx); err != nil {
			return ConnectStatus{}, err
		}
	}
	c.stateMu.Lock()
	c.ready = true
	c.stateMu.Unlock()
	return status, nil
}

func (c *Client) Reconcile(ctx context.Context) error {
	c.stateMu.Lock()
	ready := c.ready
	c.stateMu.Unlock()
	if !ready {
		return ErrNotReady
	}
	return c.reconcile(ctx)
}
func (c *Client) reconcile(ctx context.Context) error {
	keys, times := c.reconcileState()
	cv, err := requestID()
	if err != nil {
		return err
	}
	values, err := wire.ReconcileRequest(c.cfg.Info, cv, keys, times)
	if err != nil {
		return err
	}
	_, err = c.request(ctx, wire.RouteNotifications, values, nil)
	return err
}

func (c *Client) handleBatch(ctx context.Context, received relay.Received, message app.Message) error {
	id, _ := message.Header(platform.HeaderRequestID)
	if id == "" {
		c.event("notification push lacks request correlation", nil)
		return nil
	}
	result := int32(0)
	batch, decodeErr := wire.DecodeBatch(message.Values)
	if decodeErr != nil {
		result = 1
		c.event("malformed notification batch rejected", nil)
	} else {
		identity := id
		if batch.DedupeID != "" {
			identity = "dedupe:" + batch.DedupeID
		}
		c.stateMu.Lock()
		prior, duplicate := c.dedupe[identity]
		c.stateMu.Unlock()
		if duplicate {
			result = prior
		} else {
			if err := c.applyBatch(ctx, batch); err != nil {
				result = 1
				c.event("notification batch failed", map[string]string{"reason": err.Error()})
			}
			if result == 0 {
				c.remember(identity, result)
			}
		}
	}
	response := wire.BaseValues(c.cfg.Info, batch.CorrelationVector)
	response["contentType"], response["result"], response["itemCount"] = wire.ContentType, result, int32(len(batch.Operations))
	payload, err := app.MarshalMessage(app.NewResponse(id, response))
	if err != nil {
		return err
	}
	select {
	case c.sends <- sendJob{payload: payload, sessionID: received.SessionID, ctx: ctx}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.New("notifications: application response queue overflow")
	}
}

func (c *Client) remember(id string, result int32) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if len(c.dedupeOrder) == 256 {
		delete(c.dedupe, c.dedupeOrder[0])
		copy(c.dedupeOrder, c.dedupeOrder[1:])
		c.dedupeOrder = c.dedupeOrder[:255]
	}
	c.dedupe[id] = result
	c.dedupeOrder = append(c.dedupeOrder, id)
}
func (c *Client) event(message string, fields map[string]string) {
	if c.cfg.OnEvent != nil {
		c.cfg.OnEvent(message, fields)
	}
}
func requestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func (c *Client) checkGuards(guards []mutationGuard) error {
	if len(guards) == 0 {
		return nil
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	for _, guard := range guards {
		if err := c.authorize(guard.key, guard.revision); err != nil {
			return err
		}
		if guard.clearable && !c.records[guard.key].item.IsClearable {
			return ErrStaleAction
		}
	}
	return nil
}
