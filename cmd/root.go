package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/KevinGong2013/apkgo/v4/pkg/config"
	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
	"github.com/KevinGong2013/apkgo/v4/pkg/telemetry"
	"github.com/KevinGong2013/apkgo/v4/pkg/update"
)

var (
	flagConfig    string
	flagCredsFrom string
	flagOutput    string
	flagVerbose   bool
	flagTimeout   time.Duration
	flagHTTPTrace string
)

// httpTrace is the open --http-trace file, closed when Execute returns.
var httpTrace *httptrace.FileRecorder

var rootCmd = &cobra.Command{
	Use:   "apkgo",
	Short: "Upload APKs to multiple Android app stores",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Configure slog to stderr so stdout stays clean for structured output
		level := slog.LevelWarn
		if flagVerbose {
			level = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

		// Config introspection must remain local and side-effect free.
		if cmd.Name() == "stores" && flagStoresConfigured {
			return nil
		}

		// --http-trace (or APKGO_HTTP_TRACE): record this command's HTTP
		// exchanges with the stores. The commands pass cmd.Context() on.
		path := flagHTTPTrace
		if path == "" {
			path = os.Getenv("APKGO_HTTP_TRACE")
		}
		if path != "" {
			rec, err := httptrace.NewFileRecorder(path)
			if err != nil {
				return fmt.Errorf("--http-trace: %w", err)
			}
			httpTrace = rec
			cmd.SetContext(httptrace.WithRecorder(cmd.Context(), rec))
		}

		// Non-blocking update check (skipped for upgrade command itself)
		if cmd.Name() != "upgrade" {
			cfg := config.LoadOrEmpty(flagConfig)
			update.CheckAndRemind(Version, cfg.UpdateCheckInterval(update.DefaultCheck))
		}
		return nil
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.Version = Version
	rootCmd.Long = fmt.Sprintf("apkgo %s — A CLI tool for distributing APK packages to Huawei, Xiaomi, OPPO, vivo, Honor, and custom servers. Designed for AI agent integration.", Version)
	rootCmd.SetVersionTemplate("apkgo {{.Version}}\n")

	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "apkgo.yaml", "config file path")
	rootCmd.PersistentFlags().StringVar(&flagCredsFrom, "creds-from", "", `read JSON config from a non-disk source: "stdin" or "fd:N" (overrides --config when set)`)
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "json", "output format: json or text")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose logging to stderr")
	rootCmd.PersistentFlags().DurationVarP(&flagTimeout, "timeout", "t", 10*time.Minute, "global timeout for upload operations")
	rootCmd.PersistentFlags().StringVar(&flagHTTPTrace, "http-trace", "", "record every HTTP exchange with the stores to this file (JSON Lines, appended; credentials redacted, uploaded files described by name and size only). Env: APKGO_HTTP_TRACE")
}

// Execute runs the root command and returns an exit code.
func Execute() int {
	// main calls os.Exit right after this returns, which would kill any
	// telemetry request still in flight.
	defer telemetry.Flush(2 * time.Second)
	defer func() {
		if httpTrace != nil {
			httpTrace.Close()
		}
	}()
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

// loadConfigForCmd returns the resolved Config for the current command,
// reading from --creds-from when set (so credentials never touch disk
// or env) and falling back to the YAML file referenced by --config
// otherwise. Centralised so every command picks up the same flag
// semantics without re-implementing them.
func loadConfigForCmd() (*config.Config, error) {
	if flagCredsFrom != "" {
		return config.LoadCreds(flagCredsFrom)
	}
	return config.Load(flagConfig)
}
