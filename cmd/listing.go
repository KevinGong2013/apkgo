package cmd

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/KevinGong2013/apkgo/v4/pkg/apkgo"
)

var (
	flagListingStore   string
	flagListingFile    string
	flagListingPackage string
	flagListingOut     string
)

func init() {
	rootCmd.AddCommand(listingCmd)
	listingCmd.Flags().StringVarP(&flagListingStore, "store", "s", "", "comma-separated store names (default: all configured)")
	listingCmd.Flags().StringVarP(&flagListingFile, "file", "f", "", "APK file (used to derive package name)")
	listingCmd.Flags().StringVarP(&flagListingPackage, "package", "p", "", "package name (overrides --file)")
	listingCmd.Flags().StringVar(&flagListingOut, "out", "", "directory to save the listing into: images under assets/ plus a listing.yaml for `upload --listing`")
}

var listingCmd = &cobra.Command{
	Use:   "listing",
	Short: "Read the listing (商店资料) each store currently holds",
	Long: `Read the current listing — one-line intro, description, icon and
screenshots — of an app from each configured store. Read-only: nothing is
changed on any store.

With --out the images are downloaded and a listing.yaml is written with
every store's values, ready to edit and submit with the next version via
` + "`apkgo upload --listing`" + `. A store that can't report its listing (or part
of it) is left out of the file, so an upload leaves it unchanged there.

Pass -f <apk> or -p <package>.`,
	Example: `  apkgo listing -p com.example.app
  apkgo listing -p com.example.app --out ./listing
  apkgo listing -f app.apk -s oppo,vivo --out ./listing`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfigForCmd()
		if err != nil {
			return err
		}
		var stores []string
		if flagListingStore != "" {
			stores = strings.Split(flagListingStore, ",")
		}
		result, err := apkgo.FetchListing(cmd.Context(), apkgo.ListingJob{
			Config:  cfg,
			Stores:  stores,
			Package: flagListingPackage,
			APKFile: flagListingFile,
			OutDir:  flagListingOut,
		})
		if err != nil {
			return err
		}
		if flagOutput == "text" {
			renderListingText(result)
		} else {
			writeOutput(result)
		}
		if result.AnyFailed() {
			exitCode = 1
		}
		return nil
	},
}

func renderListingText(result *apkgo.ListingReport) {
	rows := append([]apkgo.ListingStoreResult(nil), result.Stores...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Store < rows[j].Store })
	width := 8
	for _, r := range rows {
		if len(r.Store) > width {
			width = len(r.Store)
		}
	}
	for _, r := range rows {
		fmt.Printf("%-*s  %s\n", width, r.Store, listingOneLiner(r))
		for _, n := range r.Notes {
			fmt.Printf("%-*s  - %s\n", width, "", n)
		}
	}
	if result.File != "" {
		fmt.Printf("\nsaved to %s\n", result.File)
	}
}

func listingOneLiner(r apkgo.ListingStoreResult) string {
	if !r.Supported {
		return "listing query not supported"
	}
	var parts []string
	if r.Brief != "" || r.Description != "" || r.Icon != "" || len(r.Screenshots) > 0 {
		icon := "no"
		if r.Icon != "" {
			icon = "yes"
		}
		parts = append(parts, fmt.Sprintf("brief=%d chars, description=%d chars, icon=%s, screenshots=%d",
			utf8.RuneCountInString(r.Brief), utf8.RuneCountInString(r.Description), icon, len(r.Screenshots)))
	}
	if len(r.Unavailable) > 0 {
		parts = append(parts, "not reported by the store: "+strings.Join(r.Unavailable, ", "))
	}
	if r.Error != "" {
		parts = append(parts, "error: "+r.Error)
	}
	if len(parts) == 0 {
		return "no listing"
	}
	return strings.Join(parts, "; ")
}
