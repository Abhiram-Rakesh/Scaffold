package commands

import (
	"fmt"
	"runtime"

	"github.com/scaffold-tool/scaffold/pkg/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version and build information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Scaffold v%s\n\n", version.Version)
		fmt.Println("Build Information:")
		fmt.Printf("  Commit:       %s\n", version.Commit)
		fmt.Printf("  Built:        %s\n", version.BuildDate)
		fmt.Printf("  Go Version:   %s\n", runtime.Version())
		fmt.Printf("  Platform:     %s/%s\n\n", runtime.GOOS, runtime.GOARCH)
		fmt.Printf("License:       MIT\n")
		fmt.Printf("Repository:    https://github.com/scaffold-tool/scaffold\n")
		fmt.Printf("Documentation: https://scaffold.sh/docs\n")
	},
}
