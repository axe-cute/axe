package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── toTitle ──────────────────────────────────────────────────────────────────

func TestToTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"post", "Post"},
		{"Post", "Post"},
		{"comment", "Comment"},
		{"a", "A"},
		{"", ""},
		{"blogPost", "BlogPost"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, toTitle(tc.input))
		})
	}
}

// ── toCamel ──────────────────────────────────────────────────────────────────

func TestToCamel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"post", "post"},
		{"Post", "post"},
		{"blog_post", "blogPost"},
		{"author_id", "authorId"},
		{"a", "a"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, toCamel(tc.input))
		})
	}
}

// ── toSnake ──────────────────────────────────────────────────────────────────

func TestToSnake(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Post", "post"},
		{"BlogPost", "blog_post"},
		{"authorID", "author_i_d"},
		{"post", "post"},
		{"A", "a"},
		{"ABTest", "a_b_test"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, toSnake(tc.input))
		})
	}
}

// ── parseFields ──────────────────────────────────────────────────────────────

func TestParseFields_HappyPath(t *testing.T) {
	fields, err := parseFields("title:string,body:text,published:bool,views:int")
	require.NoError(t, err)
	require.Len(t, fields, 4)

	assert.Equal(t, "title", fields[0].Name)
	assert.Equal(t, "string", fields[0].Type)
	assert.Equal(t, "VARCHAR(255)", fields[0].SQLType)
	assert.Equal(t, "field.String", fields[0].EntType)

	assert.Equal(t, "body", fields[1].Name)
	assert.Equal(t, "string", fields[1].Type)
	assert.Equal(t, "TEXT", fields[1].SQLType)

	assert.Equal(t, "published", fields[2].Name)
	assert.Equal(t, "bool", fields[2].Type)

	assert.Equal(t, "views", fields[3].Name)
	assert.Equal(t, "int64", fields[3].Type)
}

func TestParseFields_AllTypes(t *testing.T) {
	typeTests := []struct {
		input   string
		goType  string
		sqlType string
	}{
		{"f:string", "string", "VARCHAR(255)"},
		{"f:str", "string", "VARCHAR(255)"},
		{"f:text", "string", "TEXT"},
		{"f:int", "int64", "BIGINT"},
		{"f:integer", "int64", "BIGINT"},
		{"f:int64", "int64", "BIGINT"},
		{"f:float", "float64", "DECIMAL(18,2)"},
		{"f:float64", "float64", "DECIMAL(18,2)"},
		{"f:decimal", "float64", "DECIMAL(18,2)"},
		{"f:bool", "bool", "BOOLEAN"},
		{"f:boolean", "bool", "BOOLEAN"},
		{"f:uuid", "uuid.UUID", "UUID"},
		{"f:time", "time.Time", "TIMESTAMPTZ"},
		{"f:datetime", "time.Time", "TIMESTAMPTZ"},
		{"f:timestamp", "time.Time", "TIMESTAMPTZ"},
	}
	for _, tc := range typeTests {
		t.Run(tc.input, func(t *testing.T) {
			fields, err := parseFields(tc.input)
			require.NoError(t, err)
			require.Len(t, fields, 1)
			assert.Equal(t, tc.goType, fields[0].Type)
			assert.Equal(t, tc.sqlType, fields[0].SQLType)
		})
	}
}

func TestParseFields_Empty(t *testing.T) {
	fields, err := parseFields("")
	require.NoError(t, err)
	assert.Nil(t, fields)
}

func TestParseFields_InvalidFormat(t *testing.T) {
	_, err := parseFields("just_a_name")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected name:type")
}

func TestParseFields_UnsupportedType(t *testing.T) {
	_, err := parseFields("data:blob")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported field type")
}

func TestParseFields_Whitespace(t *testing.T) {
	fields, err := parseFields("  title : string , body : text  ")
	require.NoError(t, err)
	require.Len(t, fields, 2)
	assert.Equal(t, "title", fields[0].Name)
	assert.Equal(t, "body", fields[1].Name)
}

func TestParseFields_SnakeCaseNames(t *testing.T) {
	fields, err := parseFields("author_id:uuid,created_date:time")
	require.NoError(t, err)
	require.Len(t, fields, 2)
	assert.Equal(t, "authorId", fields[0].Name)
	assert.Equal(t, "author_id", fields[0].NameSnake)
	assert.Equal(t, "createdDate", fields[1].Name)
}

// ── buildField ───────────────────────────────────────────────────────────────

func TestBuildField_NullValues(t *testing.T) {
	tests := []struct {
		typ       string
		nullValue string
	}{
		{"string", `""`},
		{"int", "0"},
		{"float", "0"},
		{"bool", "false"},
		{"uuid", "uuid.Nil"},
		{"time", "time.Time{}"},
	}
	for _, tc := range tests {
		t.Run(tc.typ, func(t *testing.T) {
			f, err := buildField("test", tc.typ)
			require.NoError(t, err)
			assert.Equal(t, tc.nullValue, f.NullValue)
		})
	}
}

func TestBuildField_JSONTag(t *testing.T) {
	f, err := buildField("author_id", "uuid")
	require.NoError(t, err)
	assert.Equal(t, "author_id", f.JSONTag)
}

// ── validateResourceName ─────────────────────────────────────────────────────

func TestValidateResourceName_Reserved(t *testing.T) {
	reserved := []string{
		"Client", "Config", "Query", "Tx", "Hook", "Policy",
		"Type", "Func", "Var", "Const", "Package",
		"Map", "Chan", "Range", "Select", "Switch",
		"Error", "String", "Int", "Bool", "Float",
	}
	for _, name := range reserved {
		t.Run(name, func(t *testing.T) {
			err := validateResourceName(name)
			assert.Error(t, err, "%q should be reserved", name)
			assert.Contains(t, err.Error(), "reserved")
		})
	}
}

func TestValidateResourceName_Valid(t *testing.T) {
	valid := []string{
		"Post", "Comment", "Order", "Article", "BlogPost",
		"Setting", "AppConfig", "UserProfile",
	}
	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			err := validateResourceName(name)
			assert.NoError(t, err, "%q should be allowed", name)
		})
	}
}

func TestValidateResourceName_CaseInsensitive(t *testing.T) {
	// "client" and "CLIENT" and "Client" are all reserved.
	for _, name := range []string{"client", "CLIENT", "Client"} {
		err := validateResourceName(name)
		assert.Error(t, err, "%q should be reserved (case insensitive)", name)
	}
}

// ── newResourceData ──────────────────────────────────────────────────────────

func TestNewResourceData_Pluralization(t *testing.T) {
	// Note: newResourceData calls readModuleName() which reads go.mod.
	// In the test directory there is a go.mod, so this should work.
	data := newResourceData("Post", nil, "", false, false, false)
	assert.Equal(t, "Post", data.Name)
	assert.Equal(t, "post", data.NameLower)
	assert.Equal(t, "posts", data.NamePlural)
	assert.Equal(t, "post", data.NameSnake)
}

func TestNewResourceData_PluralEndingInS(t *testing.T) {
	data := newResourceData("Address", nil, "", false, false, false)
	assert.Equal(t, "addresses", data.NamePlural)
}

func TestNewResourceData_AdminOnlyImpliesAuth(t *testing.T) {
	data := newResourceData("Secret", nil, "", false, true, false)
	assert.True(t, data.WithAuth, "--admin-only should imply --with-auth")
	assert.True(t, data.AdminOnly)
}

func TestNewResourceData_WithWSFlag(t *testing.T) {
	data := newResourceData("Chat", nil, "", false, false, true)
	assert.True(t, data.WithWS)
}

// ── bt helper ────────────────────────────────────────────────────────────────

func TestBt_ReturnsBacktick(t *testing.T) {
	assert.Equal(t, "`", bt())
}
