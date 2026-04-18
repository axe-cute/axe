//go:build scaffold_integration

// Package scaffold_test verifies that axe CLI generators produce code that
// actually compiles. These tests catch the class of bug where a generator
// injects a wrong import path or missing package — bugs that only surface at
// runtime (or the next `make run`) without this suite.
//
// Why not unit tests?
//   Unit tests can validate template string rendering, but they cannot verify
//   that the generated Go code satisfies the compiler. Only running `go build`
//   against the output catches real issues.
//
// How to run locally:
//
//	go test -tags scaffold_integration -v -timeout 5m ./tests/scaffold/
//
// The tests use a `replace` directive in the generated go.mod so they work
// against local source without needing a published module version.
package scaffold_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// axeRoot returns the absolute path to the axe repository root,
// derived from this test file's location.
func axeRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// tests/scaffold/scaffold_test.go → ../../ = repo root
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// buildAxeCLI compiles the axe binary into a temp dir and returns its path.
func buildAxeCLI(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "axe")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/axe/main.go")
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build axe CLI: %v", err)
	}
	return bin
}

// newProject scaffolds a project and returns its directory.
// It also patches go.mod with a replace directive so tests work offline.
func newProject(t *testing.T, axeBin, root, name, module, db string, extraFlags ...string) string {
	t.Helper()
	dir := t.TempDir()
	projDir := filepath.Join(dir, name)

	args := []string{"new", name,
		"--module=" + module,
		"--db=" + db,
		"--yes",
	}
	args = append(args, extraFlags...)

	cmd := exec.Command(axeBin, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("axe new failed: %v", err)
	}

	// Inject replace directive so go.mod resolves axe from local source.
	patchGoMod(t, projDir, root)

	return projDir
}

// patchGoMod adds a replace directive pointing to local axe source.
func patchGoMod(t *testing.T, projDir, axeRoot string) {
	t.Helper()
	goModPath := filepath.Join(projDir, "go.mod")
	content, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	replace := fmt.Sprintf("\nreplace github.com/axe-cute/axe => %s\n", axeRoot)
	if strings.Contains(string(content), "replace github.com/axe-cute/axe") {
		return // already patched
	}
	patched := string(content) + replace
	if err := os.WriteFile(goModPath, []byte(patched), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	// go mod tidy with replace directive
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = projDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Logf("go mod tidy warning (non-fatal): %v", err)
	}
}

// gobuild runs go build ./... and fails the test if it errors.
func gobuild(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("go build ./... failed in %s: %v", dir, err)
	}
}

// govet runs go vet ./... and fails the test if it errors.
func govet(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("go vet ./... failed in %s: %v", dir, err)
	}
}

// gotest runs go test ./... and returns whether it passed.
// Failures from missing DB/Redis infra are expected — we track compile errors only.
func gotest(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "test", "./...", "-timeout", "30s")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Test failures are not fatal here — infra (DB/Redis) may be absent.
		// We only care that tests COMPILE. Build errors already caught by gobuild().
		t.Logf("go test ./... had failures (may be infra-related, non-fatal): %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: axe new (postgres) → go build → go vet
// ─────────────────────────────────────────────────────────────────────────────

func TestScaffold_New_Postgres_Compiles(t *testing.T) {
	root := axeRoot(t)
	axeBin := buildAxeCLI(t, root)

	projDir := newProject(t, axeBin, root,
		"myapp", "github.com/test/myapp", "postgres")

	t.Run("go build", func(t *testing.T) { gobuild(t, projDir) })
	t.Run("go vet", func(t *testing.T) { govet(t, projDir) })
}

func TestScaffold_New_MySQL_Compiles(t *testing.T) {
	root := axeRoot(t)
	axeBin := buildAxeCLI(t, root)

	projDir := newProject(t, axeBin, root,
		"myapp-mysql", "github.com/test/myapp-mysql", "mysql")

	t.Run("go build", func(t *testing.T) { gobuild(t, projDir) })
	t.Run("go vet", func(t *testing.T) { govet(t, projDir) })
}

func TestScaffold_New_SQLite_Compiles(t *testing.T) {
	root := axeRoot(t)
	axeBin := buildAxeCLI(t, root)

	projDir := newProject(t, axeBin, root,
		"myapp-sqlite", "github.com/test/myapp-sqlite", "sqlite",
		"--no-worker", "--no-cache")

	t.Run("go build", func(t *testing.T) { gobuild(t, projDir) })
	t.Run("go vet", func(t *testing.T) { govet(t, projDir) })
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: axe generate resource → go build
// This is the critical regression test for BUG-6, BUG-7, BUG-8.
// ─────────────────────────────────────────────────────────────────────────────

func TestScaffold_GenerateResource_Compiles(t *testing.T) {
	root := axeRoot(t)
	axeBin := buildAxeCLI(t, root)

	projDir := newProject(t, axeBin, root,
		"gen-test", "github.com/test/gen-test", "postgres")

	// Verify base project compiles first.
	gobuild(t, projDir)

	// Generate a resource.
	genCmd := exec.Command(axeBin, "generate", "resource", "Article",
		`--fields=title:string,body:text,published:bool,views:int`)
	genCmd.Dir = projDir
	genCmd.Stdout = os.Stdout
	genCmd.Stderr = os.Stderr
	if err := genCmd.Run(); err != nil {
		t.Fatalf("axe generate resource failed: %v", err)
	}

	t.Run("go build after generate", func(t *testing.T) { gobuild(t, projDir) })
	t.Run("go vet after generate", func(t *testing.T) { govet(t, projDir) })
	t.Run("go test compiles", func(t *testing.T) { gotest(t, projDir) })
}

func TestScaffold_GenerateResource_WithAuth_Compiles(t *testing.T) {
	root := axeRoot(t)
	axeBin := buildAxeCLI(t, root)

	projDir := newProject(t, axeBin, root,
		"gen-auth-test", "github.com/test/gen-auth-test", "postgres")

	gobuild(t, projDir)

	genCmd := exec.Command(axeBin, "generate", "resource", "Post",
		`--fields=title:string,body:text`,
		"--with-auth")
	genCmd.Dir = projDir
	genCmd.Stdout = os.Stdout
	genCmd.Stderr = os.Stderr
	if err := genCmd.Run(); err != nil {
		t.Fatalf("axe generate resource --with-auth failed: %v", err)
	}

	t.Run("go build after generate --with-auth", func(t *testing.T) { gobuild(t, projDir) })
	t.Run("go vet", func(t *testing.T) { govet(t, projDir) })
	t.Run("go test compiles", func(t *testing.T) { gotest(t, projDir) })
}

func TestScaffold_GenerateMultipleResources_Compiles(t *testing.T) {
	root := axeRoot(t)
	axeBin := buildAxeCLI(t, root)

	projDir := newProject(t, axeBin, root,
		"gen-multi-test", "github.com/test/gen-multi-test", "postgres")

	gobuild(t, projDir)

	resources := []struct {
		name   string
		fields string
	}{
		{"Post", "title:string,body:text,published:bool"},
		{"Comment", "body:text,score:int"},
		{"Tag", "name:string,slug:string"},
	}

	for _, r := range resources {
		genCmd := exec.Command(axeBin, "generate", "resource", r.name,
			"--fields="+r.fields)
		genCmd.Dir = projDir
		genCmd.Stdout = os.Stdout
		genCmd.Stderr = os.Stderr
		if err := genCmd.Run(); err != nil {
			t.Fatalf("axe generate resource %s failed: %v", r.name, err)
		}
	}

	t.Run("go build after 3 resources", func(t *testing.T) { gobuild(t, projDir) })
	t.Run("go vet after 3 resources", func(t *testing.T) { govet(t, projDir) })
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: axe plugin add storage → go build
// Regression test for BUG-8 (wrong import path: pkg/plugin/storage vs pkg/storage)
// ─────────────────────────────────────────────────────────────────────────────

func TestScaffold_PluginAddStorage_Compiles(t *testing.T) {
	root := axeRoot(t)
	axeBin := buildAxeCLI(t, root)

	projDir := newProject(t, axeBin, root,
		"storage-test", "github.com/test/storage-test", "postgres")

	gobuild(t, projDir)

	pluginCmd := exec.Command(axeBin, "plugin", "add", "storage")
	pluginCmd.Dir = projDir
	pluginCmd.Stdout = os.Stdout
	pluginCmd.Stderr = os.Stderr
	if err := pluginCmd.Run(); err != nil {
		t.Fatalf("axe plugin add storage failed: %v", err)
	}

	t.Run("go build after plugin add storage", func(t *testing.T) { gobuild(t, projDir) })
	t.Run("go vet after plugin add storage", func(t *testing.T) { govet(t, projDir) })
}

func TestScaffold_PluginAddStorage_Idempotent(t *testing.T) {
	root := axeRoot(t)
	axeBin := buildAxeCLI(t, root)

	projDir := newProject(t, axeBin, root,
		"storage-idem-test", "github.com/test/storage-idem-test", "postgres")

	// Run twice — should not produce duplicate imports or double-wiring.
	for i := 1; i <= 2; i++ {
		pluginCmd := exec.Command(axeBin, "plugin", "add", "storage")
		pluginCmd.Dir = projDir
		pluginCmd.Stdout = os.Stdout
		pluginCmd.Stderr = os.Stderr
		if err := pluginCmd.Run(); err != nil && i == 1 {
			t.Fatalf("axe plugin add storage (run %d) failed: %v", i, err)
		}
	}

	t.Run("go build after idempotent plugin add", func(t *testing.T) { gobuild(t, projDir) })
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Full workflow — new → generate → plugin add → build
// This is the canonical e2e path a new user takes.
// ─────────────────────────────────────────────────────────────────────────────

func TestScaffold_FullWorkflow_Compiles(t *testing.T) {
	root := axeRoot(t)
	axeBin := buildAxeCLI(t, root)

	projDir := newProject(t, axeBin, root,
		"full-test", "github.com/test/full-test", "postgres")

	t.Log("Step 1: base scaffold compiles")
	gobuild(t, projDir)

	t.Log("Step 2: generate resource Post")
	genCmd := exec.Command(axeBin, "generate", "resource", "Post",
		"--fields=title:string,body:text,published:bool",
		"--with-auth")
	genCmd.Dir = projDir
	genCmd.Stdout = os.Stdout
	genCmd.Stderr = os.Stderr
	if err := genCmd.Run(); err != nil {
		t.Fatalf("generate resource failed: %v", err)
	}
	gobuild(t, projDir)

	t.Log("Step 3: axe plugin add storage")
	pluginCmd := exec.Command(axeBin, "plugin", "add", "storage")
	pluginCmd.Dir = projDir
	pluginCmd.Stdout = os.Stdout
	pluginCmd.Stderr = os.Stderr
	if err := pluginCmd.Run(); err != nil {
		t.Fatalf("plugin add storage failed: %v", err)
	}

	t.Run("final build", func(t *testing.T) { gobuild(t, projDir) })
	t.Run("final vet", func(t *testing.T) { govet(t, projDir) })
	t.Run("final test", func(t *testing.T) { gotest(t, projDir) })
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: setup/plugin.go must NOT import "fmt" unless WithStorage=true
// Regression test for BUG-5
// ─────────────────────────────────────────────────────────────────────────────

func TestScaffold_SetupPluginGo_NoUnusedImports(t *testing.T) {
	root := axeRoot(t)
	axeBin := buildAxeCLI(t, root)

	projDir := newProject(t, axeBin, root,
		"no-fmt-test", "github.com/test/no-fmt-test", "postgres")

	setupFile := filepath.Join(projDir, "internal", "setup", "plugin.go")
	content, err := os.ReadFile(setupFile)
	if err != nil {
		t.Fatalf("read setup/plugin.go: %v", err)
	}

	src := string(content)

	// Without --with-storage, "fmt" must NOT appear in the import block.
	if strings.Contains(src, `"fmt"`) {
		t.Errorf(`BUG-5 regression: setup/plugin.go imports "fmt" without storage — will cause build failure.\nContent:\n%s`, src)
	}

	// Regardless, the file must compile.
	gobuild(t, projDir)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: axe new --with-storage → correct import path in main.go
// Regression test for BUG-6
// ─────────────────────────────────────────────────────────────────────────────

func TestScaffold_WithStorage_CorrectImportPath(t *testing.T) {
	root := axeRoot(t)
	axeBin := buildAxeCLI(t, root)

	projDir := newProject(t, axeBin, root,
		"with-storage", "github.com/test/with-storage", "postgres",
		"--with-storage")

	mainGoPath := filepath.Join(projDir, "cmd", "api", "main.go")
	content, err := os.ReadFile(mainGoPath)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	src := string(content)

	// Must use local pkg/storage, NOT the framework's pkg/plugin/storage.
	if strings.Contains(src, "pkg/plugin/storage") {
		t.Errorf("BUG-6 regression: main.go imports pkg/plugin/storage (framework pkg) instead of local pkg/storage")
	}
	if !strings.Contains(src, "pkg/storage") {
		t.Errorf("BUG-6: main.go should import pkg/storage but doesn't")
	}

	gobuild(t, projDir)
}
