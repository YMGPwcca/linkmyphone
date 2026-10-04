package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	authstate "github.com/YMGPwcca/linkmyphone/auth/state"
	"github.com/YMGPwcca/linkmyphone/bootstrap"
	"github.com/YMGPwcca/linkmyphone/features"
	clipboardfeature "github.com/YMGPwcca/linkmyphone/features/clipboard"
	"github.com/YMGPwcca/linkmyphone/runtime/controlplane"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
	"github.com/YMGPwcca/linkmyphone/runtime/phonehost"
	"github.com/YMGPwcca/linkmyphone/runtime/systemdnotify"
)

type runtimeHostOptions struct {
	statePath          string
	appVersion         string
	ringName           string
	osVersion          string
	target             string
	signalrTimeout     time.Duration
	wakeTimeout        time.Duration
	wakeTTL            time.Duration
	requestTimeout     time.Duration
	reconnectMinDelay  time.Duration
	reconnectMaxDelay  time.Duration
	sessionOpenTimeout time.Duration
	refreshMargin      time.Duration
}

func addRuntimeHostFlags(fs *flag.FlagSet, opts *runtimeHostOptions) error {
	defaultStatePath, err := authstate.DefaultPath()
	if err != nil {
		return err
	}
	fs.StringVar(&opts.statePath, "state", defaultStatePath, "persistent bootstrap state path")
	fs.StringVar(&opts.appVersion, "app-version", defaultAppVersion, "compatibility app version advertised to DCG")
	fs.StringVar(&opts.ringName, "ring", defaultRingName, "compatibility ring name")
	fs.StringVar(&opts.osVersion, "os-version", defaultOSVersion, "Windows-compatible OS version advertised to DCG")
	fs.StringVar(&opts.target, "target", "", "target linked peer id/name; defaults to the sole linked Android device")
	fs.DurationVar(&opts.signalrTimeout, "signalr-timeout", bootstrap.DefaultOnConnectedTimeout, "time to wait for SignalR OnConnected")
	fs.DurationVar(&opts.wakeTimeout, "wake-timeout", bootstrap.DefaultPeerWakeTimeout, "time to wait for target peer presence after wake")
	fs.DurationVar(&opts.wakeTTL, "wake-ttl", bootstrap.DefaultPeerWakeTTL, "Dispatcher wake time-to-live")
	fs.DurationVar(&opts.requestTimeout, "request-timeout", 10*time.Second, "time to wait for PLATFORM feature requests")
	fs.DurationVar(&opts.reconnectMinDelay, "reconnect-min-delay", phonehost.DefaultReconnectMinDelay, "initial session recovery backoff (with jitter)")
	fs.DurationVar(&opts.reconnectMaxDelay, "reconnect-max-delay", phonehost.DefaultReconnectMaxDelay, "maximum session recovery backoff; Retry-After can exceed it")
	fs.DurationVar(&opts.sessionOpenTimeout, "session-open-timeout", phonehost.DefaultSessionOpenTimeout, "deadline for one complete authentication/trust/relay/wake attempt")
	fs.DurationVar(&opts.refreshMargin, "refresh-margin", phonehost.DefaultRefreshMargin, "refresh before earliest token expiry; capped at 20% of token lifetime")
	return nil
}

func (o runtimeHostOptions) config() phonehost.Config {
	return phonehost.Config{
		StatePath:          o.statePath,
		AppVersion:         o.appVersion,
		RingName:           o.ringName,
		OSVersion:          o.osVersion,
		Target:             o.target,
		SignalRTimeout:     o.signalrTimeout,
		WakeTimeout:        o.wakeTimeout,
		WakeTTL:            o.wakeTTL,
		RequestTimeout:     o.requestTimeout,
		ReconnectMinDelay:  o.reconnectMinDelay,
		ReconnectMaxDelay:  o.reconnectMaxDelay,
		SessionOpenTimeout: o.sessionOpenTimeout,
		RefreshMargin:      o.refreshMargin,
	}
}

func (o runtimeHostOptions) validate() error {
	if o.signalrTimeout <= 0 ||
		o.wakeTimeout <= 0 ||
		o.wakeTTL <= 0 ||
		o.requestTimeout <= 0 {
		return errors.New("all timeout/TTL values must be positive")
	}
	if o.reconnectMinDelay <= 0 || o.reconnectMaxDelay < o.reconnectMinDelay || o.sessionOpenTimeout <= 0 || o.refreshMargin <= 0 {
		return errors.New("recovery durations must be positive; reconnect-max-delay must be >= reconnect-min-delay")
	}
	return nil
}

func runRuntime(ctx context.Context, args []string) error {
	defaultFeaturePath, err := kernel.DefaultFeatureStorePath()
	if err != nil {
		return fmt.Errorf("resolve feature store path: %w", err)
	}

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var hostOpts runtimeHostOptions
	if err := addRuntimeHostFlags(fs, &hostOpts); err != nil {
		return err
	}
	featurePath := defaultFeaturePath
	fs.StringVar(&featurePath, "features-state", defaultFeaturePath, "persistent feature registry path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if err := hostOpts.validate(); err != nil {
		return err
	}

	store, err := kernel.OpenFeatureStore(featurePath)
	if err != nil {
		return err
	}
	records := store.List()
	enabled := 0
	for _, record := range records {
		if record.Enabled {
			enabled++
		}
	}

	fmt.Println("LinkMyPhone modular runtime")
	fmt.Printf("State: %s\n", hostOpts.statePath)
	fmt.Printf("Features: %s\n", featurePath)
	fmt.Printf("Enabled modules: %d\n", enabled)
	fmt.Println()

	return runManagedFeatureRuntime(ctx, hostOpts.config(), store)
}

func runClipboardSync(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("clipboard-sync", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var hostOpts runtimeHostOptions
	if err := addRuntimeHostFlags(fs, &hostOpts); err != nil {
		return err
	}

	cfg := clipboardfeature.DefaultConfig()
	pollInterval := cfg.PollInterval()
	fs.DurationVar(&pollInterval, "poll-interval", pollInterval, "fallback clipboard polling interval when native watching is unavailable")
	fs.BoolVar(&cfg.PublishInitial, "publish-initial", false, "publish the current Linux clipboard immediately after startup")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if err := hostOpts.validate(); err != nil {
		return err
	}
	if pollInterval < 50*time.Millisecond || pollInterval > 60*time.Second {
		return errors.New("poll interval must be between 50ms and 60s")
	}
	cfg.PollIntervalMS = int(pollInterval / time.Millisecond)
	cfg.RequestTimeoutMS = int(hostOpts.requestTimeout / time.Millisecond)
	if err := cfg.Validate(); err != nil {
		return err
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	record := kernel.FeatureRecord{
		ID:      "linkmyphone.clipboard",
		Enabled: true,
		Config:  raw,
	}

	fmt.Println("LinkMyPhone clipboard sync")
	fmt.Printf("State: %s\n", hostOpts.statePath)
	fmt.Println("Compatibility alias for modular feature linkmyphone.clipboard.")
	fmt.Println("Clipboard contents are not printed.")
	fmt.Println()

	return runFeatureRuntime(ctx, hostOpts.config(), []kernel.FeatureRecord{record})
}

func runManagedFeatureRuntime(ctx context.Context, hostConfig phonehost.Config, store *kernel.FeatureStore) (runErr error) {
	reporter := consoleReporter{}
	socketPath := controlplane.SocketPathForStore(store.Path())
	transientHandler := func(message string) controlplane.Handler {
		return controlplane.HandlerFunc(func(controlplane.Request) controlplane.Response {
			return controlplane.Failure(errors.New(message))
		})
	}
	switchHandler := controlplane.NewSwitchHandler(transientHandler("runtime is starting; retry the feature command"))
	server, err := controlplane.Listen(socketPath, switchHandler)
	if err != nil {
		return err
	}
	managedCtx, cancelManaged := context.WithCancel(ctx)
	var stoppingOnce sync.Once
	announceStopping := func() {
		stoppingOnce.Do(func() {
			if err := systemdnotify.Stopping("LinkMyPhone runtime stopping"); err != nil {
				kernel.Report(reporter, kernel.Event{ModuleID: "runtime.systemd", Level: "warning",
					Message: "systemd stopping notification failed", Fields: map[string]string{"error": err.Error()}})
			}
		})
	}
	controlErr := make(chan error, 1)
	go func() {
		controlErr <- server.Serve(managedCtx)
		cancelManaged()
	}()
	defer func() {
		announceStopping()
		cancelManaged()
		switchHandler.Set(transientHandler("runtime is stopping"))
		if err := server.Close(); err != nil && runErr == nil {
			runErr = err
		}
	}()

	runErr = phonehost.Supervise(managedCtx, hostConfig, reporter, func(generationCtx context.Context, session *phonehost.Session) (generationErr error) {
		controlCtx, cancelControl := context.WithCancel(generationCtx)
		registry := kernel.NewRegistry(reporter)
		controller := newRuntimeController(controlCtx, store, registry, session, reporter)
		defer func() {
			if generationCtx.Err() != nil {
				announceStopping()
			}
			// Cancel active mutations before swapping the handler. Set waits for
			// old handlers, so no operation can touch a revoked generation.
			cancelControl()
			switchHandler.Set(transientHandler("runtime is recovering; retry the feature command"))
			if generationErr != nil {
				_ = session.Close()
			}
			if err := stopFeatureGeneration(registry); err != nil {
				generationErr = err
			}
		}()
		if err := controller.load(controlCtx); err != nil {
			return startupFailure(session, err)
		}
		switchHandler.Set(controller)
		if err := systemdnotify.Ready("LinkMyPhone runtime ready"); err != nil {
			return fmt.Errorf("notify systemd readiness: %w", err)
		}
		printRuntimeState(registry)
		fmt.Printf("[runtime] Control socket: %s\n", socketPath)
		fmt.Println("[OK] Modular runtime is running. Feature CRUD is live. Press Ctrl+C to stop.")
		return waitFeatureGeneration(controlCtx, session, registry, reporter)
	})
	if ctx.Err() == nil && managedCtx.Err() != nil {
		err := <-controlErr
		if err == nil {
			return errors.New("controlplane: server exited")
		}
		return err
	}
	return runErr
}

func stopFeatureGeneration(registry *kernel.Registry) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := registry.StopAll(stopCtx); err != nil {
		return fmt.Errorf("runtime generation teardown: %w", err)
	}
	return nil
}

func startupFailure(session *phonehost.Session, err error) error {
	select {
	case hostErr := <-session.Errors():
		if hostErr != nil {
			return hostErr
		}
	default:
	}
	return err
}

func waitFeatureGeneration(ctx context.Context, session *phonehost.Session, registry *kernel.Registry, reporter kernel.Reporter) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-session.Errors():
			if err == nil {
				return errors.New("phonehost: session monitor exited")
			}
			return fmt.Errorf("LinkMyPhone host: %w", err)
		case runtimeErr := <-registry.Errors():
			kernel.Report(reporter, kernel.Event{
				ModuleID: runtimeErr.ModuleID, Level: "error",
				Message: "module failed; shared runtime remains online",
				Fields:  map[string]string{"error": runtimeErr.Err.Error()},
			})
		}
	}
}

func printRuntimeState(registry *kernel.Registry) {
	ready := registry.List()
	fmt.Println()
	fmt.Println("[runtime] Modules:")
	if len(ready) == 0 {
		fmt.Println("  (none installed)")
	}
	for _, snapshot := range ready {
		fmt.Printf(
			"  %s %s enabled=%t state=%s epoch=%d\n",
			snapshot.ID,
			snapshot.Version,
			snapshot.Enabled,
			snapshot.State,
			snapshot.Epoch,
		)
	}
	capabilities := registry.Capabilities().Snapshot()
	fmt.Println("[runtime] Live capabilities:")
	if len(capabilities) == 0 {
		fmt.Println("  (none)")
	}
	for _, capability := range capabilities {
		fmt.Printf(
			"  %s@%s <- %s\n",
			capability.ID,
			capability.ContractVersion,
			capability.ProviderID,
		)
	}
}

func runFeatureRuntime(ctx context.Context, hostConfig phonehost.Config, records []kernel.FeatureRecord) error {
	reporter := consoleReporter{}
	type preparedFeature struct {
		record     kernel.FeatureRecord
		definition features.Definition
	}
	prepared := make([]preparedFeature, 0, len(records))
	for _, record := range records {
		if !record.Enabled {
			continue
		}
		definition, err := features.Find(record.ID)
		if err != nil {
			return fmt.Errorf("enabled feature %s is unavailable: %w", record.ID, err)
		}
		if err := definition.Validate(record.Config); err != nil {
			return fmt.Errorf("validate feature %s: %w", record.ID, err)
		}
		prepared = append(prepared, preparedFeature{record: record, definition: definition})
	}
	return phonehost.Supervise(ctx, hostConfig, reporter, func(generationCtx context.Context, session *phonehost.Session) (generationErr error) {
		registry := kernel.NewRegistry(reporter)
		defer func() {
			if generationErr != nil {
				_ = session.Close()
			}
			if err := stopFeatureGeneration(registry); err != nil {
				generationErr = err
			}
		}()
		for _, item := range prepared {
			module, err := item.definition.Build(session)
			if err != nil {
				return fmt.Errorf("build feature %s: %w", item.record.ID, err)
			}
			if err := registry.Create(module, item.record); err != nil {
				return err
			}
		}
		if err := registry.StartEnabled(generationCtx); err != nil {
			return startupFailure(session, err)
		}
		printRuntimeState(registry)
		fmt.Println("[OK] Modular runtime is running. Press Ctrl+C to stop.")
		return waitFeatureGeneration(generationCtx, session, registry, reporter)
	})
}

type consoleReporter struct{}

func (consoleReporter) Report(event kernel.Event) {
	prefix := "[runtime]"
	if event.ModuleID != "" {
		prefix = "[" + event.ModuleID + "]"
	}
	fields := make([]string, 0, len(event.Fields))
	for key, value := range event.Fields {
		fields = append(fields, key+"="+value)
	}
	sort.Strings(fields)
	if len(fields) == 0 {
		fmt.Printf("%s %s\n", prefix, event.Message)
		return
	}
	fmt.Printf("%s %s (%s)\n", prefix, event.Message, strings.Join(fields, ", "))
}
