package preferences_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/presentation/preferences"
)

func TestDefaultsAndBoundedColors(t *testing.T) {
	layout := preferences.Defaults()
	if err := layout.Validate(); err != nil {
		t.Fatal(err)
	}
	if !layout.Sidebar.Visible || !layout.Sidebar.Identity || !layout.Sidebar.Context ||
		!layout.Sidebar.Provider || !layout.Sidebar.Status {
		t.Fatalf("default sidebar = %#v", layout.Sidebar)
	}
	if layout.Colors.Accent != preferences.ColorCyan || layout.Colors.Border != preferences.ColorBlue ||
		layout.Colors.Background != preferences.ColorBlack || layout.Colors.Text != preferences.ColorWhite {
		t.Fatalf("default colors = %#v", layout.Colors)
	}
	for _, value := range preferences.ColorNames(false) {
		if _, err := preferences.ParseColor(value, false); err != nil {
			t.Fatalf("ParseColor(%q): %v", value, err)
		}
	}
	if color, err := preferences.ParseColor("default", true); err != nil || color != preferences.ColorTerminal {
		t.Fatalf("background default = %q/%v", color, err)
	}
	for _, value := range []string{"\x1b[31m", "rgb(1,2,3)", "unknown", "terminal"} {
		if _, err := preferences.ParseColor(value, false); err == nil {
			t.Fatalf("foreground accepted %q", value)
		}
	}
}

func TestServicePersistsReloadsAndResetsPresentationOnly(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "xdg", "praetor")
	service, err := preferences.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	if service.Current() != preferences.Defaults() {
		t.Fatalf("missing file layout = %#v", service.Current())
	}
	if err := service.SetSidebarVisible(false); err != nil {
		t.Fatal(err)
	}
	if err := service.SetSidebarSection("identity", false); err != nil {
		t.Fatal(err)
	}
	if err := service.SetColor("accent", preferences.ColorGreen); err != nil {
		t.Fatal(err)
	}
	if err := service.SetColor("background", preferences.ColorBlack); err != nil {
		t.Fatal(err)
	}

	reloaded, err := preferences.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	current := reloaded.Current()
	if current.Sidebar.Visible || current.Sidebar.Identity || current.Colors.Accent != preferences.ColorGreen ||
		current.Colors.Background != preferences.ColorBlack {
		t.Fatalf("reloaded layout = %#v", current)
	}
	info, err := os.Stat(service.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("preference permissions = %o", info.Mode().Perm())
	}
	if err := reloaded.Reset(); err != nil {
		t.Fatal(err)
	}
	reset, err := preferences.Open(directory)
	if err != nil || reset.Current() != preferences.Defaults() {
		t.Fatalf("reset layout = %#v/%v", reset.Current(), err)
	}
}

func TestMalformedOversizedAndSymlinkFilesFailClosed(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string)
	}{
		{name: "malformed", prepare: func(t *testing.T, path string) { writePreferenceTestFile(t, path, []byte("{bad")) }},
		{name: "unknown field", prepare: func(t *testing.T, path string) {
			writePreferenceTestFile(t, path, []byte(`{"schema_version":1,"sidebar":{},"colors":{},"extra":true}`))
		}},
		{name: "oversized", prepare: func(t *testing.T, path string) {
			writePreferenceTestFile(t, path, []byte(strings.Repeat("x", 17*1024)))
		}},
		{name: "unsafe permissions", prepare: func(t *testing.T, path string) {
			writePreferenceTestFile(t, path, []byte(`{"schema_version":1}`))
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", prepare: func(t *testing.T, path string) {
			target := filepath.Join(t.TempDir(), "target")
			writePreferenceTestFile(t, target, []byte("{}"))
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "praetor")
			if err := os.MkdirAll(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			test.prepare(t, preferences.Path(directory))
			if _, err := preferences.Open(directory); err == nil {
				t.Fatal("Open() accepted unsafe preferences")
			}
		})
	}
}

func TestInvalidUpdateDoesNotPartiallyPersist(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "praetor")
	service, err := preferences.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetColor("accent", preferences.Color("\x1b[31m")); err == nil {
		t.Fatal("SetColor accepted ANSI injection")
	}
	if err := service.SetSidebarSection("unknown", false); err == nil {
		t.Fatal("SetSidebarSection accepted unknown section")
	}
	if err := service.SetColor("background", preferences.ColorCyan); err == nil {
		t.Fatal("SetColor accepted background matching the accent")
	}
	if service.Current() != preferences.Defaults() {
		t.Fatalf("invalid update changed memory: %#v", service.Current())
	}
	if _, err := os.Stat(service.ConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("invalid update persisted file: %v", err)
	}
}

func TestResolveConfigDirUsesAbsoluteXDGConfiguration(t *testing.T) {
	root := filepath.Join(t.TempDir(), "config")
	t.Setenv("XDG_CONFIG_HOME", root)
	resolved, err := preferences.ResolveConfigDir()
	if err != nil || resolved != filepath.Join(root, "praetor") {
		t.Fatalf("ResolveConfigDir() = %q/%v", resolved, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if _, err := preferences.ResolveConfigDir(); err == nil {
		t.Fatal("ResolveConfigDir accepted relative XDG_CONFIG_HOME")
	}
}

func writePreferenceTestFile(t *testing.T, path string, payload []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPersistedFormatIsVersionedJSON(t *testing.T) {
	service, err := preferences.Open(filepath.Join(t.TempDir(), "praetor"))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Reset(); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(service.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	if document["schema_version"] != float64(preferences.SchemaVersion) {
		t.Fatalf("schema version = %#v", document["schema_version"])
	}
}
