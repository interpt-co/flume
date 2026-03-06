package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/interpt-co/flume/internal/collector"
	"github.com/interpt-co/flume/internal/dispatcher"
)

var rootCmd = &cobra.Command{
	Use:   "flume",
	Short: "Kubernetes log collector and dispatcher",
	Long:  "flume collects container logs from Kubernetes nodes, writes to Redis, and serves them to browser clients via WebSocket.",
}

var collectorCmd = &cobra.Command{
	Use:   "collector",
	Short: "Run the DaemonSet log collector",
	Long:  "Collects container logs from /var/log/containers/, enriches with K8s metadata, and writes to Redis.",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		cfg, err := collector.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		verbose, _ := cmd.Flags().GetBool("verbose")
		if verbose {
			cfg.Verbose = true
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		return collector.New(cfg).Run(ctx)
	},
}

var dispatcherCmd = &cobra.Command{
	Use:   "dispatcher",
	Short: "Run the client-facing log dispatcher",
	Long:  "Reads logs from Redis and serves them to browser clients via HTTP/WebSocket. Horizontally scalable.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := dispatcher.Config{
			Host:          getStringFlag(cmd, "host", "FLUME_HOST", "0.0.0.0"),
			Port:          getIntFlag(cmd, "port", "FLUME_PORT", 8080),
			BulkWindowMS:  getIntFlag(cmd, "bulk-window-ms", "FLUME_BULK_WINDOW_MS", 100),
			Verbose:       getBoolFlag(cmd, "verbose", "FLUME_VERBOSE", false),
			RedisAddr:     getStringFlag(cmd, "redis-addr", "FLUME_REDIS_ADDR", "localhost:6379"),
			RedisPassword: getStringFlag(cmd, "redis-password", "FLUME_REDIS_PASSWORD", ""),
			RedisDB:       getIntFlag(cmd, "redis-db", "FLUME_REDIS_DB", 0),
			S3Bucket:      getStringFlag(cmd, "s3-bucket", "FLUME_S3_BUCKET", ""),
			S3Prefix:      getStringFlag(cmd, "s3-prefix", "FLUME_S3_PREFIX", ""),
			S3Region:      getStringFlag(cmd, "s3-region", "FLUME_S3_REGION", ""),
			S3Endpoint:    getStringFlag(cmd, "s3-endpoint", "FLUME_S3_ENDPOINT", ""),
		}

		cfg.AuthURL = getStringFlag(cmd, "auth-url", "FLUME_AUTH_URL", "")
		cfg.AuthTimeout = getDurationFlag(cmd, "auth-timeout", "FLUME_AUTH_TIMEOUT", 5*time.Second)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		return dispatcher.New(cfg).Run(ctx)
	},
}

func init() {
	// Persistent flags shared by all commands.
	rootCmd.PersistentFlags().Bool("verbose", false, "Enable verbose (debug) logging")

	// Collector flags.
	collectorCmd.Flags().String("config", "/etc/flume/config.yaml", "Path to collector config YAML")

	// Dispatcher flags.
	dispatcherCmd.Flags().Int("port", 8080, "HTTP server port")
	dispatcherCmd.Flags().String("host", "0.0.0.0", "HTTP server bind address")
	dispatcherCmd.Flags().Int("bulk-window-ms", 100, "WebSocket flush interval in milliseconds")
	dispatcherCmd.Flags().String("redis-addr", "localhost:6379", "Redis address")
	dispatcherCmd.Flags().String("redis-password", "", "Redis password")
	dispatcherCmd.Flags().Int("redis-db", 0, "Redis database number")
	dispatcherCmd.Flags().String("s3-bucket", "", "S3 bucket for log history (read-only)")
	dispatcherCmd.Flags().String("s3-prefix", "", "S3 key prefix")
	dispatcherCmd.Flags().String("s3-region", "", "AWS region")
	dispatcherCmd.Flags().String("s3-endpoint", "", "Custom S3 endpoint (MinIO, localstack)")
	dispatcherCmd.Flags().String("auth-url", "", "Auth callback URL (POST) for WebSocket upgrade authorization")
	dispatcherCmd.Flags().String("auth-timeout", "5s", "Timeout for auth callback requests")

	rootCmd.AddCommand(collectorCmd, dispatcherCmd)
}

func getIntFlag(cmd *cobra.Command, flag, envVar string, fallback int) int {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetInt(flag)
		return v
	}
	if env := os.Getenv(envVar); env != "" {
		if v, err := strconv.Atoi(env); err == nil {
			return v
		}
	}
	return fallback
}

func getStringFlag(cmd *cobra.Command, flag, envVar string, fallback string) string {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetString(flag)
		return v
	}
	if env := os.Getenv(envVar); env != "" {
		return env
	}
	return fallback
}

func getDurationFlag(cmd *cobra.Command, flag, envVar string, fallback time.Duration) time.Duration {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetString(flag)
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		fmt.Fprintf(os.Stderr, "warning: invalid duration for --%s: %q, using default %s\n", flag, v, fallback)
	}
	if env := os.Getenv(envVar); env != "" {
		if d, err := time.ParseDuration(env); err == nil {
			return d
		}
		fmt.Fprintf(os.Stderr, "warning: invalid duration in %s: %q, using default %s\n", envVar, env, fallback)
	}
	return fallback
}

func getBoolFlag(cmd *cobra.Command, flag, envVar string, fallback bool) bool {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetBool(flag)
		return v
	}
	if env := os.Getenv(envVar); env != "" {
		v, err := strconv.ParseBool(env)
		if err == nil {
			return v
		}
	}
	return fallback
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
