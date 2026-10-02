package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveUserServicePathsUsesXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(t.TempDir(), "xdg-config")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", config)

	paths, err := resolveUserServicePaths()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".local", "bin", installedBinaryName); paths.Binary != want {
		t.Fatalf("binary=%q want=%q", paths.Binary, want)
	}
	if want := filepath.Join(config, "systemd", "user", userServiceUnitName); paths.Unit != want {
		t.Fatalf("unit=%q want=%q", paths.Unit, want)
	}
}

func TestWriteFileAtomicCreatesAndReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "service")
	if err := writeFileAtomic(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("two"), 0o640); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte("two")) {
		t.Fatalf("data=%q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("mode=%#o", got)
	}
}

func TestInstallCurrentExecutableCreatesRunnableCopy(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "bin", installedBinaryName)
	if err := installCurrentExecutable(destination); err != nil {
		t.Fatal(err)
	}

	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	destinationInfo, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if sourceInfo.Size() != destinationInfo.Size() {
		t.Fatalf(
			"installed size=%d source size=%d",
			destinationInfo.Size(),
			sourceInfo.Size(),
		)
	}
	if destinationInfo.Mode().Perm() != 0o755 {
		t.Fatalf("installed mode=%#o", destinationInfo.Mode().Perm())
	}
}

func TestGraphicalEnvironmentImportDoesNotOverrideRuntimeDir(t *testing.T) {
	for _, key := range graphicalEnvironmentKeys {
		if key == "XDG_RUNTIME_DIR" {
			t.Fatal("XDG_RUNTIME_DIR belongs to the systemd user manager and must not be imported")
		}
	}
}

func TestServiceInstallActionsRestartFreshBinary(t *testing.T) {
	tests := []struct {
		name   string
		enable bool
		start  bool
		want   [][]string
	}{
		{
			name:   "enable and start",
			enable: true,
			start:  true,
			want: [][]string{
				{"enable", userServiceUnitName},
				{"restart", userServiceUnitName},
			},
		},
		{
			name:   "start only",
			enable: false,
			start:  true,
			want: [][]string{
				{"restart", userServiceUnitName},
			},
		},
		{
			name:   "enable only",
			enable: true,
			start:  false,
			want: [][]string{
				{"enable", userServiceUnitName},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serviceInstallActions(tt.enable, tt.start)
			if len(got) != len(tt.want) {
				t.Fatalf("actions=%#v want=%#v", got, tt.want)
			}
			for index := range tt.want {
				if strings.Join(got[index], "\x00") !=
					strings.Join(tt.want[index], "\x00") {
					t.Fatalf("actions=%#v want=%#v", got, tt.want)
				}
			}
		})
	}
}
