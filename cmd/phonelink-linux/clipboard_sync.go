package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/YMGPwcca/phonelink-linux/auth/dcgauth"
	"github.com/YMGPwcca/phonelink-linux/auth/msa"
	authstate "github.com/YMGPwcca/phonelink-linux/auth/state"
	"github.com/YMGPwcca/phonelink-linux/bootstrap"
	clipclient "github.com/YMGPwcca/phonelink-linux/clipboard"
	clipproto "github.com/YMGPwcca/phonelink-linux/protocol/clipboard"
	servicedcg "github.com/YMGPwcca/phonelink-linux/services/dcg"
)

const (
	defaultClipboardPollInterval = 500 * time.Millisecond
	remoteClipboardSettleWindow  = 3 * time.Second
)

type clipboardSyncOptions struct {
	statePath      string
	appVersion     string
	ringName       string
	osVersion      string
	target         string
	signalrTimeout time.Duration
	wakeTimeout    time.Duration
	wakeTTL        time.Duration
	requestTimeout time.Duration
	pollInterval   time.Duration
	publishInitial bool
}

type trackedLocalClipboard struct {
	base clipclient.Local

	writeMu sync.Mutex
	mu      sync.Mutex

	lastExactHash    [32]byte
	lastTrackingHash [32]byte
	initialized      bool
	applying         bool
	suppress         map[[32]byte]time.Time
	remoteWrite      chan int
}

func newTrackedLocalClipboard(base clipclient.Local, initial string) *trackedLocalClipboard {
	return &trackedLocalClipboard{
		base:        base,
		lastExactHash:    sha256.Sum256([]byte(initial)),
		lastTrackingHash: clipboardTrackingHash(initial),
		initialized:      true,
		suppress:    make(map[[32]byte]time.Time),
		remoteWrite: make(chan int, 8),
	}
}

func (l *trackedLocalClipboard) ReadText(ctx context.Context) (string, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	return l.base.ReadText(ctx)
}

func (l *trackedLocalClipboard) WriteText(ctx context.Context, text string) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	nextExactHash := sha256.Sum256([]byte(text))
	nextTrackingHash := clipboardTrackingHash(text)
	now := time.Now()

	l.mu.Lock()
	l.pruneSuppressedLocked(now)
	l.applying = true
	if l.initialized {
		l.suppress[l.lastTrackingHash] = now.Add(remoteClipboardSettleWindow)
	}
	l.suppress[nextTrackingHash] = now.Add(remoteClipboardSettleWindow)
	l.mu.Unlock()

	err := l.base.WriteText(ctx, text)

	l.mu.Lock()
	l.applying = false
	if err == nil {
		l.lastExactHash = nextExactHash
		l.lastTrackingHash = nextTrackingHash
		l.initialized = true
	}
	l.mu.Unlock()
	if err != nil {
		return err
	}

	select {
	case l.remoteWrite <- len([]byte(text)):
	default:
	}
	return nil
}

func (l *trackedLocalClipboard) MarkIfChanged(text string) bool {
	exactHash := sha256.Sum256([]byte(text))
	trackingHash := clipboardTrackingHash(text)
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneSuppressedLocked(now)

	if l.applying {
		return false
	}
	if until, ok := l.suppress[trackingHash]; ok && now.Before(until) {
		l.lastExactHash = exactHash
		l.lastTrackingHash = trackingHash
		l.initialized = true
		delete(l.suppress, trackingHash)
		return false
	}
	if l.initialized && l.lastExactHash == exactHash {
		return false
	}
	l.lastExactHash = exactHash
	l.lastTrackingHash = trackingHash
	l.initialized = true
	return true
}

func clipboardTrackingHash(text string) [32]byte {
	// Clipboard providers and Android clients can normalize CRLF to LF and some
	// Wayland paths preserve or add one terminal newline. Those representations
	// are equivalent for echo suppression only; the actual clipboard payload is
	// never modified before being sent or applied.
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.TrimSuffix(normalized, "\n")
	return sha256.Sum256([]byte(normalized))
}

func (l *trackedLocalClipboard) pruneSuppressedLocked(now time.Time) {
	for hash, until := range l.suppress {
		if !now.Before(until) {
			delete(l.suppress, hash)
		}
	}
}

type clipboardPublishResult struct {
	size          int
	correlationID string
	err           error
}

func startClipboardPublisher(
	ctx context.Context,
	client *clipclient.Client,
) (chan string, <-chan clipboardPublishResult) {
	queue := make(chan string, 1)
	results := make(chan clipboardPublishResult, 4)

	go func() {
		defer close(results)
		for {
			select {
			case <-ctx.Done():
				return
			case text := <-queue:
				correlationID, err := client.PublishLocalText(ctx, text, "")
				result := clipboardPublishResult{
					size:          len([]byte(text)),
					correlationID: correlationID,
					err:           err,
				}
				select {
				case results <- result:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return queue, results
}

func queueLatestClipboardText(queue chan string, text string) {
	select {
	case queue <- text:
		return
	default:
	}

	select {
	case <-queue:
	default:
	}

	select {
	case queue <- text:
	default:
	}
}

func runClipboardSync(ctx context.Context, args []string) error {
	defaultStatePath, err := authstate.DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve state path: %w", err)
	}

	fs := flag.NewFlagSet("clipboard-sync", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := clipboardSyncOptions{}
	fs.StringVar(&opts.statePath, "state", defaultStatePath, "persistent bootstrap state path")
	fs.StringVar(&opts.appVersion, "app-version", defaultAppVersion, "CrossDevice app version advertised to DCG")
	fs.StringVar(&opts.ringName, "ring", defaultRingName, "CrossDevice ring name")
	fs.StringVar(&opts.osVersion, "os-version", defaultOSVersion, "Windows-compatible OS version advertised to DCG")
	fs.StringVar(&opts.target, "target", "", "target linked peer id/name; defaults to the sole linked Android device")
	fs.DurationVar(&opts.signalrTimeout, "signalr-timeout", bootstrap.DefaultOnConnectedTimeout, "time to wait for SignalR OnConnected")
	fs.DurationVar(&opts.wakeTimeout, "wake-timeout", bootstrap.DefaultPeerWakeTimeout, "time to wait for target peer presence after wake")
	fs.DurationVar(&opts.wakeTTL, "wake-ttl", bootstrap.DefaultPeerWakeTTL, "Dispatcher wake time-to-live")
	fs.DurationVar(&opts.requestTimeout, "request-timeout", clipclient.DefaultRequestTimeout, "time to wait for clipboard and SessionValidation responses")
	fs.DurationVar(&opts.pollInterval, "poll-interval", defaultClipboardPollInterval, "local clipboard polling interval")
	fs.BoolVar(&opts.publishInitial, "publish-initial", false, "publish the current Linux clipboard immediately after startup")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if opts.signalrTimeout <= 0 ||
		opts.wakeTimeout <= 0 ||
		opts.wakeTTL <= 0 ||
		opts.requestTimeout <= 0 ||
		opts.pollInterval <= 0 {
		return errors.New("all timeout/TTL/poll values must be positive")
	}

	fmt.Println("Phone Link Linux clipboard sync")
	fmt.Printf("State: %s\n", opts.statePath)
	fmt.Println("Clipboard contents are not printed.")
	fmt.Println()

	native, err := clipclient.DetectNativeLocal()
	if err != nil {
		return err
	}
	initialText, err := native.ReadText(ctx)
	if err != nil {
		return fmt.Errorf("read initial Linux clipboard: %w", err)
	}
	local := newTrackedLocalClipboard(native, initialText)
	fmt.Printf("[local] Clipboard backend: %s\n", native.BackendName())
	fmt.Printf("[local] Initial text snapshot: %d bytes\n", len([]byte(initialText)))

	snapshot, err := authstate.Load(opts.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("no persisted enrollment found; run bootstrap-probe first")
		}
		return fmt.Errorf("load state: %w", err)
	}
	if snapshot.Enrollment.AccountCert == "" {
		return errors.New("persisted enrollment has no account certificate")
	}

	clientInfo, err := buildClientInfo(probeOptions{
		appVersion: opts.appVersion,
		ringName:   opts.ringName,
		osVersion:  opts.osVersion,
	}, snapshot.LogicalDeviceID)
	if err != nil {
		return err
	}

	msaClient := msa.NewDeviceCodeClient()
	authClient := dcgauth.NewClient(dcgauth.ProdServiceBase)
	serviceClient := servicedcg.NewClient(servicedcg.ProdServiceBase)
	configureDCGClients(authClient, serviceClient, clientInfo)

	fmt.Println("[1/5] Resuming Microsoft + DCG identity...")
	resumed, err := bootstrap.ResumeAuth(ctx, msaClient, authClient, snapshot)
	if err != nil {
		return fmt.Errorf("resume authentication: %w", err)
	}
	snapshot = resumed.State
	if err := authstate.Save(opts.statePath, snapshot); err != nil {
		return fmt.Errorf("persist refreshed authentication: %w", err)
	}
	fmt.Printf("[1/5] Authentication: OK (DCG %s)\n", shortID(resumed.Identity.DeviceID))

	fmt.Println("[2/5] Refreshing trust and selecting Android peer...")
	trust, err := bootstrap.SyncTrust(
		ctx,
		serviceClient,
		resumed.MSAToken.AccessToken,
		resumed.Identity.DeviceID,
		snapshot.Enrollment.AccountCert,
		"",
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("trust sync: %w", err)
	}
	snapshot.TrustRelationships = append([]dcgauth.TrustRelationship(nil), trust.Relationships()...)
	if err := authstate.Save(opts.statePath, snapshot); err != nil {
		return fmt.Errorf("persist trust state: %w", err)
	}
	target, err := selectPeer(trust.Devices, resumed.Identity.DeviceID, opts.target)
	if err != nil {
		return err
	}
	fmt.Printf(
		"[2/5] Target: %s / %s / DCG %s\n",
		peerDisplayName(target),
		peerOSName(target),
		shortID(target.ID),
	)
	fmt.Println("[2/5] Trust: OK")

	fmt.Println("[3/5] Connecting Hub Relay and ensuring peer presence...")
	cloud, err := bootstrap.OpenCloudRelay(ctx, bootstrap.CloudConfig{
		Services:           serviceClient,
		MSAAccessToken:     resumed.MSAToken.AccessToken,
		DCGAccessToken:     resumed.ServicesToken.Token,
		ClientInfo:         clientInfo,
		OnConnectedTimeout: opts.signalrTimeout,
	})
	if err != nil {
		return fmt.Errorf("SignalR bootstrap: %w", err)
	}
	defer cloud.Close()
	fmt.Printf("[3/5] SignalR shard: %s\n", cloud.Region)

	woke, err := bootstrap.EnsurePeerOnline(
		ctx,
		cloud,
		serviceClient,
		resumed.TrustIdentity,
		resumed.Identity.DeviceID,
		target.ID,
		resumed.ServicesToken.Token,
		bootstrap.PeerOnlineOptions{
			Timeout:           opts.wakeTimeout,
			WakeTTL:           opts.wakeTTL,
			RequestNewSession: true,
		},
	)
	if err != nil {
		return fmt.Errorf("peer presence: %w", err)
	}
	if woke {
		fmt.Println("[3/5] Signed wake: accepted; target appeared on Hub Relay")
	}
	fmt.Println("[3/5] Peer presence: OK")

	fmt.Println("[4/5] Validating PLATFORM session...")
	if _, err := bootstrap.ValidatePlatformSession(
		ctx,
		cloud.Relay,
		target.ID,
		opts.requestTimeout,
	); err != nil {
		return fmt.Errorf("SessionValidation: %w", err)
	}
	fmt.Println("[4/5] SessionValidation: OK")

	fmt.Println("[5/5] Starting clipboard protocol...")
	client := clipclient.New(cloud.Relay, local, clipclient.Config{
		Target:          target.ID,
		SelfDcgClientID: resumed.Identity.DeviceID,
		RequestTimeout:  opts.requestTimeout,
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- client.Run(ctx)
	}()

	featureCtx, cancelFeature := context.WithTimeout(ctx, opts.requestTimeout)
	featureStatus, featureErr := client.PushFeatureState(featureCtx, clipproto.RequestFeatureOn)
	cancelFeature()
	if featureErr != nil {
		fmt.Printf("[5/5] FEATURE_ON sync: warning: %v\n", featureErr)
	} else {
		fmt.Printf("[5/5] FEATURE_ON sync: response status %d\n", featureStatus)
	}

	if opts.publishInitial {
		correlationID, err := client.PublishLocalText(ctx, initialText, "")
		if err != nil {
			return fmt.Errorf("publish initial clipboard: %w", err)
		}
		fmt.Printf(
			"[5/5] Initial Linux -> phone publication: %d bytes / correlation %s\n",
			len([]byte(initialText)),
			shortID(correlationID),
		)
	}

	publishQueue, publishResults := startClipboardPublisher(ctx, client)

	fmt.Printf("[5/5] Clipboard sync: RUNNING (poll %s)\n", opts.pollInterval)
	fmt.Println()
	fmt.Println("[OK] Copy text on Linux or on the phone. Press Ctrl+C to stop.")

	ticker := time.NewTicker(opts.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case err := <-runErr:
			if err == nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("clipboard receive loop: %w", err)

		case result, ok := <-publishResults:
			if !ok {
				return nil
			}
			if result.err != nil {
				return fmt.Errorf("publish Linux clipboard: %w", result.err)
			}
			fmt.Printf(
				"[sync] Linux -> phone: published text (%d bytes / correlation %s)\n",
				result.size,
				shortID(result.correlationID),
			)

		case size := <-local.remoteWrite:
			fmt.Printf("[sync] phone -> Linux: applied text (%d bytes)\n", size)

		case <-ticker.C:
			text, err := local.ReadText(ctx)
			if err != nil {
				return fmt.Errorf("poll Linux clipboard: %w", err)
			}
			if !local.MarkIfChanged(text) {
				continue
			}
			queueLatestClipboardText(publishQueue, text)
		}
	}
}
