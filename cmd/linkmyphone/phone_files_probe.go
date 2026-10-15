package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/YMGPwcca/linkmyphone/runtime/controlplane"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
)

// runPhoneFilesProbe makes a one-shot request through the existing daemon.
// It never opens a second SignalR connection or changes authentication state.
func runPhoneFilesProbe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("phone-files-probe", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	execute := fs.Bool("execute", false, "explicitly send one read-only metadata request")
	featurePath, err := kernel.DefaultFeatureStorePath()
	if err != nil { return err }
	fs.StringVar(&featurePath, "features-state", featurePath, "running daemon's feature registry path")
	if err := fs.Parse(args); err != nil { return err }
	if fs.NArg() != 0 { return errors.New("phone-files-probe: unexpected positional arguments") }
	if !*execute {
		return errors.New("phone-files-probe: add --execute to send one read-only FullSync first-batch GET")
	}

	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	response, err := controlplane.Call(callCtx, controlplane.SocketPathForStore(featurePath), controlplane.Request{
		Version: controlplane.ProtocolVersion,
		Operation: controlplane.OperationPhoneFilesProbe,
	})
	if err != nil { return fmt.Errorf("phone-files-probe: active daemon required: %w", err) }
	if !response.OK { return fmt.Errorf("phone-files-probe: %s", response.Error) }
	if response.PhoneFilesProbe == nil { return errors.New("phone-files-probe: daemon returned no probe result") }

	p := response.PhoneFilesProbe
	fmt.Println("LinkMyPhone Phone Files F1 (read-only)")
	fmt.Printf("DRM response status: %d\n", p.DeviceResourceStatus)
	if p.DeviceResourceStatus == 1 {
		fmt.Printf("Phone Files status: %d (%s)\n", p.Status, phoneFilesStatusName(p.Status))
		if p.Status == 1 {
			fmt.Printf("Metadata entries in first batch: %d\n", p.Files)
			fmt.Printf("Has more batches: %t\n", p.HasMoreBatches)
			fmt.Printf("Batch index: %d\n", p.BatchIndex)
			fmt.Printf("Estimated total items: %d\n", p.EstimatedTotalItems)
		}
	}
	fmt.Printf("Response payload bytes: %d\n", p.PayloadBytes)
	fmt.Printf("Elapsed: %d ms\n", p.ElapsedMS)
	if p.DeviceResourceStatus != 1 || p.Status != 1 {
		return errors.New("phone-files-probe: remote request did not return Phone Files SUCCESS")
	}
	return nil
}

func phoneFilesStatusName(status uint64) string {
	switch status {
	case 1: return "SUCCESS"
	case 2: return "NO_PERMISSION"
	case 3: return "NOT_FOUND"
	case 4: return "CHECKSUM_MISMATCH"
	case 5: return "IO_ERROR"
	case 6: return "NOT_SUPPORTED"
	case 7: return "REQUEST_NOT_SET"
	case 8: return "FEATURE_DISABLED"
	case 9: return "PLATFORM_ERROR"
	case 10: return "CANCELLED"
	case 11: return "SYNC_FAILED"
	case 12: return "BATCH_RESTART_REQUIRED"
	default: return "UNKNOWN"
	}
}
