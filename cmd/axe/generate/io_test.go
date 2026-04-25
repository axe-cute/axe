package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── generateResource (file I/O only — skip go generate/build) ────────────────

// testGenerateFiles verifies that generateResource creates the expected files.
// It uses a temp directory to isolate from the real project.
// NOTE: generateResource calls `go generate`, `go build`, `go mod tidy` which
// will fail in the temp dir — that's expected. We test the file creation part.
func TestGenerateResource_CreatesExpectedFiles(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))

	// Create required directory structure.
	for _, d := range []string{
		"internal/domain",
		"internal/handler",
		"internal/service",
		"internal/repository",
		"ent/schema",
		"db/queries",
		"db/migrations",
	} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	fields, err := parseFields("title:string,body:text")
	require.NoError(t, err)

	data := ResourceData{
		Name:       "Post",
		NameLower:  "post",
		NamePlural: "posts",
		NameSnake:  "post",
		Fields:     fields,
		Module:     "github.com/test/project",
		Date:       "2026-01-01",
	}

	// generateResource will fail at go generate/build steps (no go.mod in temp dir)
	// but the files should still be created.
	_ = generateResource(data)

	// Verify files were created.
	expectedFiles := []string{
		"internal/domain/post.go",
		"internal/handler/post_handler.go",
		"internal/handler/post_handler_test.go",
		"internal/service/post_service.go",
		"internal/service/post_service_test.go",
		"internal/repository/post_repo.go",
		"ent/schema/post.go",
		"db/queries/post.sql",
	}

	for _, f := range expectedFiles {
		_, err := os.Stat(f)
		assert.NoError(t, err, "expected file %s to exist", f)
	}

	// Verify migration file was created (has timestamp prefix).
	matches, _ := filepath.Glob("db/migrations/*_create_posts.sql")
	assert.Len(t, matches, 1, "should have exactly one migration file")
}

func TestGenerateResource_WithFields(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))

	for _, d := range []string{
		"internal/domain", "internal/handler", "internal/service",
		"internal/repository", "ent/schema", "db/queries", "db/migrations",
	} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	fields, err := parseFields("title:string,body:text,published:bool")
	require.NoError(t, err)

	data := ResourceData{
		Name: "Article", NameLower: "article", NamePlural: "articles",
		NameSnake: "article", Fields: fields, Module: "github.com/test/project",
		Date: "2026-01-01",
	}

	_ = generateResource(data)

	// Verify domain file contains field definitions.
	content, err := os.ReadFile("internal/domain/article.go")
	require.NoError(t, err)
	assert.Contains(t, string(content), "Article")
}

func TestGenerateResource_WithWS(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))

	for _, d := range []string{
		"internal/domain", "internal/handler", "internal/service",
		"internal/repository", "ent/schema", "db/queries", "db/migrations",
		"pkg/ws",
	} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	data := ResourceData{
		Name: "Chat", NameLower: "chat", NamePlural: "chats",
		NameSnake: "chat", Module: "github.com/test/project",
		Date: "2026-01-01", WithWS: true,
	}

	_ = generateResource(data)

	// Should create the WS handler file.
	_, err = os.Stat("internal/handler/chat_ws_handler.go")
	assert.NoError(t, err, "WS handler should be created")
}

// ── wireMainGo ───────────────────────────────────────────────────────────────

func TestWireMainGo_NoMainFile(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))

	data := ResourceData{
		Name: "Post", NameLower: "post", NamePlural: "posts",
		Module: "github.com/test/project",
	}

	// Should not error — just prints info message.
	err = wireMainGo(data)
	assert.NoError(t, err)
}

func TestWireMainGo_AlreadyWired(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))

	// Create cmd/api/main.go with existing handler wiring.
	require.NoError(t, os.MkdirAll("cmd/api", 0o755))
	mainContent := `package main
// axe:wire:repo
// axe:wire:handler
// axe:wire:route
postHandler := handler.NewPostHandler(postSvc)
`
	require.NoError(t, os.WriteFile("cmd/api/main.go", []byte(mainContent), 0o644))

	data := ResourceData{
		Name: "Post", NameLower: "post", NamePlural: "posts",
		Module: "github.com/test/project",
	}

	// Should skip — already wired.
	err = wireMainGo(data)
	assert.NoError(t, err)

	// File should be unchanged.
	content, _ := os.ReadFile("cmd/api/main.go")
	assert.Equal(t, mainContent, string(content))
}

func TestWireMainGo_MissingMarkers(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))

	// Create cmd/api/main.go without markers.
	require.NoError(t, os.MkdirAll("cmd/api", 0o755))
	mainContent := `package main
func main() {}
`
	require.NoError(t, os.WriteFile("cmd/api/main.go", []byte(mainContent), 0o644))

	data := ResourceData{
		Name: "Post", NameLower: "post", NamePlural: "posts",
		Module: "github.com/test/project",
	}

	// Should skip gracefully — no markers found.
	err = wireMainGo(data)
	assert.NoError(t, err)
}

func TestWireMainGo_InjectsCode(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))

	require.NoError(t, os.MkdirAll("cmd/api", 0o755))
	mainContent := `package main

import (
	// axe:wire:import
)

func main() {
	// axe:wire:db
	// axe:wire:repo
	// axe:wire:handler
	// axe:wire:route
}
`
	require.NoError(t, os.WriteFile("cmd/api/main.go", []byte(mainContent), 0o644))

	data := ResourceData{
		Name: "Post", NameLower: "post", NamePlural: "posts",
		NameSnake: "post", Module: "github.com/test/project",
	}

	err = wireMainGo(data)
	require.NoError(t, err)

	content, _ := os.ReadFile("cmd/api/main.go")
	s := string(content)

	// Should contain injected repo, handler, and route lines.
	assert.Contains(t, s, "postRepo := repository.NewPostRepo(entClient)")
	assert.Contains(t, s, "postSvc := service.NewPostService(postRepo)")
	assert.Contains(t, s, "postHandler := handler.NewPostHandler(postSvc)")
	assert.Contains(t, s, `r.Mount("/posts", postHandler.Routes())`)
}

func TestWireMainGo_WithWSRoute(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))

	require.NoError(t, os.MkdirAll("cmd/api", 0o755))
	mainContent := `package main

func main() {
	// axe:wire:repo
	// axe:wire:handler
	// axe:wire:route
	// axe:wire:ws-route
}
`
	require.NoError(t, os.WriteFile("cmd/api/main.go", []byte(mainContent), 0o644))

	data := ResourceData{
		Name: "Chat", NameLower: "chat", NamePlural: "chats",
		Module: "github.com/test/project", WithWS: true,
	}

	err = wireMainGo(data)
	require.NoError(t, err)

	content, _ := os.ReadFile("cmd/api/main.go")
	s := string(content)

	assert.Contains(t, s, "chatWSHandler := handler.NewChatWSHandler(wsHub, wsTracker)")
	assert.Contains(t, s, `/ws/chats`)
}

// ── printManualWiring ────────────────────────────────────────────────────────

func TestPrintManualWiring_NoWS(t *testing.T) {
	// Capture stdout is complex; just verify it doesn't panic.
	data := ResourceData{
		Name: "Post", NameLower: "post", NamePlural: "posts",
		Module: "github.com/test/project",
	}
	// Should not panic.
	printManualWiring(data)
}

func TestPrintManualWiring_WithWS(t *testing.T) {
	data := ResourceData{
		Name: "Chat", NameLower: "chat", NamePlural: "chats",
		Module: "github.com/test/project", WithWS: true,
	}
	// Should not panic — includes WS wiring instructions.
	printManualWiring(data)
}

// ── readModuleName edge cases ────────────────────────────────────────────────

func TestReadModuleName_CustomModule(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))

	goMod := "module github.com/myorg/myproject\n\ngo 1.25.0\n"
	require.NoError(t, os.WriteFile("go.mod", []byte(goMod), 0o644))

	name := readModuleName()
	assert.Equal(t, "github.com/myorg/myproject", name)
}

func TestReadModuleName_EmptyGoMod(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))

	// go.mod exists but has no "module" line → fallback.
	require.NoError(t, os.WriteFile("go.mod", []byte("go 1.25.0\n"), 0o644))

	name := readModuleName()
	assert.Equal(t, "github.com/axe-cute/axe", name)
}

// ── scaffoldWSPackage ────────────────────────────────────────────────────────

func TestScaffoldWSPackage_CreatesFiles(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))

	data := ResourceData{
		Name: "Chat", NameLower: "chat", Module: "github.com/test/project",
	}

	// Will fail at `go get` step (no go.mod) but files should be created.
	_ = scaffoldWSPackage(data)

	// Verify WS package files were created.
	wsFiles := []string{
		"pkg/ws/adapter.go",
		"pkg/ws/metrics.go",
		"pkg/ws/room.go",
		"pkg/ws/client.go",
		"pkg/ws/hub.go",
		"pkg/ws/auth.go",
		"pkg/ws/redis_adapter.go",
	}

	for _, f := range wsFiles {
		_, err := os.Stat(f)
		assert.NoError(t, err, "expected %s to exist", f)
	}
}

func TestScaffoldWSPackage_SkipsExisting(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck

	require.NoError(t, os.Chdir(dir))

	// Pre-create one file.
	require.NoError(t, os.MkdirAll("pkg/ws", 0o755))
	require.NoError(t, os.WriteFile("pkg/ws/hub.go", []byte("existing"), 0o644))

	data := ResourceData{
		Name: "Chat", Module: "github.com/test/project",
	}

	_ = scaffoldWSPackage(data)

	// hub.go should be unchanged (skipped).
	content, _ := os.ReadFile("pkg/ws/hub.go")
	assert.Equal(t, "existing", string(content))
}

// ── resourceCmd flags ────────────────────────────────────────────────────────

func TestResourceCmd_Flags(t *testing.T) {
	cmd := resourceCmd()

	// Should have all expected flags.
	flags := []string{"fields", "belongs-to", "with-auth", "admin-only", "with-ws"}
	for _, f := range flags {
		flag := cmd.Flag(f)
		assert.NotNil(t, flag, "missing flag: %s", f)
	}
}

func TestResourceCmd_NoArgs(t *testing.T) {
	cmd := resourceCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "should fail with no args")
}

func TestResourceCmd_ReservedName(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck
	require.NoError(t, os.Chdir(dir))

	cmd := resourceCmd()
	cmd.SetArgs([]string{"Config"})
	err = cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reserved")
}

func TestResourceCmd_InvalidField(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck
	require.NoError(t, os.Chdir(dir))

	cmd := resourceCmd()
	cmd.SetArgs([]string{"Post", "--fields", "badfield"})
	err = cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid field")
}

// ── newResourceData ──────────────────────────────────────────────────────────

func TestNewResourceData_PluralEndsWithS(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck
	require.NoError(t, os.Chdir(dir))

	data := newResourceData("Status", nil, "", false, false, false)
	assert.Equal(t, "statuses", data.NamePlural)
}

func TestNewResourceData_AdminOnlyImpliesAuth_IO(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck
	require.NoError(t, os.Chdir(dir))

	data := newResourceData("Post", nil, "", false, true, false)
	assert.True(t, data.WithAuth, "admin-only should imply with-auth")
	assert.True(t, data.AdminOnly)
}

func TestNewResourceData_WithBelongsTo(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck
	require.NoError(t, os.Chdir(dir))

	data := newResourceData("Comment", nil, "Post", false, false, false)
	assert.Equal(t, "Post", data.BelongsTo)
	assert.Contains(t, strings.ToLower(data.Name), "comment")
}

// Regression: the generated handler must thread the parent FK all the way
// from HTTP body → domain input, and reject unknown fields on decode.
//
// Historically the handler template forgot to include BelongsTo in the
// request DTO, so POST /comments/ with {"post_id":"..."} silently dropped
// the FK and the service threw "post_id is required". This test locks
// that behaviour in place.
func TestGenerateResource_HandlerPropagatesBelongsTo(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(orig) //nolint:errcheck
	require.NoError(t, os.Chdir(dir))

	// generateResource writes into these subtrees — create them upfront.
	for _, d := range []string{
		"internal/domain",
		"internal/handler",
		"internal/service",
		"internal/repository",
		"ent/schema",
		"db/queries",
		"db/migrations",
	} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	data := ResourceData{
		Name:       "Comment",
		NameLower:  "comment",
		NamePlural: "comments",
		NameSnake:  "comment",
		BelongsTo:  "Post",
		Module:     "github.com/test/app",
		Date:       "2026-01-01",
	}
	_ = generateResource(data) // build steps will fail in temp dir; files still land

	content, err := os.ReadFile("internal/handler/comment_handler.go")
	require.NoError(t, err)
	got := string(content)

	// 1. Request DTO carries the FK as a string (JSON-friendly UUID).
	assert.Contains(t, got, `PostID string `, "createCommentRequest must expose PostID")
	assert.Contains(t, got, `json:"post_id"`, "FK JSON tag must be snake_case post_id")

	// 2. Create handler parses the FK into a uuid.UUID and forwards it.
	assert.Contains(t, got, "uuid.Parse(req.PostID)", "Create must parse the FK")
	assert.Contains(t, got, "PostID: parentID", "Create must forward parsed FK into domain input")

	// 3. Decoder rejects unknown fields on Create + Update — catches
	// client/server schema drift at the boundary instead of silently
	// dropping data.
	assert.Contains(t, got, "DisallowUnknownFields()", "decoder must reject unknown fields")

	// 4. Response exposes the FK so clients can display the relation.
	assert.Contains(t, got, `PostID: e.PostID.String()`, "response must serialise FK as string")
}
