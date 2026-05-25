package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Env      string         `yaml:"env"`
	Postgres PostgresConfig `yaml:"postgres"`
	RefSync  RefSyncConfig  `yaml:"ref_sync"`
	Parser   ParserConfig   `yaml:"parser"`
}

type PostgresConfig struct {
	DSN string `yaml:"dsn"`
}

type RefSyncConfig struct {
	Enabled       bool   `yaml:"enabled"`
	SourceBaseURL string `yaml:"source_base_url"`
	Schedule      string `yaml:"schedule"`
}

type ParserConfig struct {
	League         string         `yaml:"league"`
	PollInterval   Duration       `yaml:"poll_interval"`
	RequestTimeout Duration       `yaml:"request_timeout"`
	Workers        []WorkerConfig `yaml:"workers"`
}

type WorkerConfig struct {
	Name      string         `yaml:"name"`
	Proxy     string         `yaml:"proxy"`
	RateLimit RateLimit      `yaml:"rate_limit"`
	Retry     RetryConfig    `yaml:"retry"`
	Cooldown  CooldownConfig `yaml:"cooldown"`
}

type RateLimit struct {
	SearchRPS   float64 `yaml:"search_rps"`
	SearchBurst int     `yaml:"search_burst"`
	FetchRPS    float64 `yaml:"fetch_rps"`
	FetchBurst  int     `yaml:"fetch_burst"`
}

type RetryConfig struct {
	MaxAttempts int      `yaml:"max_attempts"`
	BaseBackoff Duration `yaml:"base_backoff"`
	MaxBackoff  Duration `yaml:"max_backoff"`
	Jitter      bool     `yaml:"jitter"`
}

type CooldownConfig struct {
	ErrorThreshold int      `yaml:"error_threshold"`
	Sleep          Duration `yaml:"sleep"`
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be scalar")
	}
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", node.Value, err)
	}
	d.Duration = parsed
	return nil
}

func Load(path string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Parser.League == "" {
		return fmt.Errorf("parser.league is required")
	}
	if len(c.Parser.Workers) == 0 {
		return fmt.Errorf("parser.workers must not be empty")
	}
	for i, w := range c.Parser.Workers {
		if w.Name == "" {
			return fmt.Errorf("parser.workers[%d].name is required", i)
		}
		if w.RateLimit.SearchRPS <= 0 || w.RateLimit.FetchRPS <= 0 {
			return fmt.Errorf("parser.workers[%d].rate_limit.*_rps must be > 0", i)
		}
		if w.RateLimit.SearchBurst <= 0 || w.RateLimit.FetchBurst <= 0 {
			return fmt.Errorf("parser.workers[%d].rate_limit.*_burst must be > 0", i)
		}
	}
	return nil
}
