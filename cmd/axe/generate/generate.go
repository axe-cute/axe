// Package generate provides the `axe generate resource` command.
// It scaffolds all Clean Architecture layers for a new domain resource.
package generate

import (
	"fmt"
	"os"
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
}

func newResourceData(name string, fields []Field, belongsTo string) ResourceData {
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
	}
}

// ── Resource command ──────────────────────────────────────────────────────────

func resourceCmd() *cobra.Command {
	var fieldsFlag string
	var belongsTo string

	cmd := &cobra.Command{
		Use:   "resource <Name>",
		Short: "Generate a full CRUD resource (domain + handler + service + repo + schema + SQL)",
		Example: `  axe generate resource Post --fields="title:string,body:text,published:bool"
  axe generate resource Comment --fields="body:text,score:int" --belongs-to=Post`,
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

			data := newResourceData(name, fields, belongsTo)
			return generateResource(data)
		},
	}

	cmd.Flags().StringVar(&fieldsFlag, "fields", "", `Field definitions: "name:type,..." (types: string, text, int, float, bool, uuid, time)`)
	cmd.Flags().StringVar(&belongsTo, "belongs-to", "", "Parent entity name for foreign key relationship")
	return cmd
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

	generated := []string{}
	for _, f := range files {
		if err := writeTemplate(f.path, f.tmplStr, data); err != nil {
			return fmt.Errorf("generate %s: %w", f.path, err)
		}
		generated = append(generated, f.path)
	}

	fmt.Printf("\n✅ Generated %d files for resource %q:\n", len(generated), data.Name)
	for _, g := range generated {
		fmt.Printf("   ✦ %s\n", g)
	}
	fmt.Printf(`
Next steps:
  1. Register route in cmd/api/main.go:
       %sHandler := handler.New%sHandler(%sSvc)
       r.Mount("/api/v1/%s", %sHandler.Routes())
  2. Run: go generate ./ent/...
  3. Run: make migrate-up
  4. Run: make test
`, data.NameLower, data.Name, data.NameLower, data.NamePlural, data.NameLower)

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
		return "github.com/axe-go/axe"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return "github.com/axe-go/axe"
}
