package config

import (
	"strings"
	"testing"
)

func TestLoadAllowsProductionAndPreviewSchemas(t *testing.T) {
	for _, schema := range []string{"public", "pr_42"} {
		t.Run(schema, func(t *testing.T) {
			t.Setenv("DATABASE_USER", "profile_user")
			t.Setenv("DATABASE_PASSWORD", "password")
			t.Setenv("DATABASE_NAME", "profile_db")
			t.Setenv("DATABASE_SCHEMA", schema)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Database.Schema != schema {
				t.Fatalf("schema = %q, want %q", cfg.Database.Schema, schema)
			}
			if !strings.Contains(cfg.Database.URL(), "search_path="+schema) {
				t.Fatalf("database URL does not include schema: %s", cfg.Database.URL())
			}
		})
	}
}

func TestLoadRejectsUnsafeDatabaseSchema(t *testing.T) {
	t.Setenv("DATABASE_USER", "profile_user")
	t.Setenv("DATABASE_PASSWORD", "password")
	t.Setenv("DATABASE_NAME", "profile_db")
	t.Setenv("DATABASE_SCHEMA", "public,evil")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_SCHEMA") {
		t.Fatalf("Load() error = %v, want DATABASE_SCHEMA validation error", err)
	}
}

func TestLoadAllowsOnlyPreviewCORSOrigins(t *testing.T) {
	t.Setenv("DATABASE_USER", "profile_user")
	t.Setenv("DATABASE_PASSWORD", "password")
	t.Setenv("DATABASE_NAME", "profile_db")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://pr-42.preview.seebyte.xyz")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.CORSAllowedOrigins; len(got) != 1 || got[0] != "https://pr-42.preview.seebyte.xyz" {
		t.Fatalf("CORSAllowedOrigins = %#v", got)
	}
}

func TestLoadRejectsNonPreviewCORSOrigin(t *testing.T) {
	t.Setenv("DATABASE_USER", "profile_user")
	t.Setenv("DATABASE_PASSWORD", "password")
	t.Setenv("DATABASE_NAME", "profile_db")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://full-stack.seebyte.xyz")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS") {
		t.Fatalf("Load() error = %v, want CORS_ALLOWED_ORIGINS validation error", err)
	}
}
