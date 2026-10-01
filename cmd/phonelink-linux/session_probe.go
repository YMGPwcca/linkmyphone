package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/YMGPwcca/phonelink-linux/auth/dcgauth"
	"github.com/YMGPwcca/phonelink-linux/auth/msa"
	authstate "github.com/YMGPwcca/phonelink-linux/auth/state"
	"github.com/YMGPwcca/phonelink-linux/bootstrap"
	clipproto "github.com/YMGPwcca/phonelink-linux/protocol/clipboard"
	"github.com/YMGPwcca/phonelink-linux/protocol/platform"
	sessionproto "github.com/YMGPwcca/phonelink-linux/protocol/sessionvalidation"
	servicedcg "github.com/YMGPwcca/phonelink-linux/services/dcg"
)

type sessionProbeOptions struct {
	statePath      string
	appVersion     string
	ringName       string
	osVersion      string
	target         string
	signalrTimeout time.Duration
	wakeTimeout    time.Duration
	wakeTTL        time.Duration
	requestTimeout time.Duration
	contextProbe   bool
	contextTimeout time.Duration
	contextText    string
}

func runSessionProbe(ctx context.Context, args []string) error {
	defaultStatePath, err := authstate.DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve state path: %w", err)
	}

	fs := flag.NewFlagSet("session-probe", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := sessionProbeOptions{}
	fs.StringVar(&opts.statePath, "state", defaultStatePath, "persistent bootstrap state path")
	fs.StringVar(&opts.appVersion, "app-version", defaultAppVersion, "CrossDevice app version advertised to DCG")
	fs.StringVar(&opts.ringName, "ring", defaultRingName, "CrossDevice ring name")
	fs.StringVar(&opts.osVersion, "os-version", defaultOSVersion, "Windows-compatible OS version advertised to DCG")
	fs.StringVar(&opts.target, "target", "", "target linked peer id/name; defaults to the sole linked Android device")
	fs.DurationVar(&opts.signalrTimeout, "signalr-timeout", bootstrap.DefaultOnConnectedTimeout, "time to wait for SignalR OnConnected")
	fs.DurationVar(&opts.wakeTimeout, "wake-timeout", bootstrap.DefaultPeerWakeTimeout, "time to wait for target peer presence after wake")
	fs.DurationVar(&opts.wakeTTL, "wake-ttl", bootstrap.DefaultPeerWakeTTL, "Dispatcher wake time-to-live")
	fs.DurationVar(&opts.requestTimeout, "request-timeout", bootstrap.DefaultSessionValidationTimeout, "time to wait for /SessionValidation response")
	fs.BoolVar(&opts.contextProbe, "context-probe", false, "publish a source-confirmed clipboard ContextSource probe after SessionValidation")
	fs.DurationVar(&opts.contextTimeout, "context-timeout", bootstrap.DefaultContextProbeTimeout, "time to observe the Android PLATFORM reaction to /Context/Publish")
	fs.StringVar(&opts.contextText, "context-text", "", "explicit probe text to return if Android requests clipboard CONTENT; omitted by default")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if opts.signalrTimeout <= 0 || opts.wakeTimeout <= 0 || opts.wakeTTL <= 0 || opts.requestTimeout <= 0 || opts.contextTimeout <= 0 {
		return errors.New("all timeout/TTL values must be positive")
	}
	if opts.contextText != "" && !opts.contextProbe {
		return errors.New("--context-text requires --context-probe")
	}

	fmt.Println("Phone Link Linux SessionValidation probe")
	fmt.Printf("State: %s\n", opts.statePath)
	fmt.Println("Secrets and raw platform payloads are not printed.")
	fmt.Println()

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

	fmt.Println("[1/4] Resuming persisted Microsoft + DCG identity...")
	resumed, err := bootstrap.ResumeAuth(ctx, msaClient, authClient, snapshot)
	if err != nil {
		return fmt.Errorf("resume authentication: %w", err)
	}
	snapshot = resumed.State
	if err := authstate.Save(opts.statePath, snapshot); err != nil {
		return fmt.Errorf("persist refreshed authentication: %w", err)
	}
	fmt.Printf("[1/4] Authentication: OK (DCG %s)\n", shortID(resumed.Identity.DeviceID))

	fmt.Println("[2/4] Refreshing trust and selecting Android peer...")
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
		"[2/4] Target: %s / %s / DCG %s\n",
		peerDisplayName(target),
		peerOSName(target),
		shortID(target.ID),
	)
	fmt.Println("[2/4] Trust: OK")

	fmt.Println("[3/4] Connecting Hub Relay and ensuring peer presence...")
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
	fmt.Printf("[3/4] SignalR shard: %s\n", cloud.Region)

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
		fmt.Println("[3/4] Signed wake: accepted; target appeared on Hub Relay")
	} else {
		fmt.Println("[3/4] Target already present; wake not needed")
	}
	fmt.Println("[3/4] Peer presence: OK")

	fmt.Println("[4/4] Sending PLATFORM /SessionValidation...")
	validation, err := bootstrap.ValidatePlatformSession(
		ctx,
		cloud.Relay,
		target.ID,
		opts.requestTimeout,
	)
	if err != nil {
		return fmt.Errorf("SessionValidation: %w", err)
	}

	fmt.Printf("[4/4] Response route: %s\n", platform.RouteInternalResponse)
	fmt.Printf("[4/4] Response headers: %s\n", strings.Join(platformHeaderNames(validation.Headers), ", "))
	fmt.Printf("[4/4] Capabilities: %s\n", strings.Join(sessionCapabilityNames(validation.Response.Capabilities), ", "))
	fmt.Printf("[4/4] PersistentMessagingChannelVersion: %d\n", validation.Response.PersistentMessagingChannelVersion)
	fmt.Printf("[4/4] NanoTransportPreferenceVersion: %d\n", validation.Response.NanoTransportPreferenceVersion)
	fmt.Println("[4/4] SessionValidation: OK")

	if !opts.contextProbe {
		fmt.Println()
		fmt.Println("[OK] S23 accepted PLATFORM /SessionValidation; ready for ContextSource probing.")
		return nil
	}

	fmt.Println()
	fmt.Println("[context] Sending clipboard tag 9 through PLATFORM /Context/Publish...")
	contextOptions := bootstrap.ContextProbeOptions{
		Timeout: opts.contextTimeout,
	}
	if opts.contextText != "" {
		contextOptions.ContentText = &opts.contextText
	}
	contextResult, err := bootstrap.ProbeClipboardContextPublishWithOptions(
		ctx,
		cloud.Relay,
		target.ID,
		resumed.Identity.DeviceID,
		contextOptions,
	)
	if err != nil {
		return fmt.Errorf("ContextSource probe: %w", err)
	}
	fmt.Println("[context] /Context/Publish DCG acknowledgement: OK")

	if contextResult.Route == "" {
		fmt.Printf("[context] No matching PLATFORM follow-up within %s.\n", opts.contextTimeout)
		fmt.Println("[OK] Context publication reached DCG transport; peer application reaction is still unknown.")
		return nil
	}

	fmt.Printf("[context] Peer route: %s\n", contextResult.Route)
	fmt.Printf("[context] Peer headers: %s\n", strings.Join(platformHeaderNames(contextResult.Headers), ", "))
	if contextResult.RejectedReason != "" && contextResult.RejectedReason != "None" {
		return fmt.Errorf("ContextSource probe rejected by peer: %s", contextResult.RejectedReason)
	}

	switch contextResult.Route {
	case platform.RouteDeviceResourceManager:
		for i, request := range contextResult.ClipboardRequests {
			fmt.Printf(
				"[context] Clipboard request #%d: %s / %s / %s\n",
				i+1,
				request.ResourcePath,
				deviceResourceRequestTypeName(request.DeviceResourceRequestType),
				clipboardRequestTypeName(request.ClipboardRequestType),
			)
			if request.CorrelationID == contextResult.CorrelationID {
				fmt.Printf("[context] Clipboard correlation #%d: matched publication\n", i+1)
			} else if request.CorrelationID != "" {
				fmt.Printf("[context] Clipboard correlation #%d: peer used a different id\n", i+1)
			}
		}
		if contextResult.StatusFeatureOnSent {
			fmt.Println("[context] STATUS response: FEATURE_ON")
		}
		if contextResult.ContentSent {
			fmt.Printf("[context] CONTENT response: text/plain sent (%d bytes).\n", contextResult.ContentTextBytes)
			fmt.Println()
			fmt.Println("[OK] S23 requested CONTENT and DCG acknowledged the explicit text response.")
			fmt.Println("[verify] Paste on the phone to confirm the Android clipboard applied the probe text.")
		} else if contextResult.ContentDeclined {
			fmt.Println("[context] CONTENT response: ResourceHandlerNotRegistered; no clipboard content was sent.")
			fmt.Println()
			fmt.Println("[OK] S23 advanced from STATUS to CONTENT; clipboard pull handshake is live.")
		} else if contextResult.StatusFeatureOnSent {
			fmt.Printf("[context] No CONTENT follow-up within %s after FEATURE_ON.\n", opts.contextTimeout)
			fmt.Println()
			fmt.Println("[OK] S23 requested clipboard STATUS and accepted the probe response path.")
		} else {
			fmt.Println()
			fmt.Println("[OK] S23 received the Context publication and requested /clipboard; ContextSource delivery is live.")
		}

	case platform.RouteContextPublish:
		fmt.Printf("[context] Incoming MSAEP message tag: %d\n", contextResult.MessageTag)
		fmt.Println()
		fmt.Println("[OK] S23 returned ContextSource traffic after the publication.")

	case platform.RouteInternalResponse:
		fmt.Println()
		fmt.Println("[OK] S23 returned an internal response for the Context publication.")
	}

	return nil
}

func sessionCapabilityNames(capabilities []sessionproto.Capability) []string {
	out := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		out = append(out, sessionproto.Name(capability))
	}
	if len(out) == 0 {
		return []string{"none"}
	}
	return out
}

func platformHeaderNames(headers []platform.Header) []string {
	seen := make(map[string]struct{}, len(headers))
	for _, header := range headers {
		if header.Key != "" {
			seen[header.Key] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return []string{"none"}
	}
	return out
}

func deviceResourceRequestTypeName(requestType clipproto.DeviceResourceRequestType) string {
	switch requestType {
	case clipproto.DeviceResourceRequestGET:
		return "GET"
	case clipproto.DeviceResourceRequestUPDATE:
		return "UPDATE"
	case clipproto.DeviceResourceRequestDELETE:
		return "DELETE"
	case clipproto.DeviceResourceRequestSYNC:
		return "SYNC"
	default:
		return fmt.Sprintf("unknown(%d)", requestType)
	}
}

func clipboardRequestTypeName(requestType clipproto.RequestType) string {
	switch requestType {
	case clipproto.RequestContent:
		return "CONTENT"
	case clipproto.RequestFeatureOn:
		return "FEATURE_ON"
	case clipproto.RequestFeatureOff:
		return "FEATURE_OFF"
	case clipproto.RequestFeatureDisable:
		return "FEATURE_DISABLE"
	case clipproto.RequestStatus:
		return "STATUS"
	default:
		return fmt.Sprintf("unknown(%d)", requestType)
	}
}
