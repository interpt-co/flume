package collector

import (
	"fmt"
	"os"
	"time"

	"github.com/interpt-co/flume/internal/pattern"
	"gopkg.in/yaml.v3"
)

// Config holds the collector configuration loaded from YAML.
type Config struct {
	LogDir     string        `yaml:"logDir"`
	BufferSize int           `yaml:"bufferSize"`
	Verbose    bool          `yaml:"verbose"`
	Aggregator AggregatorRef `yaml:"aggregator"`
	S3         S3Ref         `yaml:"s3"`
	Patterns   []pattern.PatternDef `yaml:"patterns"`
	// StaticLabels provides pod labels for local testing without K8s API.
	// Key format: "namespace/pod" -> labels map.
	StaticLabels map[string]map[string]string `yaml:"staticLabels"`
}

// AggregatorRef holds the gRPC aggregator connection info.
type AggregatorRef struct {
	Addr string `yaml:"addr"`
}

// S3Ref holds S3 configuration for the collector.
type S3Ref struct {
	Bucket        string        `yaml:"bucket"`
	Prefix        string        `yaml:"prefix"`
	Region        string        `yaml:"region"`
	Endpoint      string        `yaml:"endpoint"`
	FlushInterval time.Duration `yaml:"flushInterval"`
	FlushCount    int           `yaml:"flushCount"`
	Retention     time.Duration `yaml:"retention"`
}

// LoadConfig reads and parses a YAML config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	// Support nested collector: key or flat structure.
	var wrapper struct {
		Collector Config `yaml:"collector"`
	}
	if err := yaml.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	cfg := wrapper.Collector

	// Defaults.
	if cfg.LogDir == "" {
		cfg.LogDir = "/var/log/containers"
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 10000
	}
	if cfg.S3.FlushInterval == 0 {
		cfg.S3.FlushInterval = 10 * time.Second
	}
	if cfg.S3.FlushCount == 0 {
		cfg.S3.FlushCount = 1000
	}

	return &cfg, nil
}
