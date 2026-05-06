package new

import (
	"strings"
	"testing"
)

// E6 — every cross-service URL/port boundary in the generated compose
// file must carry a comment explaining that container-to-container
// traffic uses the SERVICE NAME, not localhost. The webtoon build
// burned ~20 min on this; the regression test below pins the cure.

func TestTmplDockerCompose_PostgresE6Comments(t *testing.T) {
	data := TemplateData{Name: "demo", WithCache: true, WithWorker: true}
	out := tmplDockerCompose(data, dbConfigs["postgres"])

	wantSubstrings := []string{
		// Header explains the hostname rule once, prominently.
		"docker-compose.yml — local development services for demo",
		"Service names below (postgres, redis, asynqmon) double as DNS",
		"localhost:<host-port>",
		"<service-name>:<container-port>",

		// Every port mapping carries a comment about host vs sibling.
		`"5432:5432"`,
		"sibling containers MUST use \"postgres:5432\"",
		`"6379:6379"`,
		`siblings use "redis:6379"`,
		`"8081:8080"`,

		// asynqmon's REDIS_ADDR carries an inline reminder, since this
		// is the most common place developers paste "localhost:6379".
		"REDIS_ADDR: redis:6379",
		`"redis" = service-name DNS inside the compose network`,

		// Volumes block tells the reader how to nuke local DB.
		"docker compose down -v",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(out, s) {
			t.Errorf("compose output missing %q\n--- output ---\n%s", s, out)
		}
	}
}

func TestTmplDockerCompose_MysqlE6Comments(t *testing.T) {
	data := TemplateData{Name: "demo", WithCache: true, WithWorker: true}
	out := tmplDockerCompose(data, dbConfigs["mysql"])

	for _, s := range []string{
		"Service names below (mysql, redis, asynqmon) double as DNS",
		`"3306:3306"`,
		`siblings use`,
		`"mysql:3306"`,
	} {
		if !strings.Contains(out, s) {
			t.Errorf("mysql compose missing %q", s)
		}
	}
}

func TestTmplDockerCompose_SqliteMinimal_NoHeaderNoise(t *testing.T) {
	// SQLite + no cache + no worker = no services → keep the file
	// minimal. Adding the DNS-rule header here would be misleading.
	data := TemplateData{Name: "demo", WithCache: false, WithWorker: false}
	out := tmplDockerCompose(data, dbConfigs["sqlite"])

	if !strings.Contains(out, "services: {}") {
		t.Errorf("sqlite minimal must emit empty services map, got:\n%s", out)
	}
	if strings.Contains(out, "ECONNREFUSED") {
		t.Errorf("sqlite minimal should not carry the DNS-rule header (no services to confuse)")
	}
}

func TestTmplDockerCompose_SqlitePlusRedis_StillExplainsDNS(t *testing.T) {
	// SQLite + cache → only redis in compose. The header should still
	// fire so the reader knows the hostname rule applies to redis too.
	data := TemplateData{Name: "demo", WithCache: true, WithWorker: false}
	out := tmplDockerCompose(data, dbConfigs["sqlite"])

	if !strings.Contains(out, "Service names below (redis)") {
		t.Errorf("sqlite+redis should list 'redis' in the header, got:\n%s", out)
	}
}

func TestTmplEnvExample_DocumentsHostVsServiceName(t *testing.T) {
	data := TemplateData{Name: "demo", WithCache: true}
	out := tmplEnvExample(data, dbConfigs["postgres"])

	for _, s := range []string{
		`host process`,
		`host = "localhost"`,
		`compose container`,
		`host = service name`,
		"See docs/docker.md",
		"# Redis — same host/service-name rule applies",
	} {
		if !strings.Contains(out, s) {
			t.Errorf(".env.example missing %q\n--- output ---\n%s", s, out)
		}
	}
}

func TestTmplDockerDocs_CoversE6AndE12(t *testing.T) {
	// The doc must answer two questions:
	//   1. Why my container can't reach the DB (E6).
	//   2. Why I keep rebuilding the image when only an env var changes (E12).
	for _, s := range []string{
		// E6 anchors
		"# Docker for {{.Name}}",
		"`localhost` vs the service name",
		"`localhost:5432`",
		"`postgres:5432`",
		"dial tcp 127.0.0.1:5432",
		// E12 anchors
		"Build-time vs runtime config",
		"`ARG`",
		"`environment:`",
		// Common-commands
		"docker compose down -v",
	} {
		if !strings.Contains(tmplDockerDocs, s) {
			t.Errorf("docker.md missing %q", s)
		}
	}
}

func TestDockerServiceList(t *testing.T) {
	cases := []struct {
		name string
		data TemplateData
		dbc  dbConfig
		want string
	}{
		{"pg+cache+worker", TemplateData{WithCache: true, WithWorker: true}, dbConfigs["postgres"], "postgres, redis, asynqmon"},
		{"mysql_only", TemplateData{}, dbConfigs["mysql"], "mysql"},
		{"sqlite+cache", TemplateData{WithCache: true}, dbConfigs["sqlite"], "redis"},
		{"sqlite_minimal", TemplateData{}, dbConfigs["sqlite"], ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dockerServiceList(tc.data, tc.dbc); got != tc.want {
				t.Errorf("dockerServiceList = %q, want %q", got, tc.want)
			}
		})
	}
}
