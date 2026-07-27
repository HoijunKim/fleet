package pgstore

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestApplyPoolConfig checks that only the non-zero PoolConfig fields override
// pgx's parsed defaults - no DB needed.
func TestApplyPoolConfig(t *testing.T) {
	base, err := pgxpool.ParseConfig("postgres://u:p@localhost:5432/db")
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	defaultMax := base.MaxConns
	defaultIdle := base.MaxConnIdleTime

	cfg, _ := pgxpool.ParseConfig("postgres://u:p@localhost:5432/db")
	applyPoolConfig(cfg, PoolConfig{MaxConns: 25, MaxConnLifetime: 30 * time.Minute})

	if cfg.MaxConns != 25 {
		t.Errorf("MaxConns = %d, want 25 (overridden)", cfg.MaxConns)
	}
	if cfg.MaxConnLifetime != 30*time.Minute {
		t.Errorf("MaxConnLifetime = %v, want 30m (overridden)", cfg.MaxConnLifetime)
	}
	// Fields left zero in PoolConfig keep pgx's defaults.
	if cfg.MaxConnIdleTime != defaultIdle {
		t.Errorf("MaxConnIdleTime = %v, want pgx default %v (untouched)", cfg.MaxConnIdleTime, defaultIdle)
	}
	if defaultMax == 25 {
		t.Skip("pgx default MaxConns happens to equal the override; test is inconclusive")
	}
}

// TestApplyPoolConfigZeroIsNoOp verifies a zero PoolConfig changes nothing.
func TestApplyPoolConfigZeroIsNoOp(t *testing.T) {
	cfg, _ := pgxpool.ParseConfig("postgres://u:p@localhost:5432/db")
	before := *cfg
	applyPoolConfig(cfg, PoolConfig{})
	if cfg.MaxConns != before.MaxConns || cfg.MinConns != before.MinConns ||
		cfg.MaxConnLifetime != before.MaxConnLifetime || cfg.MaxConnIdleTime != before.MaxConnIdleTime {
		t.Error("zero PoolConfig altered a pool field")
	}
}

// TestNewWithPoolBadURL surfaces a parse error rather than panicking.
func TestNewWithPoolBadURL(t *testing.T) {
	if _, err := NewWithPool(context.Background(), "://not a url", PoolConfig{}); err == nil {
		t.Fatal("expected an error for a malformed database URL")
	}
}
