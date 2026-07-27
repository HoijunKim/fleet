package main

import (
	"testing"
	"time"
)

func TestEnvDuration(t *testing.T) {
	def := 15 * time.Second

	if got := envDuration("FLEET_TEST_DUR_UNSET", def); got != def {
		t.Errorf("unset = %v, want default %v", got, def)
	}

	t.Setenv("FLEET_TEST_DUR", "2m")
	if got := envDuration("FLEET_TEST_DUR", def); got != 2*time.Minute {
		t.Errorf("valid = %v, want 2m", got)
	}

	t.Setenv("FLEET_TEST_DUR", "not-a-duration")
	if got := envDuration("FLEET_TEST_DUR", def); got != def {
		t.Errorf("garbage = %v, want default %v", got, def)
	}

	t.Setenv("FLEET_TEST_DUR", "0s")
	if got := envDuration("FLEET_TEST_DUR", def); got != def {
		t.Errorf("zero = %v, want default %v (nonpositive rejected)", got, def)
	}

	t.Setenv("FLEET_TEST_DUR", "-5s")
	if got := envDuration("FLEET_TEST_DUR", def); got != def {
		t.Errorf("negative = %v, want default %v", got, def)
	}
}

func TestEnvInt(t *testing.T) {
	def := 4

	if got := envInt("FLEET_TEST_INT_UNSET", def); got != def {
		t.Errorf("unset = %d, want default %d", got, def)
	}

	t.Setenv("FLEET_TEST_INT", "16")
	if got := envInt("FLEET_TEST_INT", def); got != 16 {
		t.Errorf("valid = %d, want 16", got)
	}

	t.Setenv("FLEET_TEST_INT", "xyz")
	if got := envInt("FLEET_TEST_INT", def); got != def {
		t.Errorf("garbage = %d, want default %d", got, def)
	}

	t.Setenv("FLEET_TEST_INT", "0")
	if got := envInt("FLEET_TEST_INT", def); got != def {
		t.Errorf("zero = %d, want default %d (nonpositive rejected)", got, def)
	}
}

func TestLoadServerConfigDefaults(t *testing.T) {
	cfg := loadServerConfig()
	want := serverConfig{
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ShutdownTimeout:   10 * time.Second,
		GCInterval:        1 * time.Hour,
	}
	if cfg != want {
		t.Errorf("defaults = %+v, want %+v", cfg, want)
	}
}

func TestLoadServerConfigOverride(t *testing.T) {
	t.Setenv("FLEET_WRITE_TIMEOUT", "45s")
	t.Setenv("FLEET_GC_INTERVAL", "30m")
	cfg := loadServerConfig()
	if cfg.WriteTimeout != 45*time.Second {
		t.Errorf("WriteTimeout = %v, want 45s", cfg.WriteTimeout)
	}
	if cfg.GCInterval != 30*time.Minute {
		t.Errorf("GCInterval = %v, want 30m", cfg.GCInterval)
	}
	// An untouched field keeps its default.
	if cfg.ReadTimeout != 15*time.Second {
		t.Errorf("ReadTimeout = %v, want default 15s", cfg.ReadTimeout)
	}
}
