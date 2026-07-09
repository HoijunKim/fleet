package pgstore

import (
	"context"
	"os"
	"testing"
)

// testPg connects to DATABASE_URL_TEST, migrates, and truncates all tables.
// It skips the test when the env var is unset so `go test` is green offline.
func testPg(t *testing.T) *Pg {
	t.Helper()
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("DATABASE_URL_TEST not set; skipping Postgres-backed test")
	}
	if err := Migrate(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pg, err := New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := pg.pool.Exec(context.Background(),
		`TRUNCATE documents, user_versions, refresh_tokens, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(pg.Close)
	return pg
}
