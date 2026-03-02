package commands

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/scaffold-tool/scaffold/pkg/version"
	"github.com/spf13/cobra"
)

var (
	verbose bool
	quiet   bool
)

var rootCmd = &cobra.Command{
	Use:   "scaffold",
	Short: "Multi-Account Terraform CI/CD Platform",
	Long: `Scaffold bootstraps production-grade, multi-account Terraform CI/CD pipelines
using GitHub Actions, centralized S3 remote state, and IAM OIDC authentication.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		color.Red("Error: %v", err)
		return err
	}
	return nil
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Suppress non-error output")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(destroyCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(versionCmd)

	// Print banner on any command
	cobra.OnInitialize(func() {
		if !quiet {
			printBanner()
		}
	})
}

func printBanner() {
	cyan := color.New(color.FgCyan, color.Bold)
	fmt.Println()
	cyan.Println("╭──────────────────────────────────────╮")
	cyan.Println("│   Scaffold - Infrastructure CI/CD   │")
	cyan.Printf("│   Version %-26s│\n", getVersion())
	cyan.Println("╰──────────────────────────────────────╯")
	fmt.Println()
}

func getVersion() string {
	return version.Version
}
