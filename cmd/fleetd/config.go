package main

import (
	"log/slog"
	"strconv"
	"time"
)

// serverConfig holds the operator-tunable timeouts and cadences. Every field
// defaults to the value that used to be a compiled-in constant, so an unset
// environment reproduces today's behaviour exactly.
type serverConfig struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	GCInterval        time.Duration
}

// loadServerConfig reads the server tunables from the environment, falling back
// to the historical defaults for any that are unset or invalid.
func loadServerConfig() serverConfig {
	return serverConfig{
		ReadHeaderTimeout: envDuration("FLEET_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       envDuration("FLEET_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:      envDuration("FLEET_WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:       envDuration("FLEET_IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout:   envDuration("FLEET_SHUTDOWN_TIMEOUT", 10*time.Second),
		GCInterval:        envDuration("FLEET_GC_INTERVAL", 1*time.Hour),
	}
}

// envDuration reads key as a Go duration ("15s", "2m", "1h"). Unset returns def.
// An unparseable or non-positive value also returns def, logging a warning: a
// typo must never silently disable a timeout or make an interval nonpositive.
func envDuration(key string, def time.Duration) time.Duration {
	v := envOr(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		slog.Warn("ignoring invalid duration env, using default", "key", key, "value", v, "default", def)
		return def
	}
	return d
}

// envInt reads key as an int. Unset returns def. An unparseable or non-positive
// value also returns def, with a warning - the same don't-silently-break policy
// as envDuration (a zero pool size would starve the server).
func envInt(key string, def int) int {
	v := envOr(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		slog.Warn("ignoring invalid int env, using default", "key", key, "value", v, "default", def)
		return def
	}
	return n
}
