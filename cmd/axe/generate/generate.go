// Package generate provides the `axe generate resource` command.
// It scaffolds all Clean Architecture layers for a new domain resource.
package generate

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/spf13/cobra"
)

// Command returns the `axe generate` cobra command with sub-commands.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Code generation commands",
		Aliases: []string{"g"},
	}
	cmd.AddCommand(resourceCmd())
	return cmd
}

// ── Field parsing ─────────────────────────────────────────────────────────────

// Field represents a single domain field.
type Field struct {
	Name      string // camelCase, e.g. "authorID"
	NameSnake string // snake_case, e.g. "author_id"
	Type      string // Go type, e.g. "string", "uuid.UUID", "int64", "bool", "time.Time"
	SQLType   string // SQL column type, e.g. "TEXT", "UUID", "BIGINT", "BOOLEAN", "TIMESTAMPTZ"
	EntType   string // Ent field builder, e.g. `field.String`, `field.UUID`, `field.Bool`
	NullValue string // zero value expression, e.g. `""`, `uuid.Nil`, `0`, `false`
	JSONTag   string // json tag name, e.g. "author_id"
}

// parseFields parses "title:string,body:text,published:bool,views:int" into []Field.
func parseFields(raw string) ([]Field, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	fields := make([]Field, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid field spec %q: expected name:type", p)
		}
		name := strings.TrimSpace(kv[0])
		typ := strings.ToLower(strings.TrimSpace(kv[1]))

		f, err := buildField(name, typ)
		if err != nil {
			return nil, err
		}
		fields = append(fields, f)
	}
	return fields, nil
}

func buildField(name, typ string) (Field, error) {
	snake := toSnake(name)
	camel := toCamel(name)

	switch typ {
	case "string", "str":
		return Field{camel, snake, "string", "VARCHAR(255)", "field.String", `""`, snake}, nil
	case "text":
		return Field{camel, snake, "string", "TEXT", "field.String", `""`, snake}, nil
	case "int", "integer", "int64":
		return Field{camel, snake, "int64", "BIGINT", "field.Int64", "0", snake}, nil
	case "float", "float64", "decimal":
		return Field{camel, snake, "float64", "DECIMAL(18,2)", "field.Float64", "0", snake}, nil
	case "bool", "boolean":
		return Field{camel, snake, "bool", "BOOLEAN", "field.Bool", "false", snake}, nil
	case "uuid":
		return Field{camel, snake, "uuid.UUID", "UUID", "field.UUID", "uuid.Nil", snake}, nil
	case "time", "datetime", "timestamp":
		return Field{camel, snake, "time.Time", "TIMESTAMPTZ", "field.Time", "time.Time{}", snake}, nil
	default:
		return Field{}, fmt.Errorf("unsupported field type %q — use: string, text, int, float, bool, uuid, time", typ)
	}
}

// ── Template data ─────────────────────────────────────────────────────────────

// ResourceData is passed to all templates.
type ResourceData struct {
	Name       string  // Title case, e.g. "Post"
	NameLower  string  // lowercase, e.g. "post"
	NamePlural string  // lowercase plural, e.g. "posts"
	NameSnake  string  // snake_case, e.g. "blog_post"
	Fields     []Field
	BelongsTo  string // optional, e.g. "User" → adds UserID field
	Module     string // go module path
	Date       string
	WithAuth   bool   // --with-auth: wrap routes with JWTAuth middleware
	AdminOnly  bool   // --admin-only: further restrict to admin role only
	WithWS     bool   // --with-ws: add a WebSocket room endpoint
}

func newResourceData(name string, fields []Field, belongsTo string, withAuth, adminOnly, withWS bool) ResourceData {
	module := readModuleName()
	plural := name + "s"
	if strings.HasSuffix(strings.ToLower(name), "s") {
		plural = strings.ToLower(name) + "es"
	}
	return ResourceData{
		Name:       toTitle(name),
		NameLower:  strings.ToLower(name[:1]) + name[1:],
		NamePlural: strings.ToLower(plural),
		NameSnake:  toSnake(name),
		Fields:     fields,
		BelongsTo:  belongsTo,
		Module:     module,
		Date:       time.Now().Format("2006-01-02"),
		WithAuth:   withAuth || adminOnly, // --admin-only implies --with-auth
		AdminOnly:  adminOnly,
		WithWS:     withWS,
	}
}

// ── Resource command ──────────────────────────────────────────────────────────

func resourceCmd() *cobra.Command {
	var fieldsFlag string
	var belongsTo string
	var withAuth bool
	var adminOnly bool
	var withWS bool

	cmd := &cobra.Command{
		Use:   "resource <Name>",
		Short: "Generate a full CRUD resource (domain + handler + service + repo + schema + SQL)",
		Example: `  axe generate resource Post --fields="title:string,body:text,published:bool"
  axe generate resource Comment --fields="body:text,score:int" --belongs-to=Post
  axe generate resource Order --fields="amount:float" --with-auth
  axe generate resource Config --fields="key:string,value:text" --admin-only`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if name == "" {
				return fmt.Errorf("resource name is required")
			}
			name = toTitle(name)

			fields, err := parseFields(fieldsFlag)
			if err != nil {
				return err
			}

			data := newResourceData(name, fields, belongsTo, withAuth, adminOnly, withWS)
			return generateResource(data)
		},
	}

	cmd.Flags().StringVar(&fieldsFlag, "fields", "", `Field definitions: "name:type,..." (types: string, text, int, float, bool, uuid, time)`)
	cmd.Flags().StringVar(&belongsTo, "belongs-to", "", "Parent entity name for foreign key relationship")
	cmd.Flags().BoolVar(&withAuth, "with-auth", false, "Wrap all routes with JWTAuth middleware")
	cmd.Flags().BoolVar(&adminOnly, "admin-only", false, "Restrict all routes to admin role (implies --with-auth)")
	cmd.Flags().BoolVar(&withWS, "with-ws", false, "Generate a WebSocket room handler (requires pkg/ws.Hub)")
	return cmd
}

// scaffoldWSPackage writes all pkg/ws/ files into the current project and
// ensures nhooyr.io/websocket is present in go.mod. Safe to call multiple
// times — existing files are skipped.
func scaffoldWSPackage(data ResourceData) error {
	if err := os.MkdirAll("pkg/ws", 0o755); err != nil {
		return fmt.Errorf("create pkg/ws dir: %w", err)
	}

	wsFiles := []struct {
		path    string
		tmplStr string
	}{
		{"pkg/ws/adapter.go", wsPkgAdapterTmpl},
		{"pkg/ws/metrics.go", wsPkgMetricsTmpl},
		{"pkg/ws/room.go", wsPkgRoomTmpl},
		{"pkg/ws/client.go", wsPkgClientTmpl},
		{"pkg/ws/hub.go", wsPkgHubTmpl},
		{"pkg/ws/auth.go", wsPkgAuthTmpl},
		{"pkg/ws/redis_adapter.go", wsPkgRedisAdapterTmpl},
	}

	for _, f := range wsFiles {
		if _, err := os.Stat(f.path); err == nil {
			// Already exists — skip to allow idempotent re-runs.
			continue
		}
		if err := writeTemplate(f.path, f.tmplStr, data); err != nil {
			return fmt.Errorf("scaffold pkg/ws: %w", err)
		}
		fmt.Printf("   ❖ %s\n", f.path)
	}

	// Ensure nhooyr.io/websocket is in go.mod.
	goBin, err := exec.LookPath("go")
	if err != nil {
		// Fallback to well-known location on macOS/Linux.
		goBin = "/usr/local/go/bin/go"
	}

	fmt.Println("\n   ⏳ Running go get nhooyr.io/websocket...")
	getCmd := exec.Command(goBin, "get", "nhooyr.io/websocket") //nolint:gosec
	getCmd.Stdout = os.Stdout
	getCmd.Stderr = os.Stderr
	if err := getCmd.Run(); err != nil {
		fmt.Printf("   ⚠️  go get nhooyr.io/websocket failed: %v\n", err)
		fmt.Println("      Run `go get nhooyr.io/websocket` manually inside the project.")
	} else {
		fmt.Println("   ⏳ Running go mod tidy...")
		tidyCmd := exec.Command(goBin, "mod", "tidy") //nolint:gosec
		tidyCmd.Stdout = os.Stdout
		tidyCmd.Stderr = os.Stderr
		if err := tidyCmd.Run(); err != nil {
			fmt.Printf("   ⚠️  go mod tidy failed: %v\n", err)
		} else {
			fmt.Println("   ✓ go.mod and go.sum updated")
		}
	}
	return nil
}

// generateResource writes all files for a new resource.
func generateResource(data ResourceData) error {
	files := []struct {
		path    string
		tmplStr string
	}{
		{fmt.Sprintf("internal/domain/%s.go", data.NameSnake), domainTmpl},
		{fmt.Sprintf("internal/handler/%s_handler.go", data.NameSnake), handlerTmpl},
		{fmt.Sprintf("internal/handler/%s_handler_test.go", data.NameSnake), handlerTestTmpl},
		{fmt.Sprintf("internal/service/%s_service.go", data.NameSnake), serviceTmpl},
		{fmt.Sprintf("internal/service/%s_service_test.go", data.NameSnake), serviceTestTmpl},
		{fmt.Sprintf("internal/repository/%s_repo.go", data.NameSnake), repoTmpl},
		{fmt.Sprintf("ent/schema/%s.go", data.NameSnake), entSchemaTmpl},
		{fmt.Sprintf("db/queries/%s.sql", data.NameSnake), sqlQueryTmpl},
		{migrationPath(data.NamePlural), migrationTmpl},
	}
	if data.WithWS {
		files = append(files, struct {
			path    string
			tmplStr string
		}{fmt.Sprintf("internal/handler/%s_ws_handler.go", data.NameSnake), wsHandlerTmpl})
	}

	generated := []string{}
	for _, f := range files {
		if err := writeTemplate(f.path, f.tmplStr, data); err != nil {
			return fmt.Errorf("generate %s: %w", f.path, err)
		}
		generated = append(generated, f.path)
	}

	// Scaffold pkg/ws package if requested (idempotent).
	if data.WithWS {
		fmt.Println("\n   📦 Scaffolding pkg/ws/ (WebSocket hub + auth middleware)...")
		if err := scaffoldWSPackage(data); err != nil {
			fmt.Printf("   ⚠️  pkg/ws scaffold error: %v\n", err)
		}
	}

	authNote := ""
	if data.WithAuth && data.AdminOnly {
		authNote = " // ⚡ admin-only routes — WrappedWithJWTAuth + RequireRole(\"admin\")"
	} else if data.WithAuth {
		authNote = " // ⚡ protected routes — wrapped with JWTAuth middleware"
	}

	mountLine := fmt.Sprintf(`       %sHandler := handler.New%sHandler(%sSvc)
       r.Mount("/api/v1/%s", %sHandler.Routes())%s`,
		data.NameLower, data.Name, data.NameLower, data.NamePlural, data.NameLower, authNote)

	fmt.Printf("\n✅ Generated %d files for resource %q:\n", len(generated), data.Name)
	for _, g := range generated {
		fmt.Printf("   ❖ %s\n", g)
	}

	authFlag := ""
	if data.AdminOnly {
		authFlag = " --admin-only"
	} else if data.WithAuth {
		authFlag = " --with-auth"
	}
	_ = authFlag

	nextSteps := `
Next steps:
  1. Register route in cmd/api/main.go:
%s
  2. Run: go generate ./ent/...
  3. Run: make migrate-up
  4. Run: make test
`
	if data.WithWS {
		nextSteps = `
Next steps:
  1. Register REST route in cmd/api/main.go:
%s
  2. Register WebSocket handler (needs *ws.Hub):
       %sWSHandler := handler.New%sWSHandler(wsHub)
       r.Mount("/ws/%s", %sWSHandler.Routes())
  3. Run: go generate ./ent/...
  4. Run: make migrate-up
  5. Run: make test
`
		nextSteps = fmt.Sprintf(nextSteps, mountLine,
			data.NameLower, data.Name, data.NamePlural, data.NameLower)
	} else {
		nextSteps = fmt.Sprintf(nextSteps, mountLine)
	}
	fmt.Print(nextSteps)

	return nil
}

func migrationPath(plural string) string {
	ts := time.Now().Format("20060102150405")
	return fmt.Sprintf("db/migrations/%s_create_%s.sql", ts, plural)
}

func writeTemplate(path, tmplStr string, data ResourceData) error {
	// Skip if file already exists
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("   ⚠️  Skipped (exists): %s\n", path)
		return nil
	}

	tmpl, err := template.New(path).Funcs(template.FuncMap{
		"lower": strings.ToLower,
		"title": toTitle,
		"snake": toSnake,
		"bt":    bt, // backtick — templates can't contain literal backticks in raw strings
	}).Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func toTitle(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func toCamel(s string) string {
	parts := strings.Split(s, "_")
	if len(parts) == 1 {
		return strings.ToLower(s[:1]) + s[1:]
	}
	result := strings.ToLower(parts[0])
	for _, p := range parts[1:] {
		if p != "" {
			result += strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return result
}

func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteRune('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func readModuleName() string {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "github.com/axe-cute/axe"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return "github.com/axe-cute/axe"
}
