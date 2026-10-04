package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/auth/msa"
	authstate "github.com/YMGPwcca/linkmyphone/auth/state"
	"github.com/YMGPwcca/linkmyphone/bootstrap"
	clipclient "github.com/YMGPwcca/linkmyphone/clipboard"
	"github.com/YMGPwcca/linkmyphone/dcgheaders"
	servicedcg "github.com/YMGPwcca/linkmyphone/services/dcg"
)

const (
	defaultAppVersion = "1.26072.116.0"
	defaultRingName   = "Public"
	defaultOSVersion  = "10.0.26100"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == clipclient.NativeWatchHelperCommand {
		if err := clipclient.RunNativeWatchHelper(
			os.Stdin,
			os.Stdout,
			os.Getenv("CLIPBOARD_STATE"),
		); err != nil {
			fmt.Fprintf(os.Stderr, "clipboard watch helper: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "bootstrap-probe":
		err = runBootstrapProbe(ctx, os.Args[2:])
	case "peer-probe":
		err = runPeerProbe(ctx, os.Args[2:])
	case "session-probe":
		err = runSessionProbe(ctx, os.Args[2:])
	case "feature":
		err = runFeatureCommand(os.Args[2:])
	case "run":
		err = runRuntime(ctx, os.Args[2:])
	case "service":
		err = runServiceCommand(os.Args[2:])
	case "clipboard-sync":
		err = runClipboardSync(ctx, os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[FAIL] %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "linkmyphone")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  linkmyphone bootstrap-probe [options]")
	fmt.Fprintln(os.Stderr, "  linkmyphone peer-probe [options]")
	fmt.Fprintln(os.Stderr, "  linkmyphone session-probe [options]")
	fmt.Fprintln(os.Stderr, "  linkmyphone feature <subcommand> [options]")
	fmt.Fprintln(os.Stderr, "  linkmyphone run [options]")
	fmt.Fprintln(os.Stderr, "  linkmyphone service <subcommand> [options]")
	fmt.Fprintln(os.Stderr, "  linkmyphone clipboard-sync [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "bootstrap-probe validates Microsoft login, DCG enrollment/state,")
	fmt.Fprintln(os.Stderr, "linked-peer trust, and the account-level SignalR relay connection.")
	fmt.Fprintln(os.Stderr, "peer-probe reuses persisted state, selects the linked Android peer,")
	fmt.Fprintln(os.Stderr, "sends a signed Dispatcher/Wake when needed, and waits for Hub presence.")
	fmt.Fprintln(os.Stderr, "session-probe continues through PLATFORM /SessionValidation and reports")
	fmt.Fprintln(os.Stderr, "the peer capability/version response without dumping raw payloads.")
	fmt.Fprintln(os.Stderr, "feature manages the persistent modular feature registry (CRUD + enable/disable).")
	fmt.Fprintln(os.Stderr, "run maintains the LinkMyPhone host session and starts all enabled feature modules.")
	fmt.Fprintln(os.Stderr, "service installs and manages the systemd --user daemon.")
	fmt.Fprintln(os.Stderr, "clipboard-sync is a compatibility alias for the linkmyphone.clipboard module.")
}

type probeOptions struct {
	statePath   string
	profile     dcgheaders.Profile
	appVersion  string
	ringName    string
	osVersion   string
	displayName string
	timeout     time.Duration
}

func runBootstrapProbe(ctx context.Context, args []string) error {
	defaultStatePath, err := authstate.DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve state path: %w", err)
	}
	hostname, _ := os.Hostname()
	if strings.TrimSpace(hostname) == "" {
		hostname = "linkmyphone"
	}

	fs := flag.NewFlagSet("bootstrap-probe", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := probeOptions{}
	fs.StringVar(&opts.statePath, "state", defaultStatePath, "persistent bootstrap state path")
	var requestedProfile string
	fs.StringVar(&requestedProfile, "profile", "", "enrollment profile: crossdevice or phonelink; persisted profile is reused")
	fs.StringVar(&opts.appVersion, "app-version", defaultAppVersion, "compatibility app version advertised to DCG")
	fs.StringVar(&opts.ringName, "ring", defaultRingName, "compatibility ring name")
	fs.StringVar(&opts.osVersion, "os-version", defaultOSVersion, "Windows-compatible OS version advertised to DCG")
	fs.StringVar(&opts.displayName, "display-name", hostname, "device display name used for first enrollment")
	fs.DurationVar(&opts.timeout, "signalr-timeout", bootstrap.DefaultOnConnectedTimeout, "time to wait for SignalR OnConnected")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if opts.timeout <= 0 {
		return errors.New("signalr timeout must be positive")
	}
	opts.profile = dcgheaders.Profile(requestedProfile)
	if err := opts.profile.Validate(); err != nil {
		return err
	}
	stateSpecified := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "state" {
			stateSpecified = true
		}
	})

	fmt.Println("LinkMyPhone bootstrap probe")
	fmt.Printf("State: %s\n", opts.statePath)
	fmt.Println("Secrets are not printed.")
	fmt.Println()

	snapshot, loadErr := authstate.Load(opts.statePath)
	switch {
	case loadErr == nil:
		if requestedProfile != "" && opts.profile.Canonical() != snapshot.ClientProfile {
			return errors.New("existing enrollment uses a different client profile; choose a separate --state path")
		}
		opts.profile = snapshot.ClientProfile
		fmt.Println("[state] Existing enrollment found; reusing the same DCG identity.")
		return probeExistingState(ctx, opts, snapshot)
	case errors.Is(loadErr, os.ErrNotExist):
		opts.profile = opts.profile.Canonical()
		if opts.profile == dcgheaders.ProfilePhoneLink && !stateSpecified {
			return errors.New("phonelink enrollment requires an explicit isolated --state path")
		}
		fmt.Println("[state] No existing enrollment; starting first-run bootstrap.")
		return probeFirstRun(ctx, opts)
	default:
		return fmt.Errorf("load existing state %q: %w (refusing to create a second identity)", opts.statePath, loadErr)
	}
}

func probeFirstRun(ctx context.Context, opts probeOptions) error {
	msaClient := msa.NewDeviceCodeClient()
	msaClient.ClientID = opts.profile.MSAClientID()
	scope := msa.ScopeWithOfflineAccess(dcgauth.MigratedProdMSAScope)

	fmt.Println("[auth] Requesting Microsoft device code...")
	code, err := msaClient.Start(ctx, scope)
	if err != nil {
		return fmt.Errorf("Microsoft device-code request: %w", err)
	}
	fmt.Printf("[auth] Open: %s\n", code.VerificationURI)
	fmt.Printf("[auth] Code: %s\n", code.UserCode)
	fmt.Println("[auth] Waiting for sign-in...")

	msaToken, err := msaClient.Poll(ctx, code)
	if err != nil {
		return fmt.Errorf("Microsoft device-code sign-in: %w", err)
	}
	if msaToken.RefreshToken == "" {
		return errors.New("Microsoft login succeeded but returned no refresh token; refusing to enroll a persistent DCG identity")
	}
	fmt.Println("[auth] Microsoft login: OK")

	logicalDeviceID, err := authstate.NewLogicalDeviceID()
	if err != nil {
		return fmt.Errorf("generate logical device id: %w", err)
	}
	clientInfo, err := buildClientInfo(opts, logicalDeviceID)
	if err != nil {
		return err
	}

	authClient := dcgauth.NewClient(dcgauth.ProdServiceBase)
	serviceClient := servicedcg.NewClient(servicedcg.ProdServiceBase)
	configureDCGClients(authClient, serviceClient, clientInfo)

	fmt.Println("[1/4] Enrolling Linux as a DCG device...")
	enroller := &bootstrap.Enroller{
		Auth: authClient,
		DCG:  serviceClient,
	}
	enrollment, err := enroller.EnrollWithMSAToken(
		ctx,
		msaToken.AccessToken,
		servicedcg.MetadataForPC(opts.profile, opts.appVersion, opts.displayName, opts.osVersion),
	)
	if err != nil {
		return fmt.Errorf("stage 1 enrollment: %w", err)
	}
	if enrollment.EnrollResponse.AccountCert == "" {
		return errors.New("stage 1 enrollment returned no account certificate")
	}
	fmt.Printf("[1/4] Enrollment: OK (DCG %s)\n", shortID(enrollment.Identity.DeviceID))

	// Persist the newly-created private keys before any later network stage.
	snapshot, err := bootstrap.BuildStateSnapshot(
		msaToken,
		enrollment,
		bootstrap.TrustSyncResult{},
		logicalDeviceID,
		opts.profile,
	)
	if err != nil {
		return fmt.Errorf("stage 2 build state: %w", err)
	}
	if err := authstate.Save(opts.statePath, snapshot); err != nil {
		return fmt.Errorf("stage 2 save state: %w", err)
	}
	fmt.Printf("[2/4] Persistent state: OK (%s)\n", opts.statePath)

	fmt.Println("[3/4] Discovering linked devices and building trust...")
	trust, err := bootstrap.SyncTrust(
		ctx,
		serviceClient,
		msaToken.AccessToken,
		enrollment.Identity.DeviceID,
		enrollment.EnrollResponse.AccountCert,
		"",
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("stage 3 trust sync: %w", err)
	}
	snapshot.TrustRelationships = append([]dcgauth.TrustRelationship(nil), trust.Relationships()...)
	if err := authstate.Save(opts.statePath, snapshot); err != nil {
		return fmt.Errorf("stage 3 persist trust: %w", err)
	}
	printLinkedPeers(trust.Devices, enrollment.Identity.DeviceID)
	fmt.Printf("[3/4] Trust sync: OK (%d relationship(s))\n", len(snapshot.TrustRelationships))

	fmt.Println("[4/4] Connecting account-level SignalR relay...")
	cloud, err := bootstrap.OpenCloudRelay(ctx, bootstrap.CloudConfig{
		Services:           serviceClient,
		MSAAccessToken:     msaToken.AccessToken,
		DCGAccessToken:     snapshot.ServicesToken.Token,
		ClientInfo:         clientInfo,
		OnConnectedTimeout: opts.timeout,
	})
	if err != nil {
		return fmt.Errorf("stage 4 SignalR: %w", err)
	}
	defer cloud.Close()
	printCloudResult(cloud)

	fmt.Println()
	fmt.Println("[OK] Bootstrap stages 1-4 reached Hub Relay OnConnected.")
	return nil
}

func probeExistingState(ctx context.Context, opts probeOptions, snapshot authstate.Snapshot) error {
	opts.profile = snapshot.ClientProfile
	clientInfo, err := buildClientInfo(opts, snapshot.LogicalDeviceID)
	if err != nil {
		return err
	}

	msaClient := msa.NewDeviceCodeClient()
	authClient := dcgauth.NewClient(dcgauth.ProdServiceBase)
	serviceClient := servicedcg.NewClient(servicedcg.ProdServiceBase)
	configureDCGClients(authClient, serviceClient, clientInfo)

	fmt.Println("[1/4] Enrollment: existing state reused")
	fmt.Println("[2/4] Refreshing Microsoft and DCG tokens with persisted identity...")
	resumed, err := bootstrap.ResumeAuthAndSave(ctx, msaClient, authClient, snapshot, opts.statePath)
	if err != nil {
		return fmt.Errorf("stage 2 resume authentication: %w", err)
	}
	snapshot = resumed.State
	fmt.Printf("[2/4] Resume: OK (DCG %s)\n", shortID(resumed.Identity.DeviceID))

	fmt.Println("[3/4] Refreshing linked-device trust...")
	if snapshot.Enrollment.AccountCert == "" {
		return errors.New("stage 3 persisted enrollment has no account certificate")
	}
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
		return fmt.Errorf("stage 3 trust sync: %w", err)
	}
	snapshot.TrustRelationships = append([]dcgauth.TrustRelationship(nil), trust.Relationships()...)
	if err := authstate.Save(opts.statePath, snapshot); err != nil {
		return fmt.Errorf("stage 3 persist trust: %w", err)
	}
	printLinkedPeers(trust.Devices, resumed.Identity.DeviceID)
	fmt.Printf("[3/4] Trust sync: OK (%d relationship(s))\n", len(snapshot.TrustRelationships))

	fmt.Println("[4/4] Connecting account-level SignalR relay...")
	cloud, err := bootstrap.OpenCloudRelay(ctx, bootstrap.CloudConfig{
		Services:           serviceClient,
		MSAAccessToken:     resumed.MSAToken.AccessToken,
		DCGAccessToken:     resumed.ServicesToken.Token,
		ClientInfo:         clientInfo,
		OnConnectedTimeout: opts.timeout,
	})
	if err != nil {
		return fmt.Errorf("stage 4 SignalR: %w", err)
	}
	defer cloud.Close()
	printCloudResult(cloud)

	fmt.Println()
	fmt.Println("[OK] Existing identity resumed and Hub Relay reached OnConnected.")
	return nil
}

func configureDCGClients(authClient *dcgauth.Client, serviceClient *servicedcg.Client, info dcgheaders.ClientInfo) {
	authClient.AuthorizationPortal = dcgheaders.PortalFirstParty
	authClient.ClientInfo = info
	serviceClient.AuthorizationPortal = dcgheaders.PortalFirstParty
	serviceClient.ClientInfo = info
}

func buildClientInfo(opts probeOptions, logicalDeviceID string) (dcgheaders.ClientInfo, error) {
	info, err := dcgheaders.NewClientInfo(
		opts.profile,
		logicalDeviceID,
		opts.appVersion,
		opts.ringName,
		opts.osVersion,
	)
	if err != nil {
		return dcgheaders.ClientInfo{}, fmt.Errorf("build client profile headers: %w", err)
	}
	return info, nil
}

func printLinkedPeers(devices []servicedcg.DeviceInfo, selfID string) {
	peers := servicedcg.LinkedPeers(devices, selfID)
	if len(peers) == 0 {
		fmt.Println("[3/4] Linked peers: none returned by DeviceInfoList")
		return
	}
	for _, peer := range peers {
		name := strings.TrimSpace(peer.Name)
		if name == "" {
			name = strings.TrimSpace(peer.ModelName)
		}
		if name == "" {
			name = "unnamed"
		}
		osName := strings.TrimSpace(peer.OSName)
		if osName == "" {
			osName = "unknown OS"
		}
		fmt.Printf("[3/4] Linked peer: %s / %s / DCG %s\n", name, osName, shortID(peer.ID))
	}
}

func printCloudResult(cloud *bootstrap.CloudRelay) {
	fmt.Printf("[4/4] SignalR shard: %s\n", cloud.Region)
	fmt.Println("[4/4] Hub OnConnected: OK")
	if len(cloud.Partners) == 0 {
		fmt.Println("[4/4] Online partners: none in initial OnConnected payload")
		return
	}
	for _, partner := range cloud.Partners {
		fmt.Printf("[4/4] Online partner: DCG %s\n", shortID(partner))
	}
}

func shortID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:8] + "..." + value[len(value)-4:]
}
