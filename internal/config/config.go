// Package config loads and exposes server configuration.
// All other packages receive a *Config — they never read flags or env directly.
package config

import (
	"flag"
	"fmt"
)

// Config holds all server settings. It is populated once at startup and then
// treated as read-only — no locks needed.
type Config struct {
	// Host is the IP address to listen on. Use "0.0.0.0" for all interfaces.
	Host string

	// Port is the TCP port to listen on. Redis default is 6379.
	Port int

	// AOFEnabled controls whether commands are persisted to the AOF file.
	AOFEnabled bool

	// AOFPath is the path to the append-only file on disk.
	AOFPath string

	// AOFSync controls fsync policy: "always" | "everysec" | "no".
	//   always   — fsync after every write (safest, slowest)
	//   everysec — fsync once per second (good default)
	//   no       — let the OS decide (fastest, least safe)
	AOFSync string

	// LogLevel controls log verbosity: "debug" | "info" | "warn" | "error".
	LogLevel string

	// MaxClients is the maximum number of simultaneous client connections.
	// 0 means unlimited.
	MaxClients int
}

// Addr returns the "host:port" string suitable for net.Listen.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// Load parses command-line flags and returns a populated Config.
// Defaults mirror Redis's own defaults where possible.
func Load() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.Host, "host", "0.0.0.0", "IP address to listen on")
	flag.IntVar(&cfg.Port, "port", 6379, "TCP port to listen on")
	flag.BoolVar(&cfg.AOFEnabled, "aof", true, "Enable AOF persistence")
	flag.StringVar(&cfg.AOFPath, "aof-path", "appendonly.aof", "Path to AOF file")
	flag.StringVar(&cfg.AOFSync, "aof-sync", "everysec", "AOF fsync policy: always|everysec|no")
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "Log level: debug|info|warn|error")
	flag.IntVar(&cfg.MaxClients, "max-clients", 0, "Max simultaneous clients (0 = unlimited)")

	flag.Parse()
	return cfg
}
