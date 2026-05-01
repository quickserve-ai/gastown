package dog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureKennelSettings_CreatesIfMissing(t *testing.T) {
	kennelDir := t.TempDir()

	if err := ensureKennelSettings(kennelDir); err != nil {
		t.Fatalf("ensureKennelSettings: %v", err)
	}

	settingsPath := filepath.Join(kennelDir, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json not created: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if settings["skipDangerousModePermissionPrompt"] != true {
		t.Error("skipDangerousModePermissionPrompt should be true")
	}

	perms, ok := settings["permissions"].(map[string]interface{})
	if !ok {
		t.Fatal("permissions should be an object")
	}
	if perms["defaultMode"] != "bypassPermissions" {
		t.Errorf("defaultMode = %v, want bypassPermissions", perms["defaultMode"])
	}

	plugins, ok := settings["enabledPlugins"].(map[string]interface{})
	if !ok {
		t.Fatal("enabledPlugins should be an object")
	}
	if plugins["beads@beads-marketplace"] != false {
		t.Error("beads@beads-marketplace should be false")
	}
}

func TestEnsureKennelSettings_IdempotentIfExists(t *testing.T) {
	kennelDir := t.TempDir()

	// Create settings first call
	if err := ensureKennelSettings(kennelDir); err != nil {
		t.Fatalf("first call: %v", err)
	}

	settingsPath := filepath.Join(kennelDir, ".claude", "settings.json")
	firstStat, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatalf("stat after first call: %v", err)
	}

	// Write different content to simulate manual customization
	customContent := []byte(`{"custom": true}`)
	if err := os.WriteFile(settingsPath, customContent, 0644); err != nil {
		t.Fatalf("writing custom content: %v", err)
	}

	// Second call should not overwrite
	if err := ensureKennelSettings(kennelDir); err != nil {
		t.Fatalf("second call: %v", err)
	}

	secondData, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read after second call: %v", err)
	}

	if string(secondData) != string(customContent) {
		t.Error("ensureKennelSettings overwrote existing settings (should be idempotent)")
	}

	_ = firstStat // suppress unused warning
}

func TestEnsureKennelSettings_NoHooks(t *testing.T) {
	kennelDir := t.TempDir()

	if err := ensureKennelSettings(kennelDir); err != nil {
		t.Fatalf("ensureKennelSettings: %v", err)
	}

	settingsPath := filepath.Join(kennelDir, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, hasHooks := settings["hooks"]; hasHooks {
		t.Error("dog kennel settings should not contain hooks (no guard hooks for dogs)")
	}
}
