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
	"time"

	authstate "github.com/YMGPwcca/phonelink-linux/auth/state"
	"github.com/YMGPwcca/phonelink-linux/bootstrap"
	"github.com/YMGPwcca/phonelink-linux/features"
	clipboardfeature "github.com/YMGPwcca/phonelink-linux/features/clipboard"
	"github.com/YMGPwcca/phonelink-linux/runtime/controlplane"
	"github.com/YMGPwcca/phonelink-linux/runtime/kernel"
	"github.com/YMGPwcca/phonelink-linux/runtime/phonehost"
)

type runtimeHostOptions struct {
	statePath      string
	appVersion     string
	ringName       string
	osVersion      string
	target         string
	signalrTimeout time.Duration
	wakeTimeout    time.Duration
	wakeTTL        time.Duration
	requestTimeout time.Duration
}

func addRuntimeHostFlags(fs *flag.FlagSet, opts *runtimeHostOptions) error {
	defaultStatePath, err := authstate.DefaultPath()
	if err != nil {
		return err
	}
	fs.StringVar(&opts.statePath, "state", defaultStatePath, "persistent bootstrap state path")
	fs.StringVar(&opts.appVersion, "app-version", defaultAppVersion, "CrossDevice app version advertised to DCG")
	fs.StringVar(&opts.ringName, "ring", defaultRingName, "CrossDevice ring name")
	fs.StringVar(&opts.osVersion, "os-version", defaultOSVersion, "Windows-compatible OS version advertised to DCG")
	fs.StringVar(&opts.target, "target", "", "target linked peer id/name; defaults to the sole linked Android device")
	fs.DurationVar(&opts.signalrTimeout, "signalr-timeout", bootstrap.DefaultOnConnectedTimeout, "time to wait for SignalR OnConnected")
	fs.DurationVar(&opts.wakeTimeout, "wake-timeout", bootstrap.DefaultPeerWakeTimeout, "time to wait for target peer presence after wake")
	fs.DurationVar(&opts.wakeTTL, "wake-ttl", bootstrap.DefaultPeerWakeTTL, "Dispatcher wake time-to-live")
	fs.DurationVar(&opts.requestTimeout, "request-timeout", 10*time.Second, "time to wait for PLATFORM feature requests")
	return nil
}

func (o runtimeHostOptions) config() phonehost.Config {
	return phonehost.Config{
		StatePath:      o.statePath,
		AppVersion:     o.appVersion,
		RingName:       o.ringName,
		OSVersion:      o.osVersion,
		Target:         o.target,
		SignalRTimeout: o.signalrTimeout,
		WakeTimeout:    o.wakeTimeout,
		WakeTTL:        o.wakeTTL,
		RequestTimeout: o.requestTimeout,
	}
}

func (o runtimeHostOptions) validate() error {
	if o.signalrTimeout <= 0 ||
		o.wakeTimeout <= 0 ||
		o.wakeTTL <= 0 ||
		o.requestTimeout <= 0 {
		return errors.New("all timeout/TTL values must be positive")
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

	fmt.Println("Phone Link Linux modular runtime")
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
		ID:      "phonelink.clipboard",
		Enabled: true,
		Config:  raw,
	}

	fmt.Println("Phone Link Linux clipboard sync")
	fmt.Printf("State: %s\n", hostOpts.statePath)
	fmt.Println("Compatibility alias for modular feature phonelink.clipboard.")
	fmt.Println("Clipboard contents are not printed.")
	fmt.Println()

	return runFeatureRuntime(ctx, hostOpts.config(), []kernel.FeatureRecord{record})
}

func runManagedFeatureRuntime(
	ctx context.Context,
	hostConfig phonehost.Config,
	store *kernel.FeatureStore,
) (runErr error) {
	reporter := consoleReporter{}
	socketPath := controlplane.SocketPathForStore(store.Path())
	switchHandler := controlplane.NewSwitchHandler(controlplane.HandlerFunc(
		func(controlplane.Request) controlplane.Response {
			return controlplane.Failure(errors.New("runtime is starting; retry the feature command"))
		},
	))
	server, err := controlplane.Listen(socketPath, switchHandler)
	if err != nil {
		return err
	}
	defer server.Close()

	controlErr := make(chan error, 1)
	go func() {
		controlErr <- server.Serve(ctx)
	}()

	session, err := phonehost.Open(ctx, hostConfig, reporter)
	if err != nil {
		return err
	}
	defer session.Close()

	registry := kernel.NewRegistry(reporter)
	controller := newRuntimeController(ctx, store, registry, session, reporter)

	// Register teardown before loading desired state so a partial startup
	// failure still revokes any modules that already reached Ready.
	defer func() {
		switchHandler.Set(controlplane.HandlerFunc(
			func(controlplane.Request) controlplane.Response {
				return controlplane.Failure(errors.New("runtime is stopping"))
			},
		))
		// Close the control plane first and wait for in-flight handlers. Their
		// operation contexts inherit ctx, so runtime cancellation aborts any
		// network/lifecycle wait before module teardown begins.
		if err := server.Close(); err != nil && runErr == nil {
			runErr = err
		}
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := registry.StopAll(stopCtx); err != nil && runErr == nil {
			runErr = err
		}
	}()

	if err := controller.load(ctx); err != nil {
		return err
	}
	switchHandler.Set(controller)

	printRuntimeState(registry)
	fmt.Printf("[runtime] Control socket: %s\n", socketPath)
	fmt.Println()
	fmt.Println("[OK] Modular runtime is running. Feature CRUD is live. Press Ctrl+C to stop.")

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-session.Errors():
			if err == nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("Phone Link host: %w", err)
		case err := <-controlErr:
			if err == nil || errors.Is(err, context.Canceled) {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("controlplane: server exited")
			}
			return err
		case runtimeErr := <-registry.Errors():
			kernel.Report(reporter, kernel.Event{
				ModuleID: runtimeErr.ModuleID,
				Level:    "error",
				Message:  "module failed; shared runtime remains online",
				Fields: map[string]string{
					"error": runtimeErr.Err.Error(),
				},
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

func runFeatureRuntime(
	ctx context.Context,
	hostConfig phonehost.Config,
	records []kernel.FeatureRecord,
) (runErr error) {
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
		prepared = append(prepared, preparedFeature{
			record:     record,
			definition: definition,
		})
	}

	session, err := phonehost.Open(ctx, hostConfig, reporter)
	if err != nil {
		return err
	}
	defer session.Close()

	registry := kernel.NewRegistry(reporter)
	for _, item := range prepared {
		module, err := item.definition.Build(session)
		if err != nil {
			return fmt.Errorf("build feature %s: %w", item.record.ID, err)
		}
		if err := registry.Create(module, item.record); err != nil {
			return err
		}
	}

	// Register cleanup before starting any module so a partial StartEnabled
	// failure still stops the subset that already reached Ready.
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := registry.StopAll(stopCtx); err != nil && runErr == nil {
			runErr = err
		}
	}()
	if err := registry.StartEnabled(ctx); err != nil {
		return err
	}

	ready := registry.List()
	fmt.Println()
	fmt.Println("[runtime] Modules:")
	for _, snapshot := range ready {
		fmt.Printf(
			"  %s %s enabled=%t state=%s\n",
			snapshot.ID,
			snapshot.Version,
			snapshot.Enabled,
			snapshot.State,
		)
	}
	capabilities := registry.Capabilities().Snapshot()
	if len(capabilities) != 0 {
		fmt.Println("[runtime] Live capabilities:")
		for _, capability := range capabilities {
			fmt.Printf(
				"  %s@%s <- %s\n",
				capability.ID,
				capability.ContractVersion,
				capability.ProviderID,
			)
		}
	}
	fmt.Println()
	fmt.Println("[OK] Modular runtime is running. Press Ctrl+C to stop.")

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-session.Errors():
			if err == nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("Phone Link host: %w", err)
		case runtimeErr := <-registry.Errors():
			kernel.Report(reporter, kernel.Event{
				ModuleID: runtimeErr.ModuleID,
				Level:    "error",
				Message:  "module failed; shared runtime remains online",
				Fields: map[string]string{
					"error": runtimeErr.Err.Error(),
				},
			})
		}
	}
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
