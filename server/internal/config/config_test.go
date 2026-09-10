package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(env(map[string]string{"DATABASE_URL": "postgres://x", "WEBHOOK_SIGNING_KEY": "wh_test"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Config{
		Port:              "8080",
		DatabaseURL:       "postgres://x",
		WebhookSigningKey: "wh_test",
		ReaperIdle:        60 * time.Second,
		ReaperTick:        30 * time.Second,
		CORSOrigin:        "*",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("got %+v\nwant %+v", cfg, want)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	_, err := load(env(map[string]string{}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, s := range []string{"DATABASE_URL", "WEBHOOK_SIGNING_KEY"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error should name %s, got: %v", s, err)
		}
	}
}

func TestLoadOverridesAndInvalid(t *testing.T) {
	cfg, err := load(env(map[string]string{
		"DATABASE_URL":        "postgres://x",
		"WEBHOOK_SIGNING_KEY": "wh_test",
		"PORT":                "9000",
		"REAPER_IDLE_SECONDS": "120",
		"REAPER_TICK_SECONDS": "5",
		"CORS_ORIGIN":         "https://ext.example",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "9000" || cfg.ReaperIdle != 120*time.Second || cfg.ReaperTick != 5*time.Second || cfg.CORSOrigin != "https://ext.example" {
		t.Errorf("overrides not applied: %+v", cfg)
	}

	_, err = load(env(map[string]string{"REAPER_IDLE_SECONDS": "abc"}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, s := range []string{"DATABASE_URL", "REAPER_IDLE_SECONDS"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error should mention %s, got: %v", s, err)
		}
	}
}
