package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// User holds the schema definition for the User entity.
// This is the reference implementation — all other schemas follow this pattern.
type User struct {
	ent.Schema
}

// Fields defines the User entity fields.
func (User) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable().
			Comment("Primary key — UUID v4, generated at creation"),

		field.String("email").
			Unique().
			NotEmpty().
			MaxLen(255).
			Comment("User email address — used for login"),

		field.String("name").
			NotEmpty().
			MaxLen(255).
			Comment("Display name"),

		field.String("password_hash").
			NotEmpty().
			Sensitive(). // excluded from JSON serialization
			Comment("bcrypt password hash — never expose in API responses"),

		field.Enum("role").
			Values("user", "admin").
			Default("user").
			Comment("User role for RBAC"),

		field.Bool("active").
			Default(true).
			Comment("Soft-delete flag — false means deactivated"),

		field.Time("created_at").
			Default(time.Now).
			Immutable().
			Comment("Record creation timestamp"),

		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			Comment("Last update timestamp"),
	}
}

// Edges defines the User entity relationships.
func (User) Edges() []ent.Edge {
	return nil // edges added in future epics (e.g. orders, sessions)
}

// Indexes defines the User entity indexes.
func (User) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("email"),             // already unique, explicit for clarity
		index.Fields("active", "created_at"), // pagination queries
	}
}
