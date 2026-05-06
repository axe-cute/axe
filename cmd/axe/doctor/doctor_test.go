package doctor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyPluginGo is the buggy template emitted by axe < v0.1.10.
const legacyPluginGo = `package setup

import (
	"context"
	"fmt"

	"github.com/example/app/config"
	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/example/app/pkg/storage"
	// axe:wire:import
)

func RegisterPlugins(_ context.Context, app *plugin.App, cfg *config.Config) error {
	storagePlug, err := storage.New(storage.Config{
		Backend:     cfg.StorageBackend,
		MountPath:   cfg.StorageMountPath,
		MaxFileSize: cfg.StorageMaxFileSize,
		URLPrefix:   cfg.StorageURLPrefix,
	})
	if err != nil {
		return fmt.Errorf("storage plugin: %w", err)
	}
	if err := app.Use(storagePlug); err != nil {
		return fmt.Errorf("storage plugin: %w", err)
	}
	// axe:wire:plugin
	return nil
}
`

const goMod = "module github.com/example/app\n\ngo 1.25.0\n"

func writeProject(t *testing.T, pluginGo string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if pluginGo != "" {
		if err := os.MkdirAll(filepath.Join(dir, "internal/setup"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "internal/setup/plugin.go"), []byte(pluginGo), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRun_DetectsLegacyPlugin(t *testing.T) {
	dir := writeProject(t, legacyPluginGo)
	var buf bytes.Buffer
	err := run(dir, false, &buf)
	if err == nil {
		t.Fatalf("expected error when legacy plugin present, got nil; output:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "legacy-setup-plugin") {
		t.Errorf("expected legacy-setup-plugin in output:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "✗") {
		t.Errorf("expected failure marker in output:\n%s", buf.String())
	}
}

func TestRun_FixesLegacyPlugin(t *testing.T) {
	dir := writeProject(t, legacyPluginGo)
	var buf bytes.Buffer
	if err := run(dir, true, &buf); err != nil {
		t.Fatalf("run --fix: %v\noutput:\n%s", err, buf.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "internal/setup/plugin.go"))
	if err != nil {
		t.Fatal(err)
	}
	gotStr := string(got)
	if strings.Contains(gotStr, "storage.New") || strings.Contains(gotStr, "storagePlug") {
		t.Errorf("fix did not strip legacy storage block:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "axe:wire:plugin") {
		t.Errorf("fixed file missing wire marker:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "github.com/example/app/config") {
		t.Errorf("fixed file missing module path:\n%s", gotStr)
	}

	// Re-running detect should now be clean.
	buf.Reset()
	if err := run(dir, false, &buf); err != nil {
		t.Errorf("doctor reported issues after fix: %v\n%s", err, buf.String())
	}
}

func TestRun_CleanProject(t *testing.T) {
	dir := writeProject(t, "package setup\n")
	var buf bytes.Buffer
	if err := run(dir, false, &buf); err != nil {
		t.Errorf("expected no issues for clean project, got: %v\n%s", err, buf.String())
	}
}

func TestRun_NoSetupPluginFile(t *testing.T) {
	dir := writeProject(t, "")
	var buf bytes.Buffer
	if err := run(dir, false, &buf); err != nil {
		t.Errorf("expected no error when setup/plugin.go absent, got: %v\n%s", err, buf.String())
	}
}

func TestRun_NotAnAxeProject(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := run(dir, false, &buf); err == nil {
		t.Error("expected error when go.mod missing, got nil")
	}
}
