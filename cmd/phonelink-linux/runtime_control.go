package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/YMGPwcca/phonelink-linux/features"
	"github.com/YMGPwcca/phonelink-linux/runtime/controlplane"
	"github.com/YMGPwcca/phonelink-linux/runtime/kernel"
	"github.com/YMGPwcca/phonelink-linux/runtime/phonehost"
)

const liveMutationTimeout = 15 * time.Second

type featureResolver func(string) (features.Definition, error)

type runtimeController struct {
	mu       sync.Mutex
	ctx      context.Context
	store    *kernel.FeatureStore
	registry *kernel.Registry
	session  *phonehost.Session
	reporter kernel.Reporter
	resolve  featureResolver
}

func newRuntimeController(
	ctx context.Context,
	store *kernel.FeatureStore,
	registry *kernel.Registry,
	session *phonehost.Session,
	reporter kernel.Reporter,
) *runtimeController {
	if ctx == nil {
		ctx = context.Background()
	}
	return &runtimeController{
		ctx:      ctx,
		store:    store,
		registry: registry,
		session:  session,
		reporter: reporter,
		resolve:  features.Find,
	}
}

func (c *runtimeController) load(ctx context.Context) error {
	for _, record := range c.store.List() {
		definition, err := c.resolve(record.ID)
		if err != nil {
			if record.Enabled {
				return fmt.Errorf("enabled feature %s is unavailable: %w", record.ID, err)
			}
			continue
		}
		if err := definition.Validate(record.Config); err != nil {
			return fmt.Errorf("validate feature %s: %w", record.ID, err)
		}
		module, err := definition.Build(c.session)
		if err != nil {
			return fmt.Errorf("build feature %s: %w", record.ID, err)
		}
		if err := c.registry.Create(module, record); err != nil {
			return err
		}
	}
	return c.registry.StartEnabled(ctx)
}

func (c *runtimeController) HandleControl(request controlplane.Request) controlplane.Response {
	c.mu.Lock()
	defer c.mu.Unlock()

	ctx, cancel := context.WithTimeout(c.ctx, liveMutationTimeout)
	defer cancel()

	switch request.Operation {
	case controlplane.OperationList:
		response := controlplane.Success()
		response.Records = c.store.List()
		response.Snapshots = c.registry.List()
		return response

	case controlplane.OperationGet:
		return c.handleGet(request.ID)

	case controlplane.OperationCreate:
		if request.Record == nil {
			return controlplane.Failure(errors.New("runtime: create requires record"))
		}
		record, snapshot, err := c.create(ctx, *request.Record)
		if err != nil {
			return controlplane.Failure(err)
		}
		response := controlplane.Success()
		response.Record = &record
		response.Snapshot = &snapshot
		return response

	case controlplane.OperationUpdate:
		record, snapshot, err := c.update(ctx, request.ID, request.Enabled, request.Config)
		if err != nil {
			return controlplane.Failure(err)
		}
		response := controlplane.Success()
		response.Record = &record
		if snapshot != nil {
			response.Snapshot = snapshot
		}
		return response

	case controlplane.OperationDelete:
		if err := c.delete(ctx, request.ID); err != nil {
			return controlplane.Failure(err)
		}
		return controlplane.Success()

	default:
		return controlplane.Failure(fmt.Errorf(
			"runtime: unsupported control operation %q",
			request.Operation,
		))
	}
}

func (c *runtimeController) handleGet(id string) controlplane.Response {
	record, recordErr := c.store.Read(id)
	snapshot, snapshotErr := c.registry.Read(id)
	if recordErr != nil && !errors.Is(recordErr, kernel.ErrFeatureNotFound) {
		return controlplane.Failure(recordErr)
	}
	if snapshotErr != nil && !errors.Is(snapshotErr, kernel.ErrFeatureNotFound) {
		return controlplane.Failure(snapshotErr)
	}
	response := controlplane.Success()
	if recordErr == nil {
		response.Record = &record
	}
	if snapshotErr == nil {
		response.Snapshot = &snapshot
	}
	return response
}

func (c *runtimeController) create(
	ctx context.Context,
	record kernel.FeatureRecord,
) (kernel.FeatureRecord, kernel.Snapshot, error) {
	definition, err := c.resolve(record.ID)
	if err != nil {
		return kernel.FeatureRecord{}, kernel.Snapshot{}, err
	}
	if err := definition.Validate(record.Config); err != nil {
		return kernel.FeatureRecord{}, kernel.Snapshot{}, err
	}
	if _, err := c.store.Read(record.ID); err == nil {
		return kernel.FeatureRecord{}, kernel.Snapshot{}, fmt.Errorf(
			"%w: %s",
			kernel.ErrFeatureExists,
			record.ID,
		)
	} else if !errors.Is(err, kernel.ErrFeatureNotFound) {
		return kernel.FeatureRecord{}, kernel.Snapshot{}, err
	}

	module, err := definition.Build(c.session)
	if err != nil {
		return kernel.FeatureRecord{}, kernel.Snapshot{}, err
	}
	if err := c.registry.Create(module, record); err != nil {
		return kernel.FeatureRecord{}, kernel.Snapshot{}, err
	}

	rollbackRegistry := func() {
		rollbackCtx, cancel := context.WithTimeout(c.ctx, liveMutationTimeout)
		defer cancel()
		_ = c.registry.Delete(rollbackCtx, record.ID)
	}

	if record.Enabled {
		if err := c.registry.Start(ctx, record.ID); err != nil {
			rollbackRegistry()
			return kernel.FeatureRecord{}, kernel.Snapshot{}, err
		}
	}
	if err := c.store.Create(record); err != nil {
		rollbackRegistry()
		return kernel.FeatureRecord{}, kernel.Snapshot{}, err
	}

	persisted, err := c.store.Read(record.ID)
	if err != nil {
		return kernel.FeatureRecord{}, kernel.Snapshot{}, err
	}
	snapshot, err := c.registry.Read(record.ID)
	if err != nil {
		return kernel.FeatureRecord{}, kernel.Snapshot{}, err
	}
	return persisted, snapshot, nil
}

func (c *runtimeController) update(
	ctx context.Context,
	id string,
	enabled *bool,
	config *json.RawMessage,
) (kernel.FeatureRecord, *kernel.Snapshot, error) {
	if id == "" {
		return kernel.FeatureRecord{}, nil, errors.New("runtime: update requires feature id")
	}
	before, err := c.store.Read(id)
	if err != nil {
		return kernel.FeatureRecord{}, nil, err
	}

	desired := before
	if enabled != nil {
		desired.Enabled = *enabled
	}
	if config != nil {
		desired.Config = append(json.RawMessage(nil), (*config)...)
	}

	definition, definitionErr := c.resolve(id)
	if definitionErr != nil {
		if desired.Enabled || config != nil {
			return kernel.FeatureRecord{}, nil, fmt.Errorf(
				"runtime: unavailable feature %s cannot be enabled or reconfigured: %w",
				id,
				definitionErr,
			)
		}
		record, err := c.store.Update(id, enabled, nil)
		return record, nil, err
	}
	if err := definition.Validate(desired.Config); err != nil {
		return kernel.FeatureRecord{}, nil, err
	}

	if desired.Enabled == before.Enabled && bytes.Equal(desired.Config, before.Config) {
		snapshot, snapshotErr := c.registry.Read(id)
		if snapshotErr != nil && !errors.Is(snapshotErr, kernel.ErrFeatureNotFound) {
			return kernel.FeatureRecord{}, nil, snapshotErr
		}
		if snapshotErr == nil {
			return before, &snapshot, nil
		}
		return before, nil, nil
	}

	if _, err := c.registry.Read(id); errors.Is(err, kernel.ErrFeatureNotFound) {
		module, buildErr := definition.Build(c.session)
		if buildErr != nil {
			return kernel.FeatureRecord{}, nil, buildErr
		}
		if err := c.registry.Create(module, before); err != nil {
			return kernel.FeatureRecord{}, nil, err
		}
	} else if err != nil {
		return kernel.FeatureRecord{}, nil, err
	}

	oldSnapshot, err := c.registry.Read(id)
	if err != nil {
		return kernel.FeatureRecord{}, nil, err
	}
	wasRunning := snapshotHasLiveInstance(oldSnapshot)

	if wasRunning {
		if err := c.registry.Stop(ctx, id); err != nil {
			return kernel.FeatureRecord{}, nil, err
		}
	}
	if _, err := c.registry.Update(id, &desired.Enabled, &desired.Config); err != nil {
		rollbackErr := c.rollbackUpdate(before, wasRunning)
		return kernel.FeatureRecord{}, nil, errors.Join(err, rollbackErr)
	}
	if desired.Enabled {
		if err := c.registry.Start(ctx, id); err != nil {
			rollbackErr := c.rollbackUpdate(before, wasRunning)
			return kernel.FeatureRecord{}, nil, errors.Join(err, rollbackErr)
		}
	}

	persisted, err := c.store.Update(id, &desired.Enabled, &desired.Config)
	if err != nil {
		rollbackErr := c.rollbackUpdate(before, wasRunning)
		return kernel.FeatureRecord{}, nil, errors.Join(err, rollbackErr)
	}
	snapshot, err := c.registry.Read(id)
	if err != nil {
		return kernel.FeatureRecord{}, nil, err
	}
	return persisted, &snapshot, nil
}

func (c *runtimeController) rollbackUpdate(
	before kernel.FeatureRecord,
	restart bool,
) error {
	ctx, cancel := context.WithTimeout(c.ctx, liveMutationTimeout)
	defer cancel()

	var errs []error
	if err := c.registry.Stop(ctx, before.ID); err != nil &&
		!errors.Is(err, kernel.ErrFeatureNotFound) {
		errs = append(errs, fmt.Errorf("rollback stop: %w", err))
	}
	if _, err := c.registry.Update(before.ID, &before.Enabled, &before.Config); err != nil {
		errs = append(errs, fmt.Errorf("rollback update: %w", err))
	}
	if before.Enabled && restart {
		if err := c.registry.Start(ctx, before.ID); err != nil {
			errs = append(errs, fmt.Errorf("rollback restart: %w", err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("runtime rollback failed: %w", errors.Join(errs...))
}

func (c *runtimeController) delete(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("runtime: delete requires feature id")
	}
	before, err := c.store.Read(id)
	if err != nil {
		return err
	}

	_, registryErr := c.registry.Read(id)
	hadRegistry := registryErr == nil
	if registryErr != nil && !errors.Is(registryErr, kernel.ErrFeatureNotFound) {
		return registryErr
	}
	if hadRegistry {
		if err := c.registry.Delete(ctx, id); err != nil {
			return err
		}
	}
	if err := c.store.Delete(id); err != nil {
		if hadRegistry {
			return errors.Join(err, c.restoreDeleted(before))
		}
		return err
	}
	return nil
}

func (c *runtimeController) restoreDeleted(record kernel.FeatureRecord) error {
	definition, err := c.resolve(record.ID)
	if err != nil {
		return fmt.Errorf("restore deleted feature definition: %w", err)
	}
	module, err := definition.Build(c.session)
	if err != nil {
		return fmt.Errorf("restore deleted feature build: %w", err)
	}
	if err := c.registry.Create(module, record); err != nil {
		return fmt.Errorf("restore deleted feature registry: %w", err)
	}
	if record.Enabled {
		ctx, cancel := context.WithTimeout(c.ctx, liveMutationTimeout)
		defer cancel()
		if err := c.registry.Start(ctx, record.ID); err != nil {
			return fmt.Errorf("restore deleted feature start: %w", err)
		}
	}
	return nil
}

func snapshotHasLiveInstance(snapshot kernel.Snapshot) bool {
	switch snapshot.State {
	case kernel.StateStarting,
		kernel.StateReady,
		kernel.StateDegraded,
		kernel.StateFailed,
		kernel.StateStopping:
		return true
	default:
		return false
	}
}
