package config

import "time"

// Config holds the runtime configuration for flume.
type Config struct {
	// Server settings
	Host         string
	Port         int
	MaxMessages  int  // ring buffer capacity
	BulkWindowMS int  // WebSocket flush interval in milliseconds
	Verbose      bool

	// Source-specific settings
	FilePaths   []string // for follow command
	SocketAddr  string   // for socket command (e.g., ":9999")
	ForwardAddr string   // for forward command (e.g., ":24224")
	DemoRate    int      // messages per second for demo command

	// S3 storage settings (optional - if Bucket is empty, no persistence)
	S3Bucket        string
	S3Prefix        string
	S3Region        string
	S3Endpoint      string        // custom endpoint (MinIO, localstack)
	S3FlushInterval  time.Duration // default 10s
	S3FlushCount     int           // default 1000
	S3PartitionLabel string        // partition S3 keys by this label (empty = no partitioning)
	S3Retention      time.Duration // TTL for S3 data (0 = disabled)
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Host:         "0.0.0.0",
		Port:         8080,
		MaxMessages:  10000,
		BulkWindowMS: 100,
		Verbose:      false,

		FilePaths:   nil,
		SocketAddr:  ":9999",
		ForwardAddr: ":24224",
		DemoRate:    10,

		S3FlushInterval: 10 * time.Second,
		S3FlushCount:    1000,
	}
}
