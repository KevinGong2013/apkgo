package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/KevinGong2013/apkgo/pkg/telemetry"
	"github.com/KevinGong2013/apkgo/pkg/update"
)

var (
	flagConfig         string
	flagOutput         string
	flagVerbose        bool
	flagTimeout        time.Duration
	flagUpdateInterval time.Duration
)

var rootCmd = &cobra.Command{
	Use:   "apkgo",
	Short: "Upload APKs to multiple Android app stores",
	Long:  "A CLI tool for distributing APK packages to Huawei, Xiaomi, OPPO, vivo, Honor, and custom servers. Designed for AI agent integration.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Configure slog to stderr so stdout stays clean for structured output
		level := slog.LevelWarn
		if flagVerbose {
			level = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

		// Non-blocking update check (skipped for upgrade command itself)
		if cmd.Name() != "upgrade" {
			update.CheckAndRemind(Version, flagUpdateInterval)
		}
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "apkgo.yaml", "config file path")
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "json", "output format: json or text")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose logging to stderr")
	rootCmd.PersistentFlags().DurationVarP(&flagTimeout, "timeout", "t", 10*time.Minute, "global timeout for upload operations")
	rootCmd.PersistentFlags().BoolVar(&telemetry.Disabled, "no-telemetry", false, "disable anonymous usage statistics")
	rootCmd.PersistentFlags().DurationVar(&flagUpdateInterval, "update-check", update.DefaultCheck, "update check interval (0 to disable)")
}

// Execute runs the root command and returns an exit code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		writeError(err)
		return 3
	}
	return exitCode
}

// exitCode is set by subcommands to indicate partial/full failure.
var exitCode int

// writeOutput writes v to stdout as JSON or text.
func writeOutput(v any) {
	if flagOutput == "text" {
		fmt.Fprintln(os.Stdout, v)
		return
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// writeError writes an error to stdout as structured JSON.
func writeError(err error) {
	if flagOutput == "text" {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(map[string]string{"error": err.Error()})
}

// discardLog suppresses all log output (useful for non-verbose mode).
func discardLog() {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
}
