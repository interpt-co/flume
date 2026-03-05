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

	"github.com/interpt-co/flume/internal/app"
	"github.com/interpt-co/flume/internal/config"
	"github.com/interpt-co/flume/internal/source"
)

var rootCmd = &cobra.Command{
	Use:   "flume",
	Short: "A real-time log viewer with a web UI",
	Long:  "flume pipes log streams through a processing pipeline and serves them to a browser-based viewer via WebSocket.",
}

var stdinCmd = &cobra.Command{
	Use:   "stdin",
	Short: "Read log lines from stdin",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := configFromFlags(cmd)
		src := source.NewStdinSource(os.Stdin)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		application, err := app.New(cfg, src)
		if err != nil {
			return err
		}
		return application.Run(ctx)
	},
}

var followCmd = &cobra.Command{
	Use:   "follow",
	Short: "Follow one or more log files (like tail -f)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := configFromFlags(cmd)

		files, _ := cmd.Flags().GetStringSlice("file")
		cfg.FilePaths = files

		src := source.NewFileSource(files)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		application, err := app.New(cfg, src)
		if err != nil {
			return err
		}
		return application.Run(ctx)
	},
}

var socketCmd = &cobra.Command{
	Use:   "socket",
	Short: "Accept log lines over TCP",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := configFromFlags(cmd)

		addr := getStringFlag(cmd, "listen", "FLUME_SOCKET_ADDR", cfg.SocketAddr)
		cfg.SocketAddr = addr

		src := source.NewSocketSource(addr)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		application, err := app.New(cfg, src)
		if err != nil {
			return err
		}
		return application.Run(ctx)
	},
}

var forwardCmd = &cobra.Command{
	Use:   "forward",
	Short: "Accept logs from Fluent Bit via Forward protocol",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := configFromFlags(cmd)

		addr := getStringFlag(cmd, "listen", "FLUME_FORWARD_LISTEN", cfg.ForwardAddr)
		cfg.ForwardAddr = addr

		src := source.NewForwardSource(addr)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		application, err := app.New(cfg, src)
		if err != nil {
			return err
		}
		return application.Run(ctx)
	},
}

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Generate fake log messages for demonstration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := configFromFlags(cmd)

		rate := getIntFlag(cmd, "rate", "FLUME_DEMO_RATE", cfg.DemoRate)
		cfg.DemoRate = rate

		src := source.NewDemoSource(rate)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		application, err := app.New(cfg, src)
		if err != nil {
			return err
		}
		return application.Run(ctx)
	},
}

func init() {
	// Persistent flags on root command.
	rootCmd.PersistentFlags().Int("port", 8080, "HTTP server port")
	rootCmd.PersistentFlags().String("host", "0.0.0.0", "HTTP server bind address")
	rootCmd.PersistentFlags().Int("max-messages", 10000, "Ring buffer capacity")
	rootCmd.PersistentFlags().Int("bulk-window-ms", 100, "WebSocket flush interval in milliseconds")
	rootCmd.PersistentFlags().Bool("verbose", false, "Enable verbose (debug) logging")

	// S3 storage persistent flags.
	rootCmd.PersistentFlags().String("s3-bucket", "", "S3 bucket for log persistence")
	rootCmd.PersistentFlags().String("s3-prefix", "", "S3 key prefix")
	rootCmd.PersistentFlags().String("s3-region", "", "AWS region")
	rootCmd.PersistentFlags().String("s3-endpoint", "", "Custom S3 endpoint (MinIO, localstack)")
	rootCmd.PersistentFlags().String("s3-flush-interval", "10s", "S3 flush interval")
	rootCmd.PersistentFlags().Int("s3-flush-count", 1000, "S3 flush message count threshold")

	// follow command flags.
	followCmd.Flags().StringSlice("file", nil, "File paths to follow (can be repeated)")
	followCmd.MarkFlagRequired("file")

	// socket command flags.
	socketCmd.Flags().String("listen", ":9999", "TCP address to listen on")

	// forward command flags.
	forwardCmd.Flags().String("listen", ":24224", "TCP address to listen on for Forward protocol")

	// demo command flags.
	demoCmd.Flags().Int("rate", 10, "Messages per second")

	rootCmd.AddCommand(stdinCmd, followCmd, socketCmd, forwardCmd, demoCmd)
}

// configFromFlags builds a Config from cobra command flags with env var fallback.
func configFromFlags(cmd *cobra.Command) config.Config {
	cfg := config.DefaultConfig()

	cfg.Port = getIntFlag(cmd, "port", "FLUME_PORT", cfg.Port)
	cfg.Host = getStringFlag(cmd, "host", "FLUME_HOST", cfg.Host)
	cfg.MaxMessages = getIntFlag(cmd, "max-messages", "FLUME_MAX_MESSAGES", cfg.MaxMessages)
	cfg.BulkWindowMS = getIntFlag(cmd, "bulk-window-ms", "FLUME_BULK_WINDOW_MS", cfg.BulkWindowMS)
	cfg.Verbose = getBoolFlag(cmd, "verbose", "FLUME_VERBOSE", cfg.Verbose)

	// S3 storage flags.
	cfg.S3Bucket = getStringFlag(cmd, "s3-bucket", "FLUME_S3_BUCKET", cfg.S3Bucket)
	cfg.S3Prefix = getStringFlag(cmd, "s3-prefix", "FLUME_S3_PREFIX", cfg.S3Prefix)
	cfg.S3Region = getStringFlag(cmd, "s3-region", "FLUME_S3_REGION", cfg.S3Region)
	cfg.S3Endpoint = getStringFlag(cmd, "s3-endpoint", "FLUME_S3_ENDPOINT", cfg.S3Endpoint)
	cfg.S3FlushInterval = getDurationFlag(cmd, "s3-flush-interval", "FLUME_S3_FLUSH_INTERVAL", cfg.S3FlushInterval)
	cfg.S3FlushCount = getIntFlag(cmd, "s3-flush-count", "FLUME_S3_FLUSH_COUNT", cfg.S3FlushCount)

	return cfg
}

// getIntFlag returns the flag value if changed, else the env var if set, else the fallback.
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

// getStringFlag returns the flag value if changed, else the env var if set, else the fallback.
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

// getDurationFlag returns the flag value (parsed as duration string) if changed,
// else the env var if set, else the fallback.
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

// getBoolFlag returns the flag value if changed, else the env var if set, else the fallback.
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
