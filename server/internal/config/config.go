// Package config loads server configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved server configuration.
type Config struct {
	Port              string
	DatabaseURL       string
	WebhookSigningKey string
	PublicURL         string
	ReaperIdle        time.Duration
	ReaperTick        time.Duration
	CORSOrigin        string
}

// Load reads the configuration from the process environment. It reports every
// missing required variable and every unparsable value in a single error.
func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	var missing, invalid []string

	cfg := Config{
		Port:              or(getenv("PORT"), "8080"),
		DatabaseURL:       getenv("DATABASE_URL"),
		WebhookSigningKey: getenv("WEBHOOK_SIGNING_KEY"),
		PublicURL:         getenv("PUBLIC_URL"),
		CORSOrigin:        or(getenv("CORS_ORIGIN"), "*"),
	}

	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.WebhookSigningKey == "" {
		missing = append(missing, "WEBHOOK_SIGNING_KEY")
	}

	var err error
	if cfg.ReaperIdle, err = seconds(getenv("REAPER_IDLE_SECONDS"), 60); err != nil {
		invalid = append(invalid, "REAPER_IDLE_SECONDS: "+err.Error())
	}
	if cfg.ReaperTick, err = seconds(getenv("REAPER_TICK_SECONDS"), 30); err != nil {
		invalid = append(invalid, "REAPER_TICK_SECONDS: "+err.Error())
	}

	var errs []error
	if len(missing) > 0 {
		errs = append(errs, fmt.Errorf("missing required env: %s", strings.Join(missing, ", ")))
	}
	for _, msg := range invalid {
		errs = append(errs, errors.New(msg))
	}
	if len(errs) > 0 {
		return Config{}, fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// seconds parses a positive integer number of seconds, defaulting when unset.
func seconds(v string, def int) (time.Duration, error) {
	if v == "" {
		return time.Duration(def) * time.Second, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("want positive integer seconds, got %q", v)
	}
	return time.Duration(n) * time.Second, nil
}
