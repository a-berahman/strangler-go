package router

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Mode says where a route goes.
type Mode string

const (
	ModeLegacy Mode = "legacy" // only the old system
	ModeNew    Mode = "new"    // only the new service, after cutover
	ModeShadow Mode = "shadow" // old answers, new one is compared in background
)

type Route struct {
	Prefix string `json:"prefix"`
	Mode   Mode   `json:"mode"`
}

type Config struct {
	Listen        string   `json:"listen"`
	Legacy        string   `json:"legacy"`
	New           string   `json:"new"`
	ShadowTimeout Duration `json:"shadow_timeout"`
	MaxShadow     int      `json:"max_shadow"` // shadow calls in flight, extra ones are skipped
	Routes        []Route  `json:"routes"`
	Ignore        []string `json:"ignore"` // JSON paths the compare should skip
}

// Duration is a time.Duration that reads "300ms" style strings from JSON.
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	*d = Duration(v)
	return err
}

// Load reads the config file and fills the defaults.
func Load(path string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, cfg.check()
}

func (c *Config) check() error {
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.ShadowTimeout <= 0 {
		c.ShadowTimeout = Duration(300 * time.Millisecond)
	}
	if c.MaxShadow <= 0 {
		c.MaxShadow = 100
	}
	if c.Legacy == "" || c.New == "" {
		return fmt.Errorf("config: legacy and new urls are both needed")
	}
	for _, r := range c.Routes {
		switch r.Mode {
		case ModeLegacy, ModeNew, ModeShadow:
		default:
			return fmt.Errorf("config: route %q has unknown mode %q", r.Prefix, r.Mode)
		}
	}
	return nil
}
