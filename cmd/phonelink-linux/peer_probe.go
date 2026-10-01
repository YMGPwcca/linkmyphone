package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/YMGPwcca/phonelink-linux/auth/dcgauth"
	"github.com/YMGPwcca/phonelink-linux/auth/msa"
	authstate "github.com/YMGPwcca/phonelink-linux/auth/state"
	"github.com/YMGPwcca/phonelink-linux/bootstrap"
	servicedcg "github.com/YMGPwcca/phonelink-linux/services/dcg"
)

type peerProbeOptions struct {
	statePath      string
	appVersion     string
	ringName       string
	osVersion      string
	target         string
	signalrTimeout time.Duration
	wakeTimeout    time.Duration
	wakeTTL        time.Duration
}

func runPeerProbe(ctx context.Context, args []string) error {
	defaultStatePath, err := authstate.DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve state path: %w", err)
	}

	fs := flag.NewFlagSet("peer-probe", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := peerProbeOptions{}
	fs.StringVar(&opts.statePath, "state", defaultStatePath, "persistent bootstrap state path")
	fs.StringVar(&opts.appVersion, "app-version", defaultAppVersion, "CrossDevice app version advertised to DCG")
	fs.StringVar(&opts.ringName, "ring", defaultRingName, "CrossDevice ring name")
	fs.StringVar(&opts.osVersion, "os-version", defaultOSVersion, "Windows-compatible OS version advertised to DCG")
	fs.StringVar(&opts.target, "target", "", "target linked peer id/name; defaults to the sole linked Android device")
	fs.DurationVar(&opts.signalrTimeout, "signalr-timeout", bootstrap.DefaultOnConnectedTimeout, "time to wait for SignalR OnConnected")
	fs.DurationVar(&opts.wakeTimeout, "wake-timeout", bootstrap.DefaultPeerWakeTimeout, "time to wait for target peer presence after wake")
	fs.DurationVar(&opts.wakeTTL, "wake-ttl", bootstrap.DefaultPeerWakeTTL, "Dispatcher wake time-to-live")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if opts.signalrTimeout <= 0 || opts.wakeTimeout <= 0 || opts.wakeTTL <= 0 {
		return errors.New("signalr timeout, wake timeout, and wake TTL must be positive")
	}

	fmt.Println("Phone Link Linux peer probe")
	fmt.Printf("State: %s\n", opts.statePath)
	fmt.Println("Secrets are not printed.")
	fmt.Println()

	snapshot, err := authstate.Load(opts.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("no persisted enrollment found; run bootstrap-probe first")
		}
		return fmt.Errorf("load state: %w", err)
	}
	if snapshot.Enrollment.AccountCert == "" {
		return errors.New("persisted enrollment has no account certificate; bootstrap state is incomplete")
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

	fmt.Println("[1/4] Refreshing Microsoft + DCG authentication...")
	resumed, err := bootstrap.ResumeAuth(ctx, msaClient, authClient, snapshot)
	if err != nil {
		return fmt.Errorf("resume authentication: %w", err)
	}
	snapshot = resumed.State
	if err := authstate.Save(opts.statePath, snapshot); err != nil {
		return fmt.Errorf("persist refreshed authentication: %w", err)
	}
	fmt.Printf("[1/4] Authentication: OK (DCG %s)\n", shortID(resumed.Identity.DeviceID))

	fmt.Println("[2/4] Refreshing linked-device trust...")
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
	fmt.Printf("[2/4] Trust sync: OK (%d relationship(s))\n", len(snapshot.TrustRelationships))

	fmt.Println("[3/4] Connecting account-level SignalR relay...")
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
	fmt.Println("[3/4] Hub OnConnected: OK")

	alreadyPresent := cloud.Relay.PartnerConnected(target.ID)
	if alreadyPresent {
		fmt.Println("[4/4] Target already present on Hub Relay; wake not needed.")
	} else {
		fmt.Printf(
			"[4/4] Target not present; sending signed Dispatcher/Wake and waiting up to %s...\n",
			opts.wakeTimeout,
		)
	}

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
		fmt.Println("[4/4] Wake accepted; target appeared on Hub Relay.")
	}
	fmt.Printf("[4/4] Peer presence: OK (DCG %s)\n", shortID(target.ID))

	fmt.Println()
	fmt.Println("[OK] Target peer is online on Hub Relay; ready for SessionValidation probing.")
	return nil
}

func selectPeer(devices []servicedcg.DeviceInfo, selfID, selector string) (servicedcg.DeviceInfo, error) {
	peers := servicedcg.LinkedPeers(devices, selfID)
	if len(peers) == 0 {
		return servicedcg.DeviceInfo{}, errors.New("peer probe: no linked peers returned by DeviceInfoList")
	}

	selector = strings.TrimSpace(selector)
	if selector == "" {
		android := make([]servicedcg.DeviceInfo, 0, 1)
		for _, peer := range peers {
			if strings.EqualFold(strings.TrimSpace(peer.OSName), "Android") {
				android = append(android, peer)
			}
		}
		switch len(android) {
		case 0:
			return servicedcg.DeviceInfo{}, errors.New("peer probe: no linked Android device found; use --target to select a peer")
		case 1:
			return android[0], nil
		default:
			return servicedcg.DeviceInfo{}, fmt.Errorf(
				"peer probe: %d linked Android devices found; use --target with a device name or DCG id",
				len(android),
			)
		}
	}

	for _, peer := range peers {
		if strings.EqualFold(peer.ID, selector) {
			return peer, nil
		}
	}
	exact := make([]servicedcg.DeviceInfo, 0, 1)
	for _, peer := range peers {
		if strings.EqualFold(strings.TrimSpace(peer.Name), selector) ||
			strings.EqualFold(strings.TrimSpace(peer.ModelName), selector) {
			exact = append(exact, peer)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return servicedcg.DeviceInfo{}, fmt.Errorf("peer probe: target %q is ambiguous", selector)
	}

	selectorLower := strings.ToLower(selector)
	partial := make([]servicedcg.DeviceInfo, 0, 1)
	for _, peer := range peers {
		haystack := strings.ToLower(peer.ID + "\n" + peer.Name + "\n" + peer.ModelName)
		if strings.Contains(haystack, selectorLower) {
			partial = append(partial, peer)
		}
	}
	if len(partial) == 1 {
		return partial[0], nil
	}
	if len(partial) > 1 {
		return servicedcg.DeviceInfo{}, fmt.Errorf("peer probe: target %q matches multiple linked devices", selector)
	}
	return servicedcg.DeviceInfo{}, fmt.Errorf("peer probe: target %q was not found among linked devices", selector)
}

func peerDisplayName(peer servicedcg.DeviceInfo) string {
	if name := strings.TrimSpace(peer.Name); name != "" {
		return name
	}
	if model := strings.TrimSpace(peer.ModelName); model != "" {
		return model
	}
	return "unnamed"
}

func peerOSName(peer servicedcg.DeviceInfo) string {
	if osName := strings.TrimSpace(peer.OSName); osName != "" {
		return osName
	}
	return "unknown OS"
}
