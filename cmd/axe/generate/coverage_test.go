package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── writeTemplate ────────────────────────────────────────────────────────────

func TestWriteTemplate_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.go")

	data := newResourceData("Post", nil, "", false, false, false)
	err := writeTemplate(path, `package test
// Name: {{.Name}}
`, data)
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Name: Post")
}

func TestWriteTemplate_SkipsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.go")

	// Create pre-existing file.
	require.NoError(t, os.WriteFile(path, []byte("original"), 0644))

	data := newResourceData("Post", nil, "", false, false, false)
	err := writeTemplate(path, `overwritten`, data)
	require.NoError(t, err)

	// Original should be preserved.
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "original", string(content))
}

func TestWriteTemplate_BadTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.go")

	data := newResourceData("Post", nil, "", false, false, false)
	err := writeTemplate(path, `{{.BadField`, data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse template")
}

func TestWriteTemplate_BadPath(t *testing.T) {
	data := newResourceData("Post", nil, "", false, false, false)
	err := writeTemplate("/nonexistent/dir/file.go", `test`, data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create file")
}

func TestWriteTemplate_WithFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "with_fields.go")

	fields, err := parseFields("title:string,body:text")
	require.NoError(t, err)

	data := newResourceData("Post", fields, "", false, false, false)
	err = writeTemplate(path, `// {{.Name}} has {{len .Fields}} fields`, data)
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Post has 2 fields")
}

// ── migrationPath ────────────────────────────────────────────────────────────

func TestMigrationPath_Format(t *testing.T) {
	path := migrationPath("posts")
	assert.True(t, strings.HasPrefix(path, "db/migrations/"))
	assert.True(t, strings.HasSuffix(path, "_create_posts.sql"))
	// Should contain a timestamp in the middle.
	base := filepath.Base(path)
	parts := strings.SplitN(base, "_", 2)
	assert.Len(t, parts[0], 14, "timestamp should be 14 chars (20060102150405)")
}

// ── readModuleName ───────────────────────────────────────────────────────────

func TestReadModuleName_Success(t *testing.T) {
	// We're running from the axe repo which has a go.mod.
	name := readModuleName()
	assert.Equal(t, "github.com/axe-cute/axe", name)
}

func TestReadModuleName_MissingGoMod(t *testing.T) {
	// chdir to temp dir with no go.mod to test fallback.
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))
	name := readModuleName()
	// Falls back to default.
	assert.Equal(t, "github.com/axe-cute/axe", name)
}

// ── goCommand ────────────────────────────────────────────────────────────────

func TestGoCommand_ReturnsPath(t *testing.T) {
	path := goCommand()
	assert.NotEmpty(t, path)
	// Should contain "go" somewhere in the path.
	assert.Contains(t, path, "go")
}

// ── Command ──────────────────────────────────────────────────────────────────

func TestCommand_HasResourceSubcommand(t *testing.T) {
	cmd := Command()
	assert.Equal(t, "generate", cmd.Use)
	assert.Equal(t, []string{"g"}, cmd.Aliases)

	// Should have the "resource" subcommand.
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "resource [Name]" || strings.HasPrefix(sub.Use, "resource") {
			found = true
			break
		}
	}
	assert.True(t, found, "generate command should have 'resource' subcommand")
}

// ── Template rendering checks ────────────────────────────────────────────────

func TestTemplates_Compile(t *testing.T) {
	// Verify all template strings are valid Go templates (no parse errors).
	templates := map[string]string{
		"domain":    domainTmpl,
		"service":   serviceTmpl,
		"handler":   handlerTmpl,
		"repo":      repoTmpl,
		"migration": migrationTmpl,
		"entSchema": entSchemaTmpl,
	}

	data := newResourceData("TestResource", nil, "", true, false, false)
	for name, tmplStr := range templates {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, name+".go")
			err := writeTemplate(path, tmplStr, data)
			require.NoError(t, err, "template %q failed to render", name)

			content, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.NotEmpty(t, content, "rendered template %q is empty", name)
		})
	}
}

func TestTemplates_CompileWithFields(t *testing.T) {
	fields, err := parseFields("title:string,body:text,published:bool,author_id:uuid")
	require.NoError(t, err)

	data := newResourceData("Article", fields, "", true, true, true)

	templates := map[string]string{
		"domain":    domainTmpl,
		"service":   serviceTmpl,
		"handler":   handlerTmpl,
		"repo":      repoTmpl,
		"migration": migrationTmpl,
		"entSchema": entSchemaTmpl,
	}

	for name, tmplStr := range templates {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, name+".go")
			err := writeTemplate(path, tmplStr, data)
			require.NoError(t, err, "template %q with fields failed", name)
		})
	}
}
