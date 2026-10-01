package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	systemdunit "github.com/YMGPwcca/phonelink-linux/packaging/systemd"
)

const (
	userServiceUnitName = "phonelink-linux.service"
	installedBinaryName  = "phonelink-linux"
)

var graphicalEnvironmentKeys = []string{
	"WAYLAND_DISPLAY",
	"DISPLAY",
	"XAUTHORITY",
	"XDG_SESSION_TYPE",
	"XDG_CURRENT_DESKTOP",
	"XDG_SESSION_DESKTOP",
	"XDG_CONFIG_HOME",
	"XDG_STATE_HOME",
	"XDG_DATA_HOME",
}

type userServicePaths struct {
	Binary string
	Unit   string
}

func resolveUserServicePaths() (userServicePaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return userServicePaths{}, fmt.Errorf("resolve home directory: %w", err)
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return userServicePaths{}, fmt.Errorf("resolve user config directory: %w", err)
	}
	return userServicePaths{
		Binary: filepath.Join(home, ".local", "bin", installedBinaryName),
		Unit: filepath.Join(
			config,
			"systemd",
			"user",
			userServiceUnitName,
		),
	}, nil
}

func runServiceCommand(args []string) error {
	if len(args) == 0 {
		serviceUsage()
		return errors.New("service subcommand is required")
	}

	switch args[0] {
	case "install":
		return runServiceInstall(args[1:])
	case "uninstall":
		return runServiceUninstall(args[1:])
	case "start":
		if len(args) != 1 {
			return errors.New("service start takes no arguments")
		}
		if err := prepareServiceStart(); err != nil {
			return err
		}
		return systemctlUser("start", userServiceUnitName)
	case "stop":
		if len(args) != 1 {
			return errors.New("service stop takes no arguments")
		}
		return systemctlUser("stop", userServiceUnitName)
	case "restart":
		if len(args) != 1 {
			return errors.New("service restart takes no arguments")
		}
		if err := prepareServiceStart(); err != nil {
			return err
		}
		return systemctlUser("restart", userServiceUnitName)
	case "enable":
		if len(args) != 1 {
			return errors.New("service enable takes no arguments")
		}
		return systemctlUser("enable", userServiceUnitName)
	case "disable":
		if len(args) != 1 {
			return errors.New("service disable takes no arguments")
		}
		return systemctlUser("disable", userServiceUnitName)
	case "status":
		if len(args) != 1 {
			return errors.New("service status takes no arguments")
		}
		return systemctlUser("--no-pager", "--full", "status", userServiceUnitName)
	case "logs":
		return runServiceLogs(args[1:])
	case "import-environment":
		if len(args) != 1 {
			return errors.New("service import-environment takes no arguments")
		}
		return importGraphicalEnvironment()
	case "help", "-h", "--help":
		serviceUsage()
		return nil
	default:
		serviceUsage()
		return fmt.Errorf("unknown service subcommand %q", args[0])
	}
}

func runServiceInstall(args []string) error {
	fs := flag.NewFlagSet("service install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	enable := true
	start := true
	fs.BoolVar(&enable, "enable", true, "enable the service for graphical sessions")
	fs.BoolVar(&start, "start", true, "start the service immediately")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	paths, err := resolveUserServicePaths()
	if err != nil {
		return err
	}
	if err := installCurrentExecutable(paths.Binary); err != nil {
		return err
	}
	if err := writeFileAtomic(paths.Unit, systemdunit.UserService, 0o644); err != nil {
		return fmt.Errorf("install systemd user unit: %w", err)
	}
	// Import before daemon-reload so a custom XDG_CONFIG_HOME used by this
	// process is also visible to the systemd user manager while it rebuilds
	// its unit search path. This matters even when --start=false.
	if err := importGraphicalEnvironment(); err != nil {
		return err
	}
	if err := systemctlUser("daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd user manager: %w", err)
	}

	if start {
		if err := resetServiceFailedState(); err != nil {
			return err
		}
	}

	for _, action := range serviceInstallActions(enable, start) {
		if err := systemctlUser(action...); err != nil {
			return fmt.Errorf(
				"apply service install action %s: %w",
				strings.Join(action, " "),
				err,
			)
		}
	}

	fmt.Printf("Installed binary: %s\n", paths.Binary)
	fmt.Printf("Installed user unit: %s\n", paths.Unit)
	if enable {
		fmt.Printf("Enabled: %s\n", userServiceUnitName)
	}
	if start {
		fmt.Printf("Started: %s\n", userServiceUnitName)
	}
	fmt.Println("Logs: phonelink-linux service logs --follow")
	return nil
}

func serviceInstallActions(enable, start bool) [][]string {
	var actions [][]string
	if enable {
		actions = append(actions, []string{"enable", userServiceUnitName})
	}
	if start {
		// restart starts an inactive unit too, and guarantees that a reinstall
		// runs the freshly copied binary instead of leaving an old process alive.
		actions = append(actions, []string{"restart", userServiceUnitName})
	}
	return actions
}

func runServiceUninstall(args []string) error {
	fs := flag.NewFlagSet("service uninstall", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	keepBinary := false
	fs.BoolVar(&keepBinary, "keep-binary", false, "leave ~/.local/bin/phonelink-linux installed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	paths, err := resolveUserServicePaths()
	if err != nil {
		return err
	}

	// Stop/disable first, but continue cleanup if the unit is already absent or
	// the user manager no longer knows about it.
	if err := systemctlUser("disable", "--now", userServiceUnitName); err != nil {
		fmt.Fprintf(os.Stderr, "[service] disable/stop warning: %v\n", err)
	}

	if err := removeIfExists(paths.Unit); err != nil {
		return fmt.Errorf("remove systemd user unit: %w", err)
	}
	if !keepBinary {
		if err := removeIfExists(paths.Binary); err != nil {
			return fmt.Errorf("remove installed binary: %w", err)
		}
	}
	if err := systemctlUser("daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd user manager: %w", err)
	}
	if err := systemctlUser("reset-failed", userServiceUnitName); err != nil {
		fmt.Fprintf(os.Stderr, "[service] reset-failed warning: %v\n", err)
	}

	fmt.Printf("Removed user unit: %s\n", paths.Unit)
	if keepBinary {
		fmt.Printf("Kept binary: %s\n", paths.Binary)
	} else {
		fmt.Printf("Removed binary: %s\n", paths.Binary)
	}
	return nil
}

func runServiceLogs(args []string) error {
	fs := flag.NewFlagSet("service logs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	follow := false
	lines := 100
	fs.BoolVar(&follow, "follow", false, "follow new journal entries")
	fs.IntVar(&lines, "lines", 100, "number of recent journal entries")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if lines < 0 {
		return errors.New("service logs --lines must be non-negative")
	}

	journalArgs := []string{
		"--user",
		"-u",
		userServiceUnitName,
		"--no-pager",
		"-n",
		strconv.Itoa(lines),
	}
	if follow {
		journalArgs = append(journalArgs, "-f")
	}
	return runInteractiveCommand("journalctl", journalArgs...)
}

func prepareServiceStart() error {
	if err := importGraphicalEnvironment(); err != nil {
		return err
	}
	return resetServiceFailedState()
}

func resetServiceFailedState() error {
	if err := systemctlUser("reset-failed", userServiceUnitName); err != nil {
		return fmt.Errorf("reset failed/start-limit state for %s: %w", userServiceUnitName, err)
	}
	return nil
}

func importGraphicalEnvironment() error {
	var names []string
	for _, key := range graphicalEnvironmentKeys {
		if _, ok := os.LookupEnv(key); ok {
			names = append(names, key)
		}
	}
	if len(names) == 0 {
		return nil
	}
	args := append([]string{"import-environment"}, names...)
	if err := systemctlUser(args...); err != nil {
		return fmt.Errorf("import graphical session environment: %w", err)
	}
	return nil
}

func installCurrentExecutable(destination string) error {
	source, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return fmt.Errorf("resolve current executable symlinks: %w", err)
	}

	sourceInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("stat current executable: %w", err)
	}
	if destinationInfo, err := os.Stat(destination); err == nil {
		if os.SameFile(sourceInfo, destinationInfo) {
			if err := os.Chmod(destination, 0o755); err != nil {
				return fmt.Errorf("chmod installed executable: %w", err)
			}
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat installed executable: %w", err)
	}

	sourceFile, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open current executable: %w", err)
	}
	defer sourceFile.Close()

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create binary install directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".phonelink-linux-*")
	if err != nil {
		return fmt.Errorf("create temporary installed executable: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o755); err != nil {
		return fmt.Errorf("chmod temporary installed executable: %w", err)
	}
	if _, err := io.Copy(tmp, sourceFile); err != nil {
		return fmt.Errorf("copy installed executable: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync installed executable: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close installed executable: %w", err)
	}
	if err := os.Rename(tmpName, destination); err != nil {
		return fmt.Errorf("replace installed executable: %w", err)
	}
	cleanup = false
	return syncDirectory(filepath.Dir(destination))
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".phonelink-linux-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func systemctlUser(args ...string) error {
	all := append([]string{"--user"}, args...)
	return runInteractiveCommand("systemctl", all...)
}

func runInteractiveCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func serviceUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  phonelink-linux service install [--enable=true] [--start=true]")
	fmt.Fprintln(os.Stderr, "  phonelink-linux service uninstall [--keep-binary]")
	fmt.Fprintln(os.Stderr, "  phonelink-linux service start|stop|restart")
	fmt.Fprintln(os.Stderr, "  phonelink-linux service enable|disable|status")
	fmt.Fprintln(os.Stderr, "  phonelink-linux service logs [--follow] [--lines N]")
	fmt.Fprintln(os.Stderr, "  phonelink-linux service import-environment")
}
