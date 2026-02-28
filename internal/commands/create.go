package commands

import (
	"fmt"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
	"github.com/scaffold-tool/scaffold/internal/aws"
	"github.com/scaffold-tool/scaffold/internal/config"
	"github.com/scaffold-tool/scaffold/internal/terraform"
	"github.com/scaffold-tool/scaffold/internal/ui"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create <environment-name>",
	Short: "Create a new environment with workflow and cross-account access",
	Long: `Create a new Terraform environment with:
  - GitHub Actions workflow for automated deployments
  - IAM OIDC role in the target AWS account
  - Cross-account access to the centralized state backend
  - providers.tf with backend configuration`,
	Args: cobra.ExactArgs(1),
	RunE: runCreate,
}

func runCreate(cmd *cobra.Command, args []string) error {
	envName := args[0]
	green := color.New(color.FgGreen)
	bold := color.New(color.Bold)
	yellow := color.New(color.FgYellow)

	// Validate backend exists
	bold.Println("→ Validating backend...")
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("backend not initialized. Run 'scaffold init' first")
	}

	// Check environment doesn't already exist
	if cfg.GetEnvironment(envName) != nil {
		return fmt.Errorf("environment '%s' already exists", envName)
	}

	green.Printf("✓ Backend found: %s (account: %s)\n\n", cfg.Backend.S3Bucket, cfg.Backend.AccountID)

	// Environment configuration
	bold.Println("→ Environment Configuration")
	var targetAccountID, targetRegion, watchDir, triggerBranch string
	survey.AskOne(&survey.Input{Message: "Target AWS Account ID:"}, &targetAccountID,
		survey.WithValidator(survey.Required))
	survey.AskOne(&survey.Input{Message: "AWS Region:", Default: cfg.Backend.Region}, &targetRegion)
	survey.AskOne(&survey.Input{
		Message: fmt.Sprintf("Watch directory [infra/%s]:", envName),
		Default: fmt.Sprintf("infra/%s", envName),
	}, &watchDir)
	survey.AskOne(&survey.Input{Message: "Trigger branch:", Default: "main"}, &triggerBranch)
	fmt.Println()

	// Cross-account setup in backend account
	bold.Println("→ Cross-Account Access Setup")
	fmt.Printf("  Backend account (%s) needs to grant access to target account.\n\n", cfg.Backend.AccountID)

	backendCredMethod, backendProfile, err := ui.SelectAWSCredentials()
	if err != nil {
		return err
	}

	spinner := ui.NewSpinner("Verifying backend credentials...")
	spinner.Start()
	backendClient, backendIdentity, err := aws.NewClientWithCredentials(cfg.Backend.Region, backendCredMethod, backendProfile)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("backend AWS authentication failed: %w", err)
	}
	if backendIdentity.AccountID != cfg.Backend.AccountID {
		return fmt.Errorf("credentials belong to account %s, expected backend account %s",
			backendIdentity.AccountID, cfg.Backend.AccountID)
	}
	green.Printf("✓ Authenticated as: %s\n\n", backendIdentity.ARN)

	// Update backend policies
	fmt.Println("  Updating backend policies...")
	spinner = ui.NewSpinner("Updating S3 bucket policy...")
	spinner.Start()
	err = backendClient.AddSpokeAccountToS3Policy(cfg.Backend.S3Bucket, targetAccountID)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("failed to update S3 policy: %w", err)
	}
	green.Printf("✓ S3 bucket policy: Added principal arn:aws:iam::%s:root\n", targetAccountID)

	spinner = ui.NewSpinner("Updating KMS key policy...")
	spinner.Start()
	err = backendClient.AddSpokeAccountToKMSPolicy(cfg.Backend.KMSKeyID, targetAccountID)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("failed to update KMS policy: %w", err)
	}
	green.Printf("✓ KMS key policy: Added principal arn:aws:iam::%s:root\n", targetAccountID)

	spinner = ui.NewSpinner("Updating DynamoDB policy...")
	spinner.Start()
	err = backendClient.AddSpokeAccountToDynamoPolicy(cfg.Backend.DynamoDBTable, cfg.Backend.Region, cfg.Backend.AccountID, targetAccountID)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("failed to update DynamoDB policy: %w", err)
	}
	green.Printf("✓ DynamoDB policy: Added principal arn:aws:iam::%s:root\n\n", targetAccountID)

	// IAM role in target account
	bold.Println("→ IAM Role Configuration (Target Account)")
	targetCredMethod, targetProfile, err := ui.SelectAWSCredentials()
	if err != nil {
		return err
	}

	spinner = ui.NewSpinner("Verifying target credentials...")
	spinner.Start()
	targetClient, targetIdentity, err := aws.NewClientWithCredentials(targetRegion, targetCredMethod, targetProfile)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("target AWS authentication failed: %w", err)
	}
	if targetIdentity.AccountID != targetAccountID {
		return fmt.Errorf("credentials belong to account %s, expected target account %s",
			targetIdentity.AccountID, targetAccountID)
	}
	green.Printf("✓ Authenticated as: %s\n\n", targetIdentity.ARN)

	policyModeOptions := []string{
		"Inline policies (SCP-compliant, recommended)",
		"Managed policy attachments (simpler, may fail with SCPs)",
	}
	var policyModeChoice string
	survey.AskOne(&survey.Select{
		Message: "IAM policy mode:",
		Options: policyModeOptions,
		Default: policyModeOptions[0],
	}, &policyModeChoice)
	policyMode := "inline"
	if strings.Contains(policyModeChoice, "Managed") {
		policyMode = "managed"
	}

	fmt.Printf("\n  Creating IAM resources in account %s...\n", targetAccountID)

	// Create OIDC provider
	spinner = ui.NewSpinner("Creating OIDC provider...")
	spinner.Start()
	err = targetClient.EnsureOIDCProvider(cfg.Repository.Org, cfg.Repository.Name)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("failed to create OIDC provider: %w", err)
	}
	green.Println("✓ OIDC provider: token.actions.githubusercontent.com")

	// Create IAM role
	roleName := fmt.Sprintf("github-actions-%s", envName)
	spinner = ui.NewSpinner(fmt.Sprintf("Creating IAM role: %s...", roleName))
	spinner.Start()
	roleARN, err := targetClient.CreateGitHubActionsRole(
		roleName, envName, policyMode,
		cfg.Repository.Org, cfg.Repository.Name,
		cfg.Backend.S3Bucket, cfg.Backend.DynamoDBTable, cfg.Backend.KMSKeyID,
		cfg.Backend.Region, cfg.Backend.AccountID,
	)
	spinner.Stop()
	if err != nil {
		return fmt.Errorf("failed to create IAM role: %w", err)
	}
	green.Printf("✓ IAM role: %s\n", roleName)
	fmt.Printf("    - Trust policy: repo:%s/%s:environment:%s\n", cfg.Repository.Org, cfg.Repository.Name, envName)
	fmt.Printf("    - Inline policy: power-user-permissions\n")
	fmt.Printf("    - Backend access: cross-account S3/DynamoDB/KMS\n\n")

	// Generate workflow and providers.tf
	bold.Println("→ Generating Terraform configuration...")
	workflowFile := fmt.Sprintf(".github/workflows/terraform-%s.yaml", envName)
	stateKey := fmt.Sprintf("%s/terraform.tfstate", envName)

	err = terraform.GenerateWorkflow(terraform.WorkflowConfig{
		Environment:   envName,
		TriggerBranch: triggerBranch,
		WatchDir:      watchDir,
		AWSRegion:     targetRegion,
		S3Bucket:      cfg.Backend.S3Bucket,
		DynamoDBTable: cfg.Backend.DynamoDBTable,
		StateKey:      stateKey,
	})
	if err != nil {
		return fmt.Errorf("failed to generate workflow: %w", err)
	}
	green.Printf("✓ Workflow: %s\n", workflowFile)

	err = terraform.GenerateProvidersFile(watchDir, envName, targetRegion, cfg.Repository.Org, cfg.Repository.Name)
	if err != nil {
		return fmt.Errorf("failed to generate providers.tf: %w", err)
	}
	green.Printf("✓ providers.tf: %s/providers.tf\n", watchDir)

	// Update config
	env := config.Environment{
		Name:          envName,
		AccountID:     targetAccountID,
		Region:        targetRegion,
		WatchDir:      watchDir,
		TriggerBranch: triggerBranch,
		IAMRoleARN:    roleARN,
		StateKey:      stateKey,
		WorkflowFile:  workflowFile,
		PolicyMode:    policyMode,
	}
	cfg.Environments = append(cfg.Environments, env)
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	green.Println("✓ Config updated: .scaffold/config.json")

	// Display manual step
	fmt.Println()
	yellow.Println("→ Manual step required:")
	fmt.Println("  Add GitHub repository secret:")
	fmt.Println()
	secretName := fmt.Sprintf("AWS_ROLE_ARN_%s", strings.ToUpper(envName))
	bold.Printf("  Name:  %s\n", secretName)
	bold.Printf("  Value: %s\n", roleARN)
	fmt.Println()
	fmt.Println("  GitHub Settings → Secrets → Actions → New repository secret")
	fmt.Println()

	green.Printf("→ Environment '%s' created!\n\n", envName)
	fmt.Println("  Next steps:")
	fmt.Println("  1. Add the GitHub secret above")
	fmt.Println("  2. Review changes: git status")
	fmt.Printf("  3. Commit: git add . && git commit -m \"feat: add %s environment\"\n", envName)
	fmt.Printf("  4. Push: git push origin %s\n", triggerBranch)
	fmt.Println("  5. Workflow will trigger automatically on next push")

	return nil
}
