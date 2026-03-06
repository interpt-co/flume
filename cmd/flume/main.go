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

	"github.com/interpt-co/flume/internal/aggregator"
	"github.com/interpt-co/flume/internal/collector"
)

var rootCmd = &cobra.Command{
	Use:   "flume",
	Short: "Kubernetes log collector and aggregator",
	Long:  "flume collects container logs from Kubernetes nodes and serves them to browser clients via WebSocket.",
}

var collectorCmd = &cobra.Command{
	Use:   "collector",
	Short: "Run the DaemonSet log collector",
	Long:  "Collects container logs from /var/log/containers/, enriches with K8s metadata, and streams to the aggregator.",
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

var aggregatorCmd = &cobra.Command{
	Use:   "aggregator",
	Short: "Run the central log aggregator",
	Long:  "Receives log streams from collectors via gRPC and serves them to browser clients.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := aggregator.Config{
			Host:         getStringFlag(cmd, "host", "FLUME_HOST", "0.0.0.0"),
			Port:         getIntFlag(cmd, "port", "FLUME_PORT", 8080),
			GRPCPort:     getIntFlag(cmd, "grpc-port", "FLUME_GRPC_PORT", 9090),
			BufferSize:   getIntFlag(cmd, "max-messages", "FLUME_MAX_MESSAGES", 10000),
			BulkWindowMS: getIntFlag(cmd, "bulk-window-ms", "FLUME_BULK_WINDOW_MS", 100),
			Verbose:      getBoolFlag(cmd, "verbose", "FLUME_VERBOSE", false),
			S3Bucket:     getStringFlag(cmd, "s3-bucket", "FLUME_S3_BUCKET", ""),
			S3Prefix:     getStringFlag(cmd, "s3-prefix", "FLUME_S3_PREFIX", ""),
			S3Region:     getStringFlag(cmd, "s3-region", "FLUME_S3_REGION", ""),
			S3Endpoint:   getStringFlag(cmd, "s3-endpoint", "FLUME_S3_ENDPOINT", ""),
		}

		cfg.AuthURL = getStringFlag(cmd, "auth-url", "FLUME_AUTH_URL", "")
		cfg.AuthTimeout = getDurationFlag(cmd, "auth-timeout", "FLUME_AUTH_TIMEOUT", 5*time.Second)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		return aggregator.New(cfg).Run(ctx)
	},
}

func init() {
	// Persistent flags shared by all commands.
	rootCmd.PersistentFlags().Bool("verbose", false, "Enable verbose (debug) logging")

	// Collector flags.
	collectorCmd.Flags().String("config", "/etc/flume/config.yaml", "Path to collector config YAML")

	// Aggregator flags.
	aggregatorCmd.Flags().Int("port", 8080, "HTTP server port")
	aggregatorCmd.Flags().String("host", "0.0.0.0", "HTTP server bind address")
	aggregatorCmd.Flags().Int("grpc-port", 9090, "gRPC server port")
	aggregatorCmd.Flags().Int("max-messages", 10000, "Per-pattern ring buffer capacity")
	aggregatorCmd.Flags().Int("bulk-window-ms", 100, "WebSocket flush interval in milliseconds")
	aggregatorCmd.Flags().String("s3-bucket", "", "S3 bucket for log history (read-only)")
	aggregatorCmd.Flags().String("s3-prefix", "", "S3 key prefix")
	aggregatorCmd.Flags().String("s3-region", "", "AWS region")
	aggregatorCmd.Flags().String("s3-endpoint", "", "Custom S3 endpoint (MinIO, localstack)")
	aggregatorCmd.Flags().String("auth-url", "", "Auth callback URL (POST) for WebSocket upgrade authorization")
	aggregatorCmd.Flags().String("auth-timeout", "5s", "Timeout for auth callback requests")

	rootCmd.AddCommand(collectorCmd, aggregatorCmd)
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
	}
	if env := os.Getenv(envVar); env != "" {
		if d, err := time.ParseDuration(env); err == nil {
			return d
		}
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
