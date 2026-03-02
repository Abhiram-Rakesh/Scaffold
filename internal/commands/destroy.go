package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
	"github.com/scaffold-tool/scaffold/internal/aws"
	"github.com/scaffold-tool/scaffold/internal/config"
	"github.com/scaffold-tool/scaffold/internal/terraform"
	"github.com/scaffold-tool/scaffold/internal/ui"
	"github.com/spf13/cobra"
)

var destroyAutoApprove bool

var destroyCmd = &cobra.Command{
	Use:   "destroy <environment-name>",
	Short: "Destroy all infrastructure managed by Terraform in the environment",
	Long: `Destroy all Terraform-managed infrastructure in the specified environment.

This runs 'terraform destroy' in the watch directory using the centralized
state backend. The operation is irreversible.

Use 'scaffold remove <env>' afterward to clean up the environment configuration.`,
	Args: cobra.ExactArgs(1),
	RunE: runDestroy,
}

func init() {
	destroyCmd.Flags().BoolVar(&destroyAutoApprove, "auto-approve", false, "Skip confirmation prompt (dangerous)")
}

func runDestroy(cmd *cobra.Command, args []string) error {
	envName := args[0]
	green := color.New(color.FgGreen)
	bold := color.New(color.Bold)
	yellow := color.New(color.FgYellow)

	// Validate environment
	bold.Println("→ Validating environment...")
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("scaffold not initialized")
	}
	env := cfg.GetEnvironment(envName)
	if env == nil {
		return fmt.Errorf("environment '%s' not found", envName)
	}
	green.Printf("✓ Environment '%s' exists\n", envName)

	// Check backend accessible
	credMethod, profile, err := ui.SelectAWSCredentials()
	if err != nil {
		return err
	}

	spinner := ui.NewSpinner("Verifying credentials...")
	spinner.Start()
	client, identity, err := aws.NewClientWithCredentials(env.Region, credMethod, profile)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("AWS authentication failed: %w", err)
	}
	if identity.AccountID != env.AccountID {
		return fmt.Errorf("credentials belong to account %s, expected target account %s",
			identity.AccountID, env.AccountID)
	}
	green.Printf("✓ Authenticated as: %s\n\n", identity.ARN)

	// Check state locks
	bold.Println("→ Checking state locks...")
	lock, err := client.GetStateLock(cfg.Backend.DynamoDBTable, cfg.Backend.S3Bucket, env.StateKey)
	if err == nil && lock != nil {
		color.Red("✗ Found active lock\n\n")
		fmt.Printf("  Lock ID:   %s\n", lock.LockID)
		fmt.Printf("  Operation: %s\n", lock.Operation)
		fmt.Printf("  Who:       %s\n", lock.Who)
		fmt.Printf("  Created:   %s\n\n", lock.Created)
		fmt.Println("  This lock may be stale if:")
		fmt.Println("  - GitHub Actions workflow already completed")
		fmt.Println("  - Workflow crashed mid-apply")
		fmt.Println("  - No Terraform operations currently running")
		fmt.Println()

		var removeLock bool
		if err := survey.AskOne(&survey.Confirm{Message: "Remove this lock?", Default: false}, &removeLock); err != nil {
			return fmt.Errorf("failed to read lock removal confirmation: %w", err)
		}
		if !removeLock {
			return fmt.Errorf("cannot proceed with active state lock")
		}

		spinner = ui.NewSpinner("Removing lock...")
		spinner.Start()
		err = client.RemoveStateLock(cfg.Backend.DynamoDBTable, lock.LockID)
		spinner.Stop()
		if err != nil {
			return fmt.Errorf("failed to remove lock: %w", err)
		}
		green.Println("✓ Lock removed")
	} else {
		green.Println("✓ No active locks found")
	}

	// Init Terraform
	bold.Println("\n→ Initializing Terraform...")
	fmt.Printf("  Working directory: %s\n", env.WatchDir)
	fmt.Printf("  Backend: s3://%s/%s\n\n", cfg.Backend.S3Bucket, env.StateKey)

	tfEnv := os.Environ()
	switch credMethod {
	case aws.CredentialProfile, aws.CredentialSSO:
		if profile != "" {
			tfEnv = append(tfEnv, "AWS_PROFILE="+profile)
		}
		tfEnv = append(tfEnv, "AWS_SDK_LOAD_CONFIG=1")
		// Ensure stale env credentials don't override selected profile/session.
		tfEnv = append(tfEnv,
			"AWS_ACCESS_KEY_ID=",
			"AWS_SECRET_ACCESS_KEY=",
			"AWS_SESSION_TOKEN=",
		)
	}

	// Use backend resource identifiers that work cross-account.
	dynamoBackend := fmt.Sprintf("arn:aws:dynamodb:%s:%s:table/%s",
		cfg.Backend.Region, cfg.Backend.AccountID, cfg.Backend.DynamoDBTable)
	kmsBackend := cfg.Backend.KMSKeyID
	if kmsBackend != "" && !strings.HasPrefix(kmsBackend, "arn:") {
		kmsBackend = fmt.Sprintf("arn:aws:kms:%s:%s:key/%s",
			cfg.Backend.Region, cfg.Backend.AccountID, kmsBackend)
	}

	tfRunner := terraform.NewRunner(
		env.WatchDir,
		cfg.Backend.S3Bucket,
		env.StateKey,
		cfg.Backend.Region,
		dynamoBackend,
		kmsBackend,
		tfEnv,
	)

	spinner = ui.NewSpinner("Running terraform init...")
	spinner.Start()
	if err := tfRunner.Init(); err != nil {
		spinner.Stop()
		return fmt.Errorf("terraform init failed: %w", err)
	}
	spinner.Stop()
	green.Println("✓ Terraform initialized")

	// Generate destroy plan
	bold.Println("\n→ Generating destroy plan...")
	fmt.Println("  Refreshing state...")

	resources, planOutput, err := tfRunner.PlanDestroy()
	if err != nil {
		return fmt.Errorf("terraform plan failed: %w", err)
	}

	if len(resources) == 0 {
		yellow.Println("  State is empty - nothing to destroy.")
		return nil
	}

	fmt.Printf("\n  Plan: 0 to add, 0 to change, %d to destroy\n\n", len(resources))
	fmt.Println("  Resources to be destroyed:")
	for _, r := range resources {
		fmt.Printf("  - %s\n", r)
	}
	_ = planOutput

	// Confirmation
	if !destroyAutoApprove {
		fmt.Println()
		bold.Println("→ Destroy Confirmation")
		fmt.Printf("  Environment:    %s\n", envName)
		fmt.Printf("  AWS Account:    %s\n", env.AccountID)
		fmt.Printf("  Region:         %s\n", env.Region)
		fmt.Printf("  Watch Dir:      %s\n", env.WatchDir)
		fmt.Printf("  Resource Count: %d\n\n", len(resources))
		color.Red("  This action is IRREVERSIBLE.\n")

		var confirm string
		if err := survey.AskOne(&survey.Input{
			Message: fmt.Sprintf("Type '%s' to confirm:", envName),
		}, &confirm); err != nil {
			return fmt.Errorf("failed to read destroy confirmation: %w", err)
		}
		if confirm != envName {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Run destroy
	bold.Println("\n→ Destroying infrastructure...")
	if err := tfRunner.Destroy(); err != nil {
		return fmt.Errorf("terraform destroy failed: %w", err)
	}
	green.Println("\n✓ Destroy complete")

	fmt.Println()
	bold.Println("→ Cleanup")
	fmt.Println("  State file is now empty.")
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Printf("  - Remove environment configuration: scaffold remove %s\n", envName)
	fmt.Println("  - Or create new infrastructure: git push (triggers workflow)")

	return nil
}
