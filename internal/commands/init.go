package commands

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
	"github.com/scaffold-tool/scaffold/internal/aws"
	"github.com/scaffold-tool/scaffold/internal/config"
	"github.com/scaffold-tool/scaffold/internal/ui"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize centralized Terraform state backend",
	Long: `Bootstrap a centralized Terraform state backend in your platform AWS account.

This creates:
  - S3 bucket with versioning, encryption, and lifecycle policies
  - DynamoDB table for state locking
  - KMS key for encryption
  - .scaffold/config.json to track configuration`,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)
	bold := color.New(color.Bold)

	// Detect repository
	repo, err := config.DetectRepository()
	if err != nil {
		return fmt.Errorf("failed to detect repository: %w", err)
	}
	green.Printf("Auto-detected repository: %s/%s\n\n", repo.Org, repo.Name)

	// Check if already initialized
	cfg, _ := config.Load()
	if cfg != nil {
		yellow.Println("⚠  Scaffold is already initialized in this repository.")
		var overwrite bool
		survey.AskOne(&survey.Confirm{
			Message: "Re-initialize (existing backend will be imported if found)?",
			Default: false,
		}, &overwrite)
		if !overwrite {
			return nil
		}
	}

	bold.Println("→ Backend Account Configuration")
	fmt.Println("  This account will host the centralized state backend.")
	fmt.Println()

	// Collect backend account info
	var accountID, region string
	survey.AskOne(&survey.Input{Message: "AWS Account ID:"}, &accountID,
		survey.WithValidator(survey.Required))
	survey.AskOne(&survey.Input{Message: "AWS Region:", Default: "us-east-1"}, &region)

	// Credential selection
	credMethod, profile, err := ui.SelectAWSCredentials()
	if err != nil {
		return err
	}

	// Verify credentials
	spinner := ui.NewSpinner("Verifying credentials...")
	spinner.Start()
	awsClient, identity, err := aws.NewClientWithCredentials(region, credMethod, profile)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("AWS authentication failed: %w", err)
	}
	green.Printf("✓ Authenticated as: %s\n\n", identity.ARN)

	// Validate account ID matches
	if identity.AccountID != accountID {
		return fmt.Errorf("credentials belong to account %s, expected %s", identity.AccountID, accountID)
	}

	bold.Println("→ Backend Configuration")
	defaultBucket := fmt.Sprintf("tf-state-%s", repo.Name)
	defaultTable := fmt.Sprintf("tf-lock-%s", repo.Name)

	var bucketName, tableName string
	var enableKMS bool
	survey.AskOne(&survey.Input{
		Message: "S3 Bucket Name",
		Help:    "Leave empty to auto-generate",
	}, &bucketName)
	if bucketName == "" {
		bucketName = defaultBucket
	}
	survey.AskOne(&survey.Input{
		Message: "DynamoDB Table Name",
		Help:    "Leave empty to auto-generate",
	}, &tableName)
	if tableName == "" {
		tableName = defaultTable
	}
	survey.AskOne(&survey.Confirm{Message: "Enable KMS encryption?", Default: true}, &enableKMS)

	fmt.Println()
	bold.Println("→ Provisioning backend resources...")

	// Provision KMS
	var kmsKeyID string
	if enableKMS {
		spinner = ui.NewSpinner("Creating KMS key...")
		spinner.Start()
		kmsKeyID, err = awsClient.CreateKMSKey(repo.Name, repo.Org, repo.Name)
		spinner.Stop()
		if err != nil {
			return fmt.Errorf("failed to create KMS key: %w", err)
		}
		green.Printf("✓ KMS key: alias/terraform-state-%s\n", repo.Name)
	}

	// Provision S3
	spinner = ui.NewSpinner("Creating S3 bucket...")
	spinner.Start()
	finalBucket, err := awsClient.CreateStateBucket(bucketName, region, accountID, kmsKeyID, repo.Org, repo.Name)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("failed to create S3 bucket: %w", err)
	}
	green.Printf("✓ S3 bucket: %s\n", finalBucket)
	fmt.Println("    - Versioning enabled")
	fmt.Println("    - Encryption: aws:kms")
	fmt.Println("    - Public access blocked")

	// Provision DynamoDB
	spinner = ui.NewSpinner("Creating DynamoDB table...")
	spinner.Start()
	finalTable, err := awsClient.CreateLockTable(tableName, region, repo.Org, repo.Name)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("failed to create DynamoDB table: %w", err)
	}
	green.Printf("✓ DynamoDB table: %s\n", finalTable)
	fmt.Println("    - On-demand billing")
	fmt.Println("    - Point-in-time recovery enabled")

	// Save config
	cfg = &config.Config{
		Version: "1.0",
		Backend: config.Backend{
			AccountID:     accountID,
			Region:        region,
			S3Bucket:      finalBucket,
			DynamoDBTable: finalTable,
			KMSKeyID:      kmsKeyID,
		},
		Repository: config.Repository{
			Org:           repo.Org,
			Name:          repo.Name,
			DefaultBranch: repo.DefaultBranch,
		},
	}
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	green.Println("✓ Config saved: .scaffold/config.json")

	fmt.Println()
	bold.Println("→ Backend ready!")
	fmt.Printf("  State backend created in account %s\n\n", accountID)
	fmt.Println("  Next steps:")
	fmt.Println("  1. Create your first environment:")
	color.New(color.FgCyan).Println("     $ scaffold create staging")
	fmt.Println()
	fmt.Println("  2. Commit configuration:")
	color.New(color.FgCyan).Println("     $ git add .scaffold/")
	color.New(color.FgCyan).Println("     $ git commit -m \"feat: initialize Scaffold backend\"")

	return nil
}
