package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
	"github.com/scaffold-tool/scaffold/internal/aws"
	"github.com/scaffold-tool/scaffold/internal/config"
	"github.com/scaffold-tool/scaffold/internal/ui"
	"github.com/spf13/cobra"
)

var uninstallForce bool

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove all Scaffold resources and backend infrastructure",
	Long: `Completely remove Scaffold from this repository.

This will:
  - Delete the S3 state bucket (including all history)
  - Delete the DynamoDB lock table
  - Schedule the KMS key for deletion
  - Remove all environment workflow files
  - Delete .scaffold/config.json

All environments must have empty state first (run 'scaffold destroy' for each).
IAM roles in spoke accounts are NOT removed automatically.`,
	RunE: runUninstall,
}

func init() {
	uninstallCmd.Flags().BoolVar(&uninstallForce, "force", false, "Uninstall even if active infrastructure exists (extremely dangerous)")
}

func runUninstall(cmd *cobra.Command, args []string) error {
	yellow := color.New(color.FgYellow)
	green := color.New(color.FgGreen)
	bold := color.New(color.Bold)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("scaffold not initialized")
	}

	yellow.Println("⚠  WARNING: Complete Scaffold Removal")
	fmt.Println()
	fmt.Println("   This will destroy ALL Scaffold resources:")
	color.Red("   ✗ S3 state bucket (including all history)\n")
	color.Red("   ✗ DynamoDB lock table\n")
	color.Red("   ✗ KMS encryption key\n")
	color.Red("   ✗ All environment configurations\n")
	color.Red("   ✗ All workflow files\n")
	fmt.Println()
	fmt.Printf("   Backend account: %s\n\n", cfg.Backend.AccountID)

	// AWS credentials for backend account
	credMethod, profile, err := ui.SelectAWSCredentials()
	if err != nil {
		return err
	}
	spinner := ui.NewSpinner("Verifying credentials...")
	spinner.Start()
	client, identity, err := aws.NewClientWithCredentials(cfg.Backend.Region, credMethod, profile)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("AWS authentication failed: %w", err)
	}
	if identity.AccountID != cfg.Backend.AccountID {
		return fmt.Errorf("credentials belong to account %s, expected backend account %s",
			identity.AccountID, cfg.Backend.AccountID)
	}

	// Check active infrastructure
	bold.Println("→ Checking for active infrastructure...")
	activeEnvs := []string{}
	for _, env := range cfg.Environments {
		resources, err := client.GetStateResources(cfg.Backend.S3Bucket, env.StateKey)
		resourceCount := len(resources)
		mark := "✓"
		status := "0 resources"
		if err != nil {
			mark = "⚠"
			status = "unknown"
		} else if resourceCount > 0 {
			mark = "✗"
			status = fmt.Sprintf("%d resources", resourceCount)
			activeEnvs = append(activeEnvs, env.Name)
		}
		fmt.Printf("  Environment: %s\n    Account: %s\n    Status:  %s %s\n\n",
			env.Name, env.AccountID, mark, status)
	}

	if len(activeEnvs) > 0 && !uninstallForce {
		color.Red("✗ Cannot uninstall: Active infrastructure in %d environments\n\n", len(activeEnvs))
		fmt.Println("  You must destroy all infrastructure first:")
		for _, e := range activeEnvs {
			color.New(color.FgCyan).Printf("    $ scaffold destroy %s\n", e)
		}
		fmt.Println()
		fmt.Println("  Or force uninstall (DANGEROUS - orphans all resources):")
		color.New(color.FgCyan).Println("    $ scaffold uninstall --force")
		return nil
	}

	if uninstallForce && len(activeEnvs) > 0 {
		totalResources := 0
		for _, env := range cfg.Environments {
			r, _ := client.GetStateResources(cfg.Backend.S3Bucket, env.StateKey)
			totalResources += len(r)
		}
		color.Red("⚠  EXTREMELY DANGEROUS OPERATION\n\n")
		fmt.Printf("   Force uninstall will:\n")
		fmt.Printf("   - Delete state bucket (orphans %d resources across %d accounts)\n",
			totalResources, len(activeEnvs))
		fmt.Println("   - Delete all state history (no rollback possible)")
		fmt.Println("   - Leave IAM roles in spoke accounts")
		fmt.Println("   - Make infrastructure unmanageable by Terraform")
		fmt.Println()

		var confirm string
		if err := survey.AskOne(&survey.Input{Message: "Type 'DESTROY EVERYTHING' to confirm:"}, &confirm); err != nil {
			return fmt.Errorf("failed to read force uninstall confirmation: %w", err)
		}
		if confirm != "DESTROY EVERYTHING" {
			fmt.Println("Aborted.")
			return nil
		}
	} else {
		var confirm string
		if err := survey.AskOne(&survey.Input{Message: "Type 'UNINSTALL' to confirm:"}, &confirm); err != nil {
			return fmt.Errorf("failed to read uninstall confirmation: %w", err)
		}
		if confirm != "UNINSTALL" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Remove environment workflows
	bold.Println("\n→ Removing environments...")
	for _, env := range cfg.Environments {
		if err := os.Remove(env.WorkflowFile); err != nil && !os.IsNotExist(err) {
			yellow.Printf("⚠  Could not delete %s: %v\n", env.WorkflowFile, err)
		} else {
			green.Printf("✓ Workflow deleted: %s\n", env.WorkflowFile)
		}
	}

	// Destroy backend resources
	bold.Println("\n→ Destroying backend resources...")

	spinner = ui.NewSpinner("Emptying S3 bucket...")
	spinner.Start()
	err = client.EmptyBucket(cfg.Backend.S3Bucket)
	spinner.Stop()
	if err != nil {
		yellow.Printf("⚠  Could not empty bucket: %v\n", err)
	} else {
		green.Printf("✓ S3 bucket emptied (%d state files deleted)\n", len(cfg.Environments))
	}

	spinner = ui.NewSpinner("Deleting S3 bucket...")
	spinner.Start()
	err = client.DeleteBucket(cfg.Backend.S3Bucket)
	spinner.Stop()
	if err != nil {
		yellow.Printf("⚠  Could not delete bucket: %v\n", err)
	} else {
		green.Printf("✓ S3 bucket deleted: %s\n", cfg.Backend.S3Bucket)
	}

	spinner = ui.NewSpinner("Deleting DynamoDB table...")
	spinner.Start()
	err = client.DeleteLockTable(cfg.Backend.DynamoDBTable)
	spinner.Stop()
	if err != nil {
		yellow.Printf("⚠  Could not delete table: %v\n", err)
	} else {
		green.Printf("✓ DynamoDB table deleted: %s\n", cfg.Backend.DynamoDBTable)
	}

	if cfg.Backend.KMSKeyID != "" {
		spinner = ui.NewSpinner("Scheduling KMS key deletion...")
		spinner.Start()
		err = client.ScheduleKMSKeyDeletion(cfg.Backend.KMSKeyID, 7)
		spinner.Stop()
		if err != nil {
			yellow.Printf("⚠  Could not schedule KMS deletion: %v\n", err)
		} else {
			green.Println("✓ KMS key scheduled for deletion (7 day waiting period)")
		}
	}

	os.RemoveAll(filepath.Dir(".scaffold/config.json"))
	green.Println("✓ Config deleted: .scaffold/config.json")

	// Report orphaned resources
	if uninstallForce && len(activeEnvs) > 0 {
		fmt.Println()
		yellow.Println("⚠  Orphaned resources remain:")
		for _, env := range cfg.Environments {
			r, _ := client.GetStateResources(cfg.Backend.S3Bucket, env.StateKey)
			if len(r) > 0 {
				fmt.Printf("   Account %s (%s): %d resources\n", env.AccountID, env.Name, len(r))
			}
		}
		fmt.Println()
		fmt.Println("   IAM roles still exist in spoke accounts:")
		for _, env := range cfg.Environments {
			fmt.Printf("   - arn:aws:iam::%s:role/github-actions-%s\n", env.AccountID, env.Name)
		}
		fmt.Println()
		fmt.Println("   Manual cleanup required.")
	} else {
		fmt.Println()
		fmt.Println("  Note: IAM roles in spoke accounts still exist.")
		fmt.Println("  Remove manually if no longer needed.")
	}

	fmt.Println()
	green.Println("✓ Scaffold uninstalled successfully")
	return nil
}
