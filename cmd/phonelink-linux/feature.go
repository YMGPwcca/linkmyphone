package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/YMGPwcca/phonelink-linux/features"
	"github.com/YMGPwcca/phonelink-linux/runtime/kernel"
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
	fmt.Fprintln(os.Stderr, "  phonelink-linux feature list [--state PATH]")
	fmt.Fprintln(os.Stderr, "  phonelink-linux feature get [--state PATH] ID")
	fmt.Fprintln(os.Stderr, "  phonelink-linux feature create [--state PATH] [--enabled] [--config JSON] ID")
	fmt.Fprintln(os.Stderr, "  phonelink-linux feature update [--state PATH] [--enabled true|false] [--config JSON] ID")
	fmt.Fprintln(os.Stderr, "  phonelink-linux feature delete [--state PATH] ID")
	fmt.Fprintln(os.Stderr, "  phonelink-linux feature enable [--state PATH] ID")
	fmt.Fprintln(os.Stderr, "  phonelink-linux feature disable [--state PATH] ID")
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

	store, err := kernel.OpenFeatureStore(*statePath)
	if err != nil {
		return err
	}
	if err := store.Create(kernel.FeatureRecord{
		ID:      id,
		Enabled: enabled,
		Config:  config,
	}); err != nil {
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
