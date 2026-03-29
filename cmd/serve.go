package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/KevinGong2013/apkgo/pkg/config"
	"github.com/KevinGong2013/apkgo/pkg/server"
)

var flagPort int

func init() {
	serveCmd.Flags().IntVarP(&flagPort, "port", "p", 8080, "server port")
	rootCmd.AddCommand(serveCmd)
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start web server with APK upload GUI",
	Example: `  apkgo serve
  apkgo serve -p 9090
  apkgo serve -c production.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagConfig)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}

		s := &server.Server{
			Config:  cfg,
			Timeout: flagTimeout,
		}

		fmt.Fprintf(cmd.ErrOrStderr(), "apkgo server running at http://localhost:%d\n", flagPort)
		return s.Start(flagPort)
	},
}
