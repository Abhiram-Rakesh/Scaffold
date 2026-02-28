package commands

import (
	"fmt"
	"os"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
	"github.com/scaffold-tool/scaffold/internal/aws"
	"github.com/scaffold-tool/scaffold/internal/config"
	"github.com/scaffold-tool/scaffold/internal/ui"
	"github.com/spf13/cobra"
)

var removeForce bool

var removeCmd = &cobra.Command{
	Use:   "remove <environment-name>",
	Short: "Remove environment workflow and configuration",
	Long: `Remove an environment's GitHub Actions workflow and Scaffold configuration.

This does NOT destroy infrastructure in the target account.
Use 'scaffold destroy <env>' first to remove all managed resources.`,
	Args: cobra.ExactArgs(1),
	RunE: runRemove,
}

func init() {
	removeCmd.Flags().BoolVar(&removeForce, "force", false, "Remove even if active infrastructure exists (dangerous)")
}

func runRemove(cmd *cobra.Command, args []string) error {
	envName := args[0]
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)
	bold := color.New(color.Bold)

	bold.Println("→ Validating environment...")
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("scaffold not initialized")
	}
	env := cfg.GetEnvironment(envName)
	if env == nil {
		return fmt.Errorf("environment '%s' not found", envName)
	}
	green.Printf("✓ Environment '%s' exists\n\n", envName)

	// Check for active infrastructure
	bold.Println("→ Checking for active infrastructure...")
	fmt.Printf("  Querying state file: s3://%s/%s\n\n", cfg.Backend.S3Bucket, env.StateKey)

	credMethod, profile, err := ui.SelectAWSCredentials()
	if err != nil {
		return err
	}
	spinner := ui.NewSpinner("Loading state file...")
	spinner.Start()
	backendClient, _, err := aws.NewClientWithCredentials(cfg.Backend.Region, credMethod, profile)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("AWS authentication failed: %w", err)
	}

	resources, err := backendClient.GetStateResources(cfg.Backend.S3Bucket, env.StateKey)
	if err != nil {
		yellow.Printf("⚠  Could not read state file: %v\n", err)
		yellow.Println("  Assuming environment may have active resources.")
	}

	if len(resources) > 0 && !removeForce {
		color.Red("✗ Found %d resources in state:\n", len(resources))
		for _, r := range resources {
			fmt.Printf("    - %s\n", r)
		}
		fmt.Println()
		color.Red("Cannot remove environment with active infrastructure.\n")
		fmt.Println("  Options:")
		fmt.Printf("  1. Destroy infrastructure first:\n")
		color.New(color.FgCyan).Printf("     $ scaffold destroy %s\n\n", envName)
		fmt.Printf("  2. Force remove (WARNING: leaves orphaned resources):\n")
		color.New(color.FgCyan).Printf("     $ scaffold remove %s --force\n", envName)
		return nil
	}

	if removeForce && len(resources) > 0 {
		yellow.Printf("⚠  WARNING: Force removing environment with %d active resources!\n\n", len(resources))
		fmt.Println("   This will:")
		fmt.Println("   - Delete the GitHub workflow")
		fmt.Println("   - Remove environment from Scaffold config")
		fmt.Printf("   - Leave %d resources running in AWS account %s\n", len(resources), env.AccountID)
		fmt.Println("   - You must manually clean up resources afterward")
		fmt.Println()

		var confirm string
		survey.AskOne(&survey.Input{
			Message: fmt.Sprintf("Type '%s' to confirm force removal:", envName),
		}, &confirm)
		if confirm != envName {
			fmt.Println("Aborted.")
			return nil
		}
	}

	bold.Println("→ Removing environment...")

	// Delete workflow file
	if err := os.Remove(env.WorkflowFile); err != nil && !os.IsNotExist(err) {
		yellow.Printf("⚠  Could not delete workflow file: %v\n", err)
	} else {
		green.Printf("✓ Workflow deleted: %s\n", env.WorkflowFile)
	}

	// Update config
	cfg.RemoveEnvironment(envName)
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}
	green.Println("✓ Config updated: .scaffold/config.json")

	if removeForce && len(resources) > 0 {
		fmt.Println()
		yellow.Printf("⚠  Orphaned resources remain in account %s\n", env.AccountID)
		fmt.Println("   Manual cleanup required:")
		fmt.Println("   - Navigate to AWS Console")
		fmt.Println("   - Or use AWS CLI to terminate resources")
		fmt.Println()
		fmt.Printf("   IAM role still exists: github-actions-%s\n", envName)
		fmt.Printf("   Remove manually or run: scaffold cleanup %s\n", envName)
	}

	fmt.Println()
	green.Printf("→ Environment '%s' removed\n", envName)
	return nil
}
