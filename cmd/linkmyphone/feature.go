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

	"github.com/YMGPwcca/linkmyphone/features"
	"github.com/YMGPwcca/linkmyphone/runtime/controlplane"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
)

func runFeatureCommand(args []string) error {
	if len(args) == 0 {
		featureUsage()
		return errors.New("feature subcommand is required")
	}
	switch args[0] {
	case "list":
		return runFeatureList(args[1:])
	case "get":
		return runFeatureGet(args[1:])
	case "create":
		return runFeatureCreate(args[1:])
	case "update":
		return runFeatureUpdate(args[1:])
	case "delete":
		return runFeatureDelete(args[1:])
	case "enable":
		return runFeatureToggle(args[1:], true)
	case "disable":
		return runFeatureToggle(args[1:], false)
	case "help", "-h", "--help":
		featureUsage()
		return nil
	default:
		return fmt.Errorf("unknown feature subcommand %q", args[0])
	}
}

func featureUsage() {
	fmt.Fprintln(os.Stderr, "Feature management:")
	fmt.Fprintln(os.Stderr, "  linkmyphone feature list [--state PATH]")
	fmt.Fprintln(os.Stderr, "  linkmyphone feature get [--state PATH] ID")
	fmt.Fprintln(os.Stderr, "  linkmyphone feature create [--state PATH] [--enabled] [--config JSON] ID")
	fmt.Fprintln(os.Stderr, "  linkmyphone feature update [--state PATH] [--enabled true|false] [--config JSON] ID")
	fmt.Fprintln(os.Stderr, "  linkmyphone feature delete [--state PATH] ID")
	fmt.Fprintln(os.Stderr, "  linkmyphone feature enable [--state PATH] ID")
	fmt.Fprintln(os.Stderr, "  linkmyphone feature disable [--state PATH] ID")
}

func featureStoreFlag(fs *flag.FlagSet) (*string, error) {
	path, err := kernel.DefaultFeatureStorePath()
	if err != nil {
		return nil, err
	}
	value := path
	fs.StringVar(&value, "state", path, "persistent feature registry path")
	return &value, nil
}

func callLiveRuntime(
	statePath string,
	request controlplane.Request,
) (controlplane.Response, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), controlplane.DefaultCallTimeout)
	defer cancel()
	response, err := controlplane.Call(
		ctx,
		controlplane.SocketPathForStore(statePath),
		request,
	)
	if errors.Is(err, controlplane.ErrUnavailable) {
		return controlplane.Response{}, false, nil
	}
	if err != nil {
		return controlplane.Response{}, true, err
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "runtime rejected control request"
		}
		return response, true, errors.New(response.Error)
	}
	return response, true, nil
}

func snapshotMap(snapshots []kernel.Snapshot) map[string]kernel.Snapshot {
	out := make(map[string]kernel.Snapshot, len(snapshots))
	for _, snapshot := range snapshots {
		out[snapshot.ID] = snapshot
	}
	return out
}

func runFeatureList(args []string) error {
	fs := flag.NewFlagSet("feature list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	statePath, err := featureStoreFlag(fs)
	if err != nil {
		return err
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	liveResponse, live, err := callLiveRuntime(*statePath, controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationList,
	})
	if err != nil {
		return err
	}
	if live {
		installed := make(map[string]kernel.FeatureRecord)
		for _, record := range liveResponse.Records {
			installed[record.ID] = record
		}
		liveStates := snapshotMap(liveResponse.Snapshots)
		definitions, err := features.Catalog()
		if err != nil {
			return err
		}
		fmt.Printf("Feature registry: %s (live)\n", *statePath)
		for _, definition := range definitions {
			record, exists := installed[definition.Manifest.ID]
			snapshot, running := liveStates[definition.Manifest.ID]
			if running {
				fmt.Printf(
					"%s %s installed=%t enabled=%t state=%s epoch=%d\n",
					definition.Manifest.ID,
					definition.Manifest.Version,
					exists,
					exists && record.Enabled,
					snapshot.State,
					snapshot.Epoch,
				)
			} else {
				fmt.Printf(
					"%s %s installed=%t enabled=%t state=unavailable\n",
					definition.Manifest.ID,
					definition.Manifest.Version,
					exists,
					exists && record.Enabled,
				)
			}
			delete(installed, definition.Manifest.ID)
		}
		unknownIDs := make([]string, 0, len(installed))
		for id := range installed {
			unknownIDs = append(unknownIDs, id)
		}
		sort.Strings(unknownIDs)
		for _, id := range unknownIDs {
			record := installed[id]
			fmt.Printf("%s unknown installed=true enabled=%t state=unavailable\n", id, record.Enabled)
		}
		return nil
	}

	store, err := kernel.OpenFeatureStore(*statePath)
	if err != nil {
		return err
	}
	installed := make(map[string]kernel.FeatureRecord)
	for _, record := range store.List() {
		installed[record.ID] = record
	}
	definitions, err := features.Catalog()
	if err != nil {
		return err
	}

	fmt.Printf("Feature registry: %s\n", *statePath)
	for _, definition := range definitions {
		record, exists := installed[definition.Manifest.ID]
		fmt.Printf(
			"%s %s installed=%t enabled=%t\n",
			definition.Manifest.ID,
			definition.Manifest.Version,
			exists,
			exists && record.Enabled,
		)
		delete(installed, definition.Manifest.ID)
	}
	unknownIDs := make([]string, 0, len(installed))
	for id := range installed {
		unknownIDs = append(unknownIDs, id)
	}
	sort.Strings(unknownIDs)
	for _, id := range unknownIDs {
		record := installed[id]
		fmt.Printf("%s unknown installed=true enabled=%t\n", id, record.Enabled)
	}
	return nil
}

func runFeatureGet(args []string) error {
	fs := flag.NewFlagSet("feature get", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	statePath, err := featureStoreFlag(fs)
	if err != nil {
		return err
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("feature get requires exactly one ID")
	}
	id := fs.Arg(0)
	liveResponse, live, err := callLiveRuntime(*statePath, controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationGet,
		ID:        id,
	})
	if err != nil {
		return err
	}
	if live {
		definition, definitionErr := features.Find(id)
		if definitionErr != nil &&
			liveResponse.Record == nil &&
			liveResponse.Snapshot == nil {
			return fmt.Errorf("%w: %s", kernel.ErrFeatureNotFound, id)
		}
		payload := struct {
			Manifest  *kernel.Manifest      `json:"manifest,omitempty"`
			Installed bool                  `json:"installed"`
			Record    *kernel.FeatureRecord `json:"record,omitempty"`
			Runtime   *kernel.Snapshot      `json:"runtime,omitempty"`
		}{
			Installed: liveResponse.Record != nil,
			Record:    liveResponse.Record,
			Runtime:   liveResponse.Snapshot,
		}
		if definitionErr == nil {
			manifest := definition.Manifest
			payload.Manifest = &manifest
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	store, err := kernel.OpenFeatureStore(*statePath)
	if err != nil {
		return err
	}
	record, readErr := store.Read(id)
	definition, definitionErr := features.Find(id)
	if readErr != nil && definitionErr != nil {
		return readErr
	}

	payload := struct {
		Manifest  *kernel.Manifest      `json:"manifest,omitempty"`
		Installed bool                  `json:"installed"`
		Record    *kernel.FeatureRecord `json:"record,omitempty"`
	}{}
	if definitionErr == nil {
		manifest := definition.Manifest
		payload.Manifest = &manifest
	}
	if readErr == nil {
		payload.Installed = true
		payload.Record = &record
	} else if !errors.Is(readErr, kernel.ErrFeatureNotFound) {
		return readErr
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func runFeatureCreate(args []string) error {
	fs := flag.NewFlagSet("feature create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	statePath, err := featureStoreFlag(fs)
	if err != nil {
		return err
	}
	enabled := false
	configText := ""
	fs.BoolVar(&enabled, "enabled", false, "enable the feature immediately")
	fs.StringVar(&configText, "config", "", "feature configuration JSON object")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("feature create requires exactly one ID")
	}
	id := fs.Arg(0)
	definition, err := features.Find(id)
	if err != nil {
		return err
	}
	config := append(json.RawMessage(nil), definition.DefaultConfig...)
	if configText != "" {
		config = json.RawMessage(configText)
	}
	if err := definition.Validate(config); err != nil {
		return err
	}

	record := kernel.FeatureRecord{
		ID:      id,
		Enabled: enabled,
		Config:  config,
	}
	liveResponse, live, err := callLiveRuntime(*statePath, controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationCreate,
		Record:    &record,
	})
	if err != nil {
		return err
	}
	if live {
		state := kernel.StateValidated
		epoch := uint64(0)
		if liveResponse.Snapshot != nil {
			state = liveResponse.Snapshot.State
			epoch = liveResponse.Snapshot.Epoch
		}
		fmt.Printf("created %s enabled=%t live_state=%s epoch=%d\n", id, enabled, state, epoch)
		return nil
	}

	store, err := kernel.OpenFeatureStore(*statePath)
	if err != nil {
		return err
	}
	if err := store.Create(record); err != nil {
		return err
	}
	fmt.Printf("created %s enabled=%t\n", id, enabled)
	return nil
}

func runFeatureUpdate(args []string) error {
	fs := flag.NewFlagSet("feature update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	statePath, err := featureStoreFlag(fs)
	if err != nil {
		return err
	}
	enabledText := ""
	configText := ""
	fs.StringVar(&enabledText, "enabled", "", "set enabled state: true or false")
	fs.StringVar(&configText, "config", "", "replace feature configuration JSON object")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("feature update requires exactly one ID")
	}
	if enabledText == "" && configText == "" {
		return errors.New("feature update requires --enabled and/or --config")
	}
	id := fs.Arg(0)

	var enabled *bool
	if enabledText != "" {
		var value bool
		switch strings.ToLower(strings.TrimSpace(enabledText)) {
		case "true", "1", "yes", "on":
			value = true
		case "false", "0", "no", "off":
			value = false
		default:
			return fmt.Errorf("invalid --enabled value %q", enabledText)
		}
		enabled = &value
	}

	definition, definitionErr := features.Find(id)
	if enabled != nil && *enabled && definitionErr != nil {
		return fmt.Errorf("cannot enable unavailable feature %s: %w", id, definitionErr)
	}

	var config *json.RawMessage
	if configText != "" {
		if definitionErr != nil {
			return fmt.Errorf("cannot update config for unavailable feature %s: %w", id, definitionErr)
		}
		raw := json.RawMessage(configText)
		if err := definition.Validate(raw); err != nil {
			return err
		}
		config = &raw
	}

	liveResponse, live, err := callLiveRuntime(*statePath, controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationUpdate,
		ID:        id,
		Enabled:   enabled,
		Config:    config,
	})
	if err != nil {
		return err
	}
	if live {
		record := liveResponse.Record
		if record == nil {
			return errors.New("runtime returned no updated feature record")
		}
		if liveResponse.Snapshot != nil {
			fmt.Printf(
				"updated %s enabled=%t live_state=%s epoch=%d\n",
				record.ID,
				record.Enabled,
				liveResponse.Snapshot.State,
				liveResponse.Snapshot.Epoch,
			)
		} else {
			fmt.Printf("updated %s enabled=%t live_state=unavailable\n", record.ID, record.Enabled)
		}
		return nil
	}

	store, err := kernel.OpenFeatureStore(*statePath)
	if err != nil {
		return err
	}
	record, err := store.Update(id, enabled, config)
	if err != nil {
		return err
	}
	fmt.Printf("updated %s enabled=%t\n", record.ID, record.Enabled)
	return nil
}

func runFeatureDelete(args []string) error {
	fs := flag.NewFlagSet("feature delete", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	statePath, err := featureStoreFlag(fs)
	if err != nil {
		return err
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("feature delete requires exactly one ID")
	}
	id := fs.Arg(0)
	_, live, err := callLiveRuntime(*statePath, controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationDelete,
		ID:        id,
	})
	if err != nil {
		return err
	}
	if live {
		fmt.Printf("deleted %s live=true\n", id)
		return nil
	}

	store, err := kernel.OpenFeatureStore(*statePath)
	if err != nil {
		return err
	}
	if err := store.Delete(id); err != nil {
		return err
	}
	fmt.Printf("deleted %s\n", id)
	return nil
}

func runFeatureToggle(args []string, enabled bool) error {
	name := "feature disable"
	if enabled {
		name = "feature enable"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	statePath, err := featureStoreFlag(fs)
	if err != nil {
		return err
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%s requires exactly one ID", name)
	}
	id := fs.Arg(0)
	if enabled {
		if _, err := features.Find(id); err != nil {
			return fmt.Errorf("cannot enable unavailable feature %s: %w", id, err)
		}
	}
	liveResponse, live, err := callLiveRuntime(*statePath, controlplane.Request{
		Version:   controlplane.ProtocolVersion,
		Operation: controlplane.OperationUpdate,
		ID:        id,
		Enabled:   &enabled,
	})
	if err != nil {
		return err
	}
	if live {
		record := liveResponse.Record
		if record == nil {
			return errors.New("runtime returned no updated feature record")
		}
		if liveResponse.Snapshot != nil {
			fmt.Printf(
				"%s enabled=%t live_state=%s epoch=%d\n",
				record.ID,
				record.Enabled,
				liveResponse.Snapshot.State,
				liveResponse.Snapshot.Epoch,
			)
		} else {
			fmt.Printf("%s enabled=%t live_state=unavailable\n", record.ID, record.Enabled)
		}
		return nil
	}

	store, err := kernel.OpenFeatureStore(*statePath)
	if err != nil {
		return err
	}
	record, err := store.Update(id, &enabled, nil)
	if err != nil {
		return err
	}
	fmt.Printf("%s enabled=%t\n", record.ID, record.Enabled)
	return nil
}
