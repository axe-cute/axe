// ecommerce CLI — Developer tooling.
//
// Usage:
//
//	axe generate resource Chat --fields="title:string,body:text" --with-ws
//	go run ./cmd/axe migrate up / down / status / create <name>
//	go run ./cmd/axe version
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
)

const cliVersion = "0.1.0"
const migrationsDir = "db/migrations"

func main() {
	root := &cobra.Command{
		Use:           "axe",
		Short:         "ecommerce CLI — developer tooling",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(versionCmd(), migrateCmd(), generateCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print CLI version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("ecommerce cli %s\n", cliVersion)
		},
	}
}

// ── generate ──────────────────────────────────────────────────────────────────

func generateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "generate",
		Aliases: []string{"g"},
		Short:   "Code generation commands",
	}
	cmd.AddCommand(resourceCmd())
	return cmd
}

// resourceData is the template data for all resource templates.
type resourceData struct {
	Name       string
	NameLower  string
	NamePlural string
	NameSnake  string
	Fields     []field
	BelongsTo  string
	Module     string
	Date       string
	WithAuth   bool
	AdminOnly  bool
	WithWS     bool
}

// field represents a single domain field.
type field struct {
	Name      string
	NameSnake string
	Type      string
	SQLType   string
	EntType   string
	NullValue string
	JSONTag   string
}

func resourceCmd() *cobra.Command {
	var fieldsFlag string
	var belongsTo string
	var withAuth bool
	var adminOnly bool
	var withWS bool

	cmd := &cobra.Command{
		Use:   "resource <Name>",
		Short: "Generate a full CRUD resource (domain + handler + service + repo + schema + SQL)",
		Example: `  axe generate resource Post --fields="title:string,body:text"
  axe generate resource Order --fields="amount:float" --with-auth
  axe generate resource Event --fields="title:string" --with-ws`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := toTitle(args[0])
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

func newResourceData(name string, fields []field, belongsTo string, withAuth, adminOnly, withWS bool) resourceData {
	plural := name + "s"
	if strings.HasSuffix(strings.ToLower(name), "s") {
		plural = strings.ToLower(name) + "es"
	}
	return resourceData{
		Name:       toTitle(name),
		NameLower:  strings.ToLower(name[:1]) + name[1:],
		NamePlural: strings.ToLower(plural),
		NameSnake:  toSnake(name),
		Fields:     fields,
		BelongsTo:  belongsTo,
		Module:     readModuleName(),
		Date:       time.Now().Format("2006-01-02"),
		WithAuth:   withAuth || adminOnly,
		AdminOnly:  adminOnly,
		WithWS:     withWS,
	}
}

func parseFields(raw string) ([]field, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	fields := make([]field, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid field spec %q: expected name:type", p)
		}
		f, err := buildField(strings.TrimSpace(kv[0]), strings.ToLower(strings.TrimSpace(kv[1])))
		if err != nil {
			return nil, err
		}
		fields = append(fields, f)
	}
	return fields, nil
}

func buildField(name, typ string) (field, error) {
	snake := toSnake(name)
	camel := toCamel(name)
	switch typ {
	case "string", "str":
		return field{camel, snake, "string", "VARCHAR(255)", "field.String", `""`, snake}, nil
	case "text":
		return field{camel, snake, "string", "TEXT", "field.String", `""`, snake}, nil
	case "int", "integer", "int64":
		return field{camel, snake, "int64", "BIGINT", "field.Int64", "0", snake}, nil
	case "float", "float64", "decimal":
		return field{camel, snake, "float64", "DECIMAL(18,2)", "field.Float64", "0", snake}, nil
	case "bool", "boolean":
		return field{camel, snake, "bool", "BOOLEAN", "field.Bool", "false", snake}, nil
	case "uuid":
		return field{camel, snake, "uuid.UUID", "UUID", "field.UUID", "uuid.Nil", snake}, nil
	case "time", "datetime", "timestamp":
		return field{camel, snake, "time.Time", "TIMESTAMPTZ", "field.Time", "time.Time{}", snake}, nil
	default:
		return field{}, fmt.Errorf("unsupported field type %q — use: string, text, int, float, bool, uuid, time", typ)
	}
}

func generateResource(data resourceData) error {
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
		{migPathTimestamp(data.NamePlural), migrationTmpl},
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

	if data.WithWS {
		fmt.Println("\n   📦 Scaffolding pkg/ws/ (WebSocket hub + auth middleware)...")
		if err := scaffoldWSPackage(data); err != nil {
			fmt.Printf("   ⚠️  pkg/ws scaffold error: %v\n", err)
		}
	}

	mountLine := fmt.Sprintf("       %sHandler := handler.New%sHandler(%sSvc)\n       r.Mount(\"/api/v1/%s\", %sHandler.Routes())",
		data.NameLower, data.Name, data.NameLower, data.NamePlural, data.NameLower)

	fmt.Printf("\n✅ Generated %d files for resource %q:\n", len(generated), data.Name)
	for _, g := range generated {
		fmt.Printf("   ❖ %s\n", g)
	}

	if data.WithWS {
		fmt.Printf(`
Next steps:
  1. Register REST route in cmd/api/main.go:
%s
  2. Register WebSocket handler (needs *ws.Hub):
       %sWSHandler := handler.New%sWSHandler(wsHub, tracker)
       r.Mount("/ws/%s", %sWSHandler.Routes())
  3. Run: go generate ./ent/...
  4. Run: make migrate-up
  5. Run: make test
`, mountLine, data.NameLower, data.Name, data.NamePlural, data.NameLower)
	} else {
		fmt.Printf(`
Next steps:
  1. Register route in cmd/api/main.go:
%s
  2. Run: go generate ./ent/...
  3. Run: make migrate-up
  4. Run: make test
`, mountLine)
	}
	return nil
}

func scaffoldWSPackage(data resourceData) error {
	if err := os.MkdirAll("pkg/ws", 0o755); err != nil {
		return fmt.Errorf("create pkg/ws dir: %w", err)
	}

	wsFiles := []struct {
		path    string
		content string
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
			continue // already exists — idempotent
		}
		if err := writeTemplate(f.path, f.content, data); err != nil {
			return fmt.Errorf("scaffold %s: %w", f.path, err)
		}
		fmt.Printf("   ❖ %s\n", f.path)
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = "/usr/local/go/bin/go"
	}

	fmt.Println("\n   ⏳ Running go get nhooyr.io/websocket...")
	getCmd := exec.Command(goBin, "get", "nhooyr.io/websocket") //nolint:gosec
	getCmd.Stdout = os.Stdout
	getCmd.Stderr = os.Stderr
	if err := getCmd.Run(); err != nil {
		fmt.Printf("   ⚠️  go get nhooyr.io/websocket failed: %v\n", err)
		fmt.Println("      Run `go get nhooyr.io/websocket` manually.")
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

func migPathTimestamp(plural string) string {
	ts := time.Now().Format("20060102150405")
	return fmt.Sprintf("db/migrations/%s_create_%s.sql", ts, plural)
}

func writeTemplate(path, tmplStr string, data resourceData) error {
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("   ⚠️  Skipped (exists): %s\n", path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmpl, err := template.New(path).Funcs(template.FuncMap{
		"lower": strings.ToLower,
		"title": toTitle,
		"snake": toSnake,
		"bt":    func() string { return "`" },
	}).Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	return tmpl.Execute(f, data)
}

func readModuleName() string {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "github.com/axe-cute/examples-ecommerce"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return "github.com/axe-cute/examples-ecommerce"
}

// ── String helpers ────────────────────────────────────────────────────────────

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

// ── Resource templates ────────────────────────────────────────────────────────

const domainTmpl = `// Code generated by axe on {{.Date}}. Customize freely.
package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// {{.Name}} is the {{.Name}} domain entity.
type {{.Name}} struct {
	ID        uuid.UUID
{{- range .Fields}}
	{{title .Name}} {{.Type}}
{{- end}}
{{- if .BelongsTo}}
	{{.BelongsTo}}ID uuid.UUID
{{- end}}
	CreatedAt time.Time
	UpdatedAt time.Time
}

type {{.Name}}Repository interface {
	Create(ctx context.Context, input Create{{.Name}}Input) (*{{.Name}}, error)
	Update(ctx context.Context, id uuid.UUID, input Update{{.Name}}Input) (*{{.Name}}, error)
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*{{.Name}}, error)
	List(ctx context.Context, pagination Pagination) ([]*{{.Name}}, int, error)
}

type {{.Name}}Service interface {
	Create{{.Name}}(ctx context.Context, input Create{{.Name}}Input) (*{{.Name}}, error)
	Get{{.Name}}(ctx context.Context, id uuid.UUID) (*{{.Name}}, error)
	Update{{.Name}}(ctx context.Context, id uuid.UUID, input Update{{.Name}}Input) (*{{.Name}}, error)
	Delete{{.Name}}(ctx context.Context, id uuid.UUID) error
	List{{.Name}}s(ctx context.Context, pagination Pagination) ([]*{{.Name}}, int, error)
}

type Create{{.Name}}Input struct {
{{- range .Fields}}
	{{title .Name}} {{.Type}}
{{- end}}
{{- if .BelongsTo}}
	{{.BelongsTo}}ID uuid.UUID
{{- end}}
}

type Update{{.Name}}Input struct {
{{- range .Fields}}
	{{title .Name}} *{{.Type}} {{bt}}json:"{{.NameSnake}},omitempty"{{bt}}
{{- end}}
}
`

const handlerTmpl = `// Code generated by axe on {{.Date}}. Customize freely.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"{{.Module}}/internal/domain"
	"{{.Module}}/internal/handler/middleware"
	"{{.Module}}/pkg/apperror"
)

type {{.Name}}Handler struct {
	svc domain.{{.Name}}Service
}

func New{{.Name}}Handler(svc domain.{{.Name}}Service) *{{.Name}}Handler {
	return &{{.Name}}Handler{svc: svc}
}

func (h *{{.Name}}Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Create{{.Name}})
	r.Get("/", h.List{{.Name}}s)
	r.Get("/{id}", h.Get{{.Name}})
	r.Put("/{id}", h.Update{{.Name}})
	r.Delete("/{id}", h.Delete{{.Name}})
	return r
}

type create{{.Name}}Request struct {
{{- range .Fields}}
	{{title .Name}} {{.Type}} {{bt}}json:"{{.NameSnake}}"{{bt}}
{{- end}}
}

type {{.NameLower}}Response struct {
	ID        string {{bt}}json:"id"{{bt}}
{{- range .Fields}}
	{{title .Name}} {{.Type}} {{bt}}json:"{{.NameSnake}}"{{bt}}
{{- end}}
	CreatedAt string {{bt}}json:"created_at"{{bt}}
	UpdatedAt string {{bt}}json:"updated_at"{{bt}}
}

func to{{.Name}}Response(e *domain.{{.Name}}) *{{.NameLower}}Response {
	return &{{.NameLower}}Response{
		ID: e.ID.String(),
{{- range .Fields}}
		{{title .Name}}: e.{{title .Name}},
{{- end}}
		CreatedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: e.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func (h *{{.Name}}Handler) Create{{.Name}}(w http.ResponseWriter, r *http.Request) {
	var req create{{.Name}}Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid JSON body"))
		return
	}
	result, err := h.svc.Create{{.Name}}(r.Context(), domain.Create{{.Name}}Input{
{{- range .Fields}}
		{{title .Name}}: req.{{title .Name}},
{{- end}}
	})
	if err != nil {
		middleware.WriteError(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, to{{.Name}}Response(result))
}

func (h *{{.Name}}Handler) Get{{.Name}}(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid UUID"))
		return
	}
	result, err := h.svc.Get{{.Name}}(r.Context(), id)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, to{{.Name}}Response(result))
}

func (h *{{.Name}}Handler) Update{{.Name}}(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid UUID"))
		return
	}
	var req domain.Update{{.Name}}Input
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid JSON body"))
		return
	}
	result, err := h.svc.Update{{.Name}}(r.Context(), id, req)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, to{{.Name}}Response(result))
}

func (h *{{.Name}}Handler) Delete{{.Name}}(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid UUID"))
		return
	}
	if err := h.svc.Delete{{.Name}}(r.Context(), id); err != nil {
		middleware.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *{{.Name}}Handler) List{{.Name}}s(w http.ResponseWriter, r *http.Request) {
	results, total, err := h.svc.List{{.Name}}s(r.Context(), domain.DefaultPagination())
	if err != nil {
		middleware.WriteError(w, err)
		return
	}
	data := make([]*{{.NameLower}}Response, len(results))
	for i, e := range results {
		data[i] = to{{.Name}}Response(e)
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"data": data, "total": total})
}
`

const handlerTestTmpl = `// Code generated by axe on {{.Date}}. Customize freely.
package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"{{.Module}}/internal/domain"
	"{{.Module}}/internal/handler"
)

type mock{{.Name}}Svc struct{}

func (m *mock{{.Name}}Svc) Create{{.Name}}(_ context.Context, _ domain.Create{{.Name}}Input) (*domain.{{.Name}}, error) {
	return &domain.{{.Name}}{ID: uuid.New()}, nil
}
func (m *mock{{.Name}}Svc) Get{{.Name}}(_ context.Context, id uuid.UUID) (*domain.{{.Name}}, error) {
	return &domain.{{.Name}}{ID: id}, nil
}
func (m *mock{{.Name}}Svc) Update{{.Name}}(_ context.Context, id uuid.UUID, _ domain.Update{{.Name}}Input) (*domain.{{.Name}}, error) {
	return &domain.{{.Name}}{ID: id}, nil
}
func (m *mock{{.Name}}Svc) Delete{{.Name}}(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mock{{.Name}}Svc) List{{.Name}}s(_ context.Context, _ domain.Pagination) ([]*domain.{{.Name}}, int, error) {
	return []*domain.{{.Name}}{}, 0, nil
}

func setup{{.Name}}Router() *chi.Mux {
	h := handler.New{{.Name}}Handler(&mock{{.Name}}Svc{})
	r := chi.NewRouter()
	r.Mount("/api/v1/{{.NamePlural}}", h.Routes())
	return r
}

func Test{{.Name}}_Get_200(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/{{.NamePlural}}/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	setup{{.Name}}Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func Test{{.Name}}_Delete_204(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/{{.NamePlural}}/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	setup{{.Name}}Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}

func Test{{.Name}}_List_200(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/{{.NamePlural}}", nil)
	rec := httptest.NewRecorder()
	setup{{.Name}}Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
`

const serviceTmpl = `// Code generated by axe on {{.Date}}. Customize freely.
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"{{.Module}}/internal/domain"
	"{{.Module}}/pkg/apperror"
	"{{.Module}}/pkg/logger"
)

type {{.Name}}Service struct {
	repo domain.{{.Name}}Repository
}

func New{{.Name}}Service(repo domain.{{.Name}}Repository) domain.{{.Name}}Service {
	return &{{.Name}}Service{repo: repo}
}

func (s *{{.Name}}Service) Create{{.Name}}(ctx context.Context, input domain.Create{{.Name}}Input) (*domain.{{.Name}}, error) {
	result, err := s.repo.Create(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("{{.Name}}Service.Create: %w", err)
	}
	logger.FromCtx(ctx).Info("{{.NameLower}} created", "id", result.ID)
	return result, nil
}

func (s *{{.Name}}Service) Get{{.Name}}(ctx context.Context, id uuid.UUID) (*domain.{{.Name}}, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *{{.Name}}Service) Update{{.Name}}(ctx context.Context, id uuid.UUID, input domain.Update{{.Name}}Input) (*domain.{{.Name}}, error) {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, err
	}
	result, err := s.repo.Update(ctx, id, input)
	if err != nil {
		return nil, fmt.Errorf("{{.Name}}Service.Update: %w", err)
	}
	return result, nil
}

func (s *{{.Name}}Service) Delete{{.Name}}(ctx context.Context, id uuid.UUID) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id)
}

func (s *{{.Name}}Service) List{{.Name}}s(ctx context.Context, p domain.Pagination) ([]*domain.{{.Name}}, int, error) {
	if err := p.Validate(); err != nil {
		return nil, 0, apperror.ErrInvalidInput.WithMessage(err.Error())
	}
	return s.repo.List(ctx, p)
}
`

const serviceTestTmpl = `// Code generated by axe on {{.Date}}. Customize freely.
package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"{{.Module}}/internal/domain"
	"{{.Module}}/internal/service"
)

type mock{{.Name}}Repo struct{}

func (m *mock{{.Name}}Repo) Create(_ context.Context, _ domain.Create{{.Name}}Input) (*domain.{{.Name}}, error) {
	return &domain.{{.Name}}{ID: uuid.New()}, nil
}
func (m *mock{{.Name}}Repo) GetByID(_ context.Context, id uuid.UUID) (*domain.{{.Name}}, error) {
	return &domain.{{.Name}}{ID: id}, nil
}
func (m *mock{{.Name}}Repo) Update(_ context.Context, id uuid.UUID, _ domain.Update{{.Name}}Input) (*domain.{{.Name}}, error) {
	return &domain.{{.Name}}{ID: id}, nil
}
func (m *mock{{.Name}}Repo) Delete(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mock{{.Name}}Repo) List(_ context.Context, _ domain.Pagination) ([]*domain.{{.Name}}, int, error) {
	return []*domain.{{.Name}}{}, 0, nil
}

func TestCreate{{.Name}}_HappyPath(t *testing.T) {
	svc := service.New{{.Name}}Service(&mock{{.Name}}Repo{})
	result, err := svc.Create{{.Name}}(context.Background(), domain.Create{{.Name}}Input{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
}

func TestDelete{{.Name}}_HappyPath(t *testing.T) {
	svc := service.New{{.Name}}Service(&mock{{.Name}}Repo{})
	if err := svc.Delete{{.Name}}(context.Background(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
`

const repoTmpl = `// Code generated by axe on {{.Date}}.
// Scaffold — implement Ent calls after running: go generate ./ent/...
package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	ent "{{.Module}}/ent"
	"{{.Module}}/internal/domain"
	"{{.Module}}/pkg/apperror"
)

type {{.Name}}Repo struct {
	client *ent.Client
}

func New{{.Name}}Repo(client *ent.Client) domain.{{.Name}}Repository {
	return &{{.Name}}Repo{client: client}
}

func (r *{{.Name}}Repo) Create(ctx context.Context, input domain.Create{{.Name}}Input) (*domain.{{.Name}}, error) {
	return nil, fmt.Errorf("{{.Name}}Repo.Create: not implemented — run go generate ./ent/... first")
}

func (r *{{.Name}}Repo) GetByID(ctx context.Context, id uuid.UUID) (*domain.{{.Name}}, error) {
	return nil, apperror.ErrNotFound.WithMessage("{{.NameLower}} not found")
}

func (r *{{.Name}}Repo) Update(ctx context.Context, id uuid.UUID, _ domain.Update{{.Name}}Input) (*domain.{{.Name}}, error) {
	return nil, fmt.Errorf("{{.Name}}Repo.Update: not implemented")
}

func (r *{{.Name}}Repo) Delete(ctx context.Context, id uuid.UUID) error {
	return fmt.Errorf("{{.Name}}Repo.Delete: not implemented")
}

func (r *{{.Name}}Repo) List(ctx context.Context, p domain.Pagination) ([]*domain.{{.Name}}, int, error) {
	return []*domain.{{.Name}}{}, 0, nil
}
`

const entSchemaTmpl = `// Code generated by axe on {{.Date}}.
// After editing: go generate ./ent/...
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type {{.Name}} struct {
	ent.Schema
}

func ({{.Name}}) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
{{- range .Fields}}
		{{.EntType}}("{{.NameSnake}}"),
{{- end}}
{{- if .BelongsTo}}
		field.UUID("{{snake .BelongsTo}}_id", uuid.UUID{}),
{{- end}}
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func ({{.Name}}) Edges() []ent.Edge {
	return nil
}
`

const sqlQueryTmpl = `-- {{.Name}} queries — Generated: {{.Date}}

-- name: Get{{.Name}}ByID :one
SELECT id,{{range .Fields}} {{.NameSnake}},{{end}} created_at, updated_at
FROM {{.NamePlural}}
WHERE id = $1
LIMIT 1;

-- name: List{{.Name}}s :many
SELECT id,{{range .Fields}} {{.NameSnake}},{{end}} created_at, updated_at
FROM {{.NamePlural}}
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: Count{{.Name}}s :one
SELECT COUNT(*) FROM {{.NamePlural}};
`

const migrationTmpl = `-- Migration: Create {{.NamePlural}} table — Generated: {{.Date}}

CREATE TABLE IF NOT EXISTS {{.NamePlural}} (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
{{- range .Fields}}
    {{.NameSnake}}    {{.SQLType}}    NOT NULL,
{{- end}}
{{- if .BelongsTo}}
    {{snake .BelongsTo}}_id UUID NOT NULL REFERENCES {{snake .BelongsTo}}s(id),
{{- end}}
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_{{.NamePlural}}_created ON {{.NamePlural}}(created_at DESC);
`

const wsHandlerTmpl = `// Code generated by axe on {{.Date}}. Customize freely.
// WebSocket handler for the {{.Name}} resource.
package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"{{.Module}}/pkg/ws"
)

type {{.Name}}WSHandler struct {
	hub     *ws.Hub
	tracker *ws.UserConnTracker
}

func New{{.Name}}WSHandler(hub *ws.Hub, tracker *ws.UserConnTracker) *{{.Name}}WSHandler {
	return &{{.Name}}WSHandler{hub: hub, tracker: tracker}
}

func (h *{{.Name}}WSHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.Connect)
	return r
}

func (h *{{.Name}}WSHandler) Connect(w http.ResponseWriter, r *http.Request) {
	client, err := h.hub.UpgradeAuthenticated(w, r, h.tracker)
	if err != nil {
		return
	}
	const room = "{{.NameLower}}"
	h.hub.Join(client, room)
	// Use hub.Context() — NOT r.Context() which is canceled on HTTP handler return.
	hubCtx := h.hub.Context()
	client.OnMessage(func(msg []byte) {
		_ = h.hub.Broadcast(hubCtx, room, msg)
	})
	// Block until client disconnects.
	// nhooyr.io/websocket closes the connection when the handler goroutine returns.
	<-client.Done()
}
`

// ── pkg/ws templates ──────────────────────────────────────────────────────────

const wsPkgAdapterTmpl = `package ws

import "context"

type Adapter interface {
	Publish(ctx context.Context, channel string, msg []byte) error
	Subscribe(ctx context.Context, channel string, handler func(msg []byte)) error
	Close() error
}

type MemoryAdapter struct{}

func (MemoryAdapter) Publish(_ context.Context, _ string, _ []byte) error        { return nil }
func (MemoryAdapter) Subscribe(_ context.Context, _ string, _ func([]byte)) error { return nil }
func (MemoryAdapter) Close() error                                                 { return nil }
`

const wsPkgMetricsTmpl = `// Package ws provides a production-ready WebSocket hub.
package ws

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	wsActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "axe", Subsystem: "ws", Name: "active_connections",
		Help: "Number of currently active WebSocket connections.",
	})
	wsMessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "axe", Subsystem: "ws", Name: "messages_total",
		Help: "Total number of WebSocket messages handled by the hub.",
	}, []string{"direction"})
	wsRoomsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "axe", Subsystem: "ws", Name: "rooms_active",
		Help: "Number of currently active WebSocket rooms.",
	})
	wsConnectRejectedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "axe", Subsystem: "ws", Name: "connect_rejected_total",
		Help: "Total number of rejected WebSocket upgrade attempts.",
	})
)
`

const wsPkgRoomTmpl = `package ws

import "sync"

type Room struct {
	id      string
	clients map[string]*Client
	mu      sync.RWMutex
}

func newRoom(id string) *Room { return &Room{id: id, clients: make(map[string]*Client)} }

func (r *Room) add(c *Client)          { r.mu.Lock(); r.clients[c.id] = c; r.mu.Unlock() }
func (r *Room) remove(clientID string) { r.mu.Lock(); delete(r.clients, clientID); r.mu.Unlock() }
func (r *Room) broadcast(msg []byte) {
	r.mu.RLock(); defer r.mu.RUnlock()
	for _, c := range r.clients { c.send(msg) }
}
func (r *Room) Presence() []string {
	r.mu.RLock(); defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.clients))
	for id := range r.clients { ids = append(ids, id) }
	return ids
}
func (r *Room) Size() int          { r.mu.RLock(); defer r.mu.RUnlock(); return len(r.clients) }
func (r *Room) isEmpty() bool      { r.mu.RLock(); defer r.mu.RUnlock(); return len(r.clients) == 0 }
`

const wsPkgClientTmpl = `package ws

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

const (
	sendBufSize  = 256
	writeTimeout = 10 * time.Second
	pingInterval = 30 * time.Second
)

type MessageHandler func(msg []byte)

type Client struct {
	id        string
	UserID    string
	conn      *websocket.Conn
	hub       *Hub
	sendCh    chan []byte
	done      chan struct{} // closed when readPump exits; signals handler goroutine to return
	onMessage MessageHandler
	rooms     map[string]struct{}
	mu        sync.RWMutex
	log       *slog.Logger
}

func newClient(conn *websocket.Conn, hub *Hub) *Client { return newClientWithMeta(conn, hub, "") }

func newClientWithMeta(conn *websocket.Conn, hub *Hub, userID string) *Client {
	return &Client{
		id: uuid.New().String(), UserID: userID,
		conn: conn, hub: hub,
		sendCh: make(chan []byte, sendBufSize),
		done:   make(chan struct{}),
		rooms:  make(map[string]struct{}),
		log:    hub.log,
	}
}

func (c *Client) ID() string { return c.id }

func (c *Client) OnMessage(fn MessageHandler) { c.mu.Lock(); c.onMessage = fn; c.mu.Unlock() }

func (c *Client) send(msg []byte) {
	select {
	case c.sendCh <- msg:
		wsMessagesTotal.WithLabelValues("outbound").Inc()
	default:
		c.log.Warn("ws: send buffer full", "client_id", c.id)
	}
}

func (c *Client) Close() { close(c.sendCh) }

// Done returns a channel closed when the client disconnects.
// HTTP handlers must block on <-client.Done() to keep the connection alive:
// nhooyr.io/websocket closes the WS when the handler goroutine returns.
func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) readPump(ctx context.Context) {
	defer func() {
		close(c.done)       // unblock handler goroutine
		c.hub.unregister <- c
	}()
	for {
		_, msg, err := c.conn.Read(ctx)
		if err != nil {
			if ctx.Err() == nil { c.log.Debug("ws: read error", "client_id", c.id, "error", err) }
			return
		}
		wsMessagesTotal.WithLabelValues("inbound").Inc()
		c.mu.RLock(); h := c.onMessage; c.mu.RUnlock()
		if h != nil { h(msg) }
	}
}

func (c *Client) writePump(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-c.sendCh:
			if !ok { _ = c.conn.Close(websocket.StatusNormalClosure, ""); return }
			wCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Write(wCtx, websocket.MessageText, msg); cancel()
			if err != nil { c.log.Debug("ws: write error", "client_id", c.id, "error", err); return }
		case <-ticker.C:
			pCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Ping(pCtx); cancel()
			if err != nil { c.log.Debug("ws: ping error", "client_id", c.id, "error", err); return }
		case <-ctx.Done():
			return
		}
	}
}

func (c *Client) joinRoom(id string)  { c.mu.Lock(); c.rooms[id] = struct{}{}; c.mu.Unlock() }
func (c *Client) leaveRoom(id string) { c.mu.Lock(); delete(c.rooms, id); c.mu.Unlock() }
func (c *Client) allRooms() []string {
	c.mu.RLock(); defer c.mu.RUnlock()
	r := make([]string, 0, len(c.rooms))
	for id := range c.rooms { r = append(r, id) }
	return r
}
func (c *Client) String() string { return fmt.Sprintf("Client(%s)", c.id) }
`

const wsPkgHubTmpl = `package ws

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"nhooyr.io/websocket"
)

type Hub struct {
	clients    map[string]*Client
	rooms      map[string]*Room
	mu         sync.RWMutex
	register   chan *Client
	unregister chan *Client
	adapter    Adapter
	ctx        context.Context
	cancel     context.CancelFunc
	log        *slog.Logger
}

type HubOption func(*Hub)

func WithAdapter(a Adapter) HubOption   { return func(h *Hub) { h.adapter = a } }
func WithLogger(l *slog.Logger) HubOption { return func(h *Hub) { h.log = l } }

func NewHub(opts ...HubOption) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{
		clients: make(map[string]*Client), rooms: make(map[string]*Room),
		register: make(chan *Client, 32), unregister: make(chan *Client, 32),
		adapter: MemoryAdapter{}, ctx: ctx, cancel: cancel, log: slog.Default(),
	}
	for _, o := range opts { o(h) }
	return h
}

func (h *Hub) Run(ctx context.Context) {
	go func() { <-ctx.Done(); h.cancel() }()
	for {
		select {
		case c := <-h.register:
			h.mu.Lock(); h.clients[c.id] = c; h.mu.Unlock()
			wsActiveConnections.Inc()
			h.log.Info("ws: client connected", "client_id", c.id)
		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c.id]; ok {
				for _, rid := range c.allRooms() {
					if room, ok := h.rooms[rid]; ok {
						room.remove(c.id)
						if room.isEmpty() { delete(h.rooms, rid); wsRoomsActive.Dec() }
					}
				}
				delete(h.clients, c.id); wsActiveConnections.Dec()
				h.log.Info("ws: client disconnected", "client_id", c.id)
			}
			h.mu.Unlock()
		case <-h.ctx.Done():
			h.mu.Lock()
			for _, c := range h.clients { c.Close() }
			h.clients = make(map[string]*Client); h.rooms = make(map[string]*Room)
			h.mu.Unlock()
			_ = h.adapter.Close()
			return
		}
	}
}

func (h *Hub) Upgrade(w http.ResponseWriter, r *http.Request) (*Client, error) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil { wsConnectRejectedTotal.Inc(); return nil, fmt.Errorf("ws: upgrade: %w", err) }
	c := newClient(conn, h); h.register <- c
	ctx, cancel := context.WithCancel(h.ctx)
	go func() { defer cancel(); c.writePump(ctx) }()
	go c.readPump(ctx)
	return c, nil
}

func (h *Hub) UpgradeAuthenticated(w http.ResponseWriter, r *http.Request, tracker *UserConnTracker) (*Client, error) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil { wsConnectRejectedTotal.Inc(); return nil, fmt.Errorf("ws: upgrade: %w", err) }
	userID := ""
	if claims := ClaimsFromCtx(r.Context()); claims != nil { userID = claims.UserID }
	c := newClientWithMeta(conn, h, userID); h.register <- c
	ctx, cancel := context.WithCancel(h.ctx)
	go func() { defer cancel(); c.writePump(ctx) }()
	go func() {
		c.readPump(ctx)
		if tracker != nil && userID != "" { tracker.Release(userID) }
	}()
	return c, nil
}

func (h *Hub) Join(client *Client, roomID string) {
	h.mu.Lock()
	room, exists := h.rooms[roomID]
	if !exists {
		room = newRoom(roomID); h.rooms[roomID] = room; wsRoomsActive.Inc()
		_ = h.adapter.Subscribe(h.ctx, roomID, func(msg []byte) {
			h.mu.RLock(); r, ok := h.rooms[roomID]; h.mu.RUnlock()
			if ok { r.broadcast(msg) }
		})
	}
	room.add(client); h.mu.Unlock()
	client.joinRoom(roomID)
}

func (h *Hub) Leave(client *Client, roomID string) {
	h.mu.Lock()
	if room, ok := h.rooms[roomID]; ok {
		room.remove(client.id)
		if room.isEmpty() { delete(h.rooms, roomID); wsRoomsActive.Dec() }
	}
	h.mu.Unlock(); client.leaveRoom(roomID)
}

func (h *Hub) Broadcast(ctx context.Context, roomID string, msg []byte) error {
	h.mu.RLock(); room, ok := h.rooms[roomID]; h.mu.RUnlock()
	if ok { room.broadcast(msg) }
	return h.adapter.Publish(ctx, roomID, msg)
}

func (h *Hub) Presence(roomID string) []string {
	h.mu.RLock(); room, ok := h.rooms[roomID]; h.mu.RUnlock()
	if !ok { return nil }
	return room.Presence()
}

func (h *Hub) ClientCount() int { h.mu.RLock(); defer h.mu.RUnlock(); return len(h.clients) }
func (h *Hub) Shutdown()        { h.cancel() }

// Context returns the hub's lifecycle context. Use this in OnMessage callbacks
// instead of the HTTP request context (which is canceled on handler return).
func (h *Hub) Context() context.Context { return h.ctx }

// RoomSize returns the number of clients currently in the given room.
func (h *Hub) RoomSize(roomID string) int {
	h.mu.RLock(); room, ok := h.rooms[roomID]; h.mu.RUnlock()
	if !ok { return 0 }
	return room.Size()
}
`

const wsPkgAuthTmpl = `package ws

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"{{.Module}}/pkg/jwtauth"
	"{{.Module}}/pkg/logger"
)

type wsContextKey string

const wsClaimsKey wsContextKey = "ws_jwt_claims"

type WSBlocklist interface {
	IsTokenBlocked(ctx context.Context, jti string) (bool, error)
}

func ClaimsFromCtx(ctx context.Context) *jwtauth.Claims {
	v, _ := ctx.Value(wsClaimsKey).(*jwtauth.Claims)
	return v
}

type authOptions struct{ maxConns int }

type AuthOption func(*authOptions)

func WithMaxConnsPerUser(n int) AuthOption {
	return func(o *authOptions) {
		if n > 0 { o.maxConns = n }
	}
}

type UserConnTracker struct{ m sync.Map }

func NewUserConnTracker() *UserConnTracker { return &UserConnTracker{} }

func (t *UserConnTracker) Acquire(userID string, max int) bool {
	actual, _ := t.m.LoadOrStore(userID, new(int64))
	counter := actual.(*int64)
	for {
		cur := atomic.LoadInt64(counter)
		if cur >= int64(max) { return false }
		if atomic.CompareAndSwapInt64(counter, cur, cur+1) { return true }
	}
}

func (t *UserConnTracker) Release(userID string) {
	if v, ok := t.m.Load(userID); ok {
		if atomic.AddInt64(v.(*int64), -1) < 0 { atomic.StoreInt64(v.(*int64), 0) }
	}
}

func (t *UserConnTracker) Count(userID string) int64 {
	v, ok := t.m.Load(userID)
	if !ok { return 0 }
	return atomic.LoadInt64(v.(*int64))
}

func WSAuth(svc *jwtauth.Service, blocklist WSBlocklist, tracker *UserConnTracker, opts ...AuthOption) func(http.Handler) http.Handler {
	options := &authOptions{maxConns: 5}
	for _, o := range opts { o(options) }
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.FromCtx(r.Context())
			token := extractWSToken(r)
			if token == "" {
				log.Info("ws auth: missing token", "remote", r.RemoteAddr)
				http.Error(w, "missing token", http.StatusUnauthorized)
				wsConnectRejectedTotal.Inc()
				return
			}
			claims, err := svc.Validate(token)
			if err != nil {
				log.Info("ws auth: invalid token", "remote", r.RemoteAddr, "error", err)
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				wsConnectRejectedTotal.Inc()
				return
			}
			if blocklist != nil && claims.JTI() != "" {
				blocked, blErr := blocklist.IsTokenBlocked(r.Context(), claims.JTI())
				if blErr != nil {
					log.Warn("ws auth: blocklist check failed", "error", blErr)
				} else if blocked {
					http.Error(w, "token revoked", http.StatusUnauthorized)
					wsConnectRejectedTotal.Inc()
					return
				}
			}
			if tracker != nil && !tracker.Acquire(claims.UserID, options.maxConns) {
				http.Error(w, "too many connections", http.StatusTooManyRequests)
				wsConnectRejectedTotal.Inc()
				return
			}
			ctx := context.WithValue(r.Context(), wsClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractWSToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if parts := strings.SplitN(h, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			if t := strings.TrimSpace(parts[1]); t != "" { return t }
		}
	}
	return r.URL.Query().Get("token")
}
`

const wsPkgRedisAdapterTmpl = `package ws

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

type RedisAdapter struct {
	rdb     *redis.Client
	prefix  string
	log     *slog.Logger
	pubsubs map[string]*redis.PubSub
}

func NewRedisAdapter(rdb *redis.Client, opts ...func(*RedisAdapter)) *RedisAdapter {
	a := &RedisAdapter{rdb: rdb, prefix: "axe:ws:", log: slog.Default(), pubsubs: make(map[string]*redis.PubSub)}
	for _, o := range opts { o(a) }
	return a
}

func WithRedisPrefix(p string) func(*RedisAdapter) { return func(a *RedisAdapter) { a.prefix = p } }
func WithRedisLogger(l *slog.Logger) func(*RedisAdapter) { return func(a *RedisAdapter) { a.log = l } }
func (a *RedisAdapter) channel(ch string) string { return a.prefix + ch }

func (a *RedisAdapter) Publish(ctx context.Context, channel string, msg []byte) error {
	return a.rdb.Publish(ctx, a.channel(channel), msg).Err()
}

func (a *RedisAdapter) Subscribe(ctx context.Context, channel string, handler func([]byte)) error {
	ch := a.channel(channel)
	ps := a.rdb.Subscribe(ctx, ch)
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return fmt.Errorf("ws/redis: subscribe to %q: %w", channel, err)
	}
	a.pubsubs[channel] = ps
	go func() {
		defer ps.Close() //nolint:errcheck
		for msg := range ps.Channel() { handler([]byte(msg.Payload)) }
	}()
	a.log.Info("ws/redis: subscribed", "channel", ch)
	return nil
}

func (a *RedisAdapter) Close() error {
	for ch, ps := range a.pubsubs {
		_ = ps.Unsubscribe(context.Background(), a.channel(ch))
		_ = ps.Close()
	}
	return nil
}
`

// ── migrate ───────────────────────────────────────────────────────────────────

func migrateCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "migrate", Short: "Database migration commands"}
	cmd.AddCommand(migrateCreateCmd(), migrateUpCmd(), migrateDownCmd(), migrateStatusCmd())
	return cmd
}

func migrateCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new timestamped migration file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(strings.ReplaceAll(args[0], " ", "_"))
			ts := time.Now().Format("20060102150405")
			filename := fmt.Sprintf("%s_%s.sql", ts, name)
			path := filepath.Join(migrationsDir, filename)
			content := fmt.Sprintf("-- Migration: %s\n-- Created: %s\n\n-- +migrate Up\n\n\n-- +migrate Down\n\n",
				filename, time.Now().Format("2006-01-02"))
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return fmt.Errorf("create migration: %w", err)
			}
			fmt.Printf("✅ Created migration: %s\n", path)
			return nil
		},
	}
}

func migrateUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			conn, err := openDB(ctx)
			if err != nil {
				return err
			}
			defer conn.Close(ctx)
			if err := ensureMigrationsTable(ctx, conn); err != nil {
				return err
			}
			files, err := pendingMigrations(ctx, conn)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				fmt.Println("✅ No pending migrations.")
				return nil
			}
			for _, f := range files {
				if err := applyMigration(ctx, conn, f); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func migrateDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Remove the last applied migration record (does not reverse SQL)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			conn, err := openDB(ctx)
			if err != nil {
				return err
			}
			defer conn.Close(ctx)
			if err := ensureMigrationsTable(ctx, conn); err != nil {
				return err
			}
			var last string
			err = conn.QueryRow(ctx, `SELECT filename FROM schema_migrations ORDER BY applied_at DESC LIMIT 1`).Scan(&last)
			if err == pgx.ErrNoRows {
				fmt.Println("⚠️  No applied migrations to roll back.")
				return nil
			}
			if err != nil {
				return fmt.Errorf("query last migration: %w", err)
			}
			_, err = conn.Exec(ctx, `DELETE FROM schema_migrations WHERE filename = $1`, last)
			if err != nil {
				return fmt.Errorf("remove migration record: %w", err)
			}
			fmt.Printf("↩️  Removed migration record: %s\n   SQL was NOT reversed. Write a new migration or run manually.\n", last)
			return nil
		},
	}
}

func migrateStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			conn, err := openDB(ctx)
			if err != nil {
				return err
			}
			defer conn.Close(ctx)
			if err := ensureMigrationsTable(ctx, conn); err != nil {
				return err
			}
			applied, err := appliedMigrations(ctx, conn)
			if err != nil {
				return err
			}
			files, err := sqlFiles()
			if err != nil {
				return err
			}
			fmt.Printf("%-55s  %s\n", "MIGRATION", "STATUS")
			fmt.Println(strings.Repeat("─", 70))
			for _, f := range files {
				status := "pending"
				if _, ok := applied[f]; ok {
					status = "applied"
				}
				fmt.Printf("%-55s  %s\n", f, status)
			}
			return nil
		},
	}
}

// ── DB helpers ────────────────────────────────────────────────────────────────

func openDB(ctx context.Context) (*pgx.Conn, error) {
	loadDotEnv(".env")
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set (check your .env file)")
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return conn, nil
}

func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if idx := strings.Index(v, " #"); idx != -1 {
			v = strings.TrimSpace(v[:idx])
		}
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}

func ensureMigrationsTable(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}
	return nil
}

func appliedMigrations(ctx context.Context, conn *pgx.Conn) (map[string]struct{}, error) {
	rows, err := conn.Query(ctx, `SELECT filename FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()
	applied := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		applied[name] = struct{}{}
	}
	return applied, rows.Err()
}

func sqlFiles() ([]string, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", migrationsDir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

func pendingMigrations(ctx context.Context, conn *pgx.Conn) ([]string, error) {
	applied, err := appliedMigrations(ctx, conn)
	if err != nil {
		return nil, err
	}
	all, err := sqlFiles()
	if err != nil {
		return nil, err
	}
	var pending []string
	for _, f := range all {
		if _, ok := applied[f]; !ok {
			pending = append(pending, f)
		}
	}
	return pending, nil
}

func applyMigration(ctx context.Context, conn *pgx.Conn, filename string) error {
	path := filepath.Join(migrationsDir, filename)
	sql, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", filename, err)
	}
	fmt.Printf("→ Applying %s ... ", filename)
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return fmt.Errorf("\n  ❌ migration %s failed: %w", filename, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, filename); err != nil {
		return fmt.Errorf("record migration %s: %w", filename, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", filename, err)
	}
	fmt.Println("✅")
	return nil
}
