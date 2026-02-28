package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/scaffold-tool/scaffold/internal/aws"
	"github.com/scaffold-tool/scaffold/internal/config"
	ghclient "github.com/scaffold-tool/scaffold/internal/github"
	"github.com/scaffold-tool/scaffold/internal/ui"
	"github.com/spf13/cobra"
)

var (
	statusAll      bool
	statusJSON     bool
	statusWatch    bool
	statusNoGitHub bool
)

var statusCmd = &cobra.Command{
	Use:   "status [environment-name]",
	Short: "Display environment status, resource inventory, and workflow history",
	Long: `Show detailed status for an environment including:
  - Configuration summary
  - State backend info (S3 path, lock status, version)
  - Resource inventory from state file
  - Recent GitHub Actions workflow runs
  - Workflow statistics

Use --all to see a summary of all environments.`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVar(&statusAll, "all", false, "Show summary of all environments")
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output as JSON")
	statusCmd.Flags().BoolVar(&statusWatch, "watch", false, "Live updates (refresh every 5s)")
	statusCmd.Flags().BoolVar(&statusNoGitHub, "no-github", false, "Skip GitHub API calls (faster, less info)")
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("scaffold not initialized")
	}

	if statusWatch {
		for {
			fmt.Print("\033[H\033[2J") // clear screen
			if err := displayStatus(cfg, args); err != nil {
				return err
			}
			fmt.Println("\n  (Refreshing every 5s. Ctrl+C to exit)")
			time.Sleep(5 * time.Second)
		}
	}

	return displayStatus(cfg, args)
}

func displayStatus(cfg *config.Config, args []string) error {
	green := color.New(color.FgGreen)
	red := color.New(color.FgRed)
	yellow := color.New(color.FgYellow)

	credMethod, profile, err := ui.SelectAWSCredentials()
	if err != nil {
		return err
	}
	backendClient, _, err := aws.NewClientWithCredentials(cfg.Backend.Region, credMethod, profile)
	if err != nil {
		return fmt.Errorf("AWS authentication failed: %w", err)
	}

	if statusAll || len(args) == 0 {
		return displayAllEnvironments(cfg, backendClient, green, red, yellow)
	}

	envName := args[0]
	env := cfg.GetEnvironment(envName)
	if env == nil {
		return fmt.Errorf("environment '%s' not found", envName)
	}

	return displayEnvironmentDetail(cfg, env, backendClient)
}

func displayAllEnvironments(cfg *config.Config, client *aws.Client, green, red, yellow *color.Color) error {
	fmt.Println("All Environments")
	fmt.Println(strings.Repeat("─", 68))

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Name", "Account", "Resources", "Status", "Last Run"})
	table.SetBorder(false)
	table.SetColumnSeparator("  ")

	for _, env := range cfg.Environments {
		resources, err := client.GetStateResources(cfg.Backend.S3Bucket, env.StateKey)
		resourceCount := len(resources)
		status := "✓ Healthy"
		if err != nil {
			status = "? Unknown"
		} else if resourceCount == 0 {
			status = "⊗ Empty"
		}

		lock, _ := client.GetStateLock(cfg.Backend.DynamoDBTable, cfg.Backend.S3Bucket, env.StateKey)
		if lock != nil {
			status = "⚠ Locked"
		}

		table.Append([]string{
			env.Name,
			env.AccountID,
			fmt.Sprintf("%d", resourceCount),
			status,
			"N/A",
		})
	}
	table.Render()

	fmt.Println()
	fmt.Println("Backend Status")
	fmt.Println(strings.Repeat("─", 68))
	fmt.Printf("Account:       %s\n", cfg.Backend.AccountID)
	fmt.Printf("S3 Bucket:     %s\n", cfg.Backend.S3Bucket)
	fmt.Printf("State Files:   %d\n", len(cfg.Environments))
	_ = green
	_ = red
	_ = yellow
	return nil
}

func displayEnvironmentDetail(cfg *config.Config, env *config.Environment, client *aws.Client) error {
	sep := strings.Repeat("─", 52)

	fmt.Printf("Environment: %s\n", env.Name)
	fmt.Println(sep)
	fmt.Printf("Repository:     %s/%s\n", cfg.Repository.Org, cfg.Repository.Name)
	fmt.Printf("Backend:        %s (%s)\n", cfg.Backend.S3Bucket, cfg.Backend.AccountID)
	fmt.Printf("Target Account: %s\n", env.AccountID)
	fmt.Printf("Region:         %s\n", env.Region)
	fmt.Printf("Watch Dir:      %s\n", env.WatchDir)
	fmt.Printf("Trigger Branch: %s\n", env.TriggerBranch)
	fmt.Printf("IAM Role:       %s\n", env.IAMRoleARN)
	fmt.Println()

	// State info
	fmt.Println("State Backend")
	fmt.Println(sep)
	stateInfo, err := client.GetStateInfo(cfg.Backend.S3Bucket, env.StateKey)
	if err != nil {
		color.Yellow("⚠  Could not retrieve state info: %v\n", err)
	} else {
		fmt.Printf("S3 State File:  s3://%s/%s\n", cfg.Backend.S3Bucket, env.StateKey)
		fmt.Printf("Last Modified:  %s\n", stateInfo.LastModified)
		fmt.Printf("State Version:  %d\n", stateInfo.Version)
		fmt.Printf("Serial:         %d\n", stateInfo.Serial)
	}

	lock, _ := client.GetStateLock(cfg.Backend.DynamoDBTable, cfg.Backend.S3Bucket, env.StateKey)
	if lock != nil {
		color.Yellow("Lock Status:    ⚠ LOCKED (by %s)\n", lock.Who)
	} else {
		color.Green("Lock Status:    ✓ Unlocked\n")
	}
	fmt.Println()

	// Resources
	resources, err := client.GetStateResources(cfg.Backend.S3Bucket, env.StateKey)
	if err != nil {
		color.Yellow("⚠  Could not retrieve resources: %v\n", err)
	} else {
		fmt.Printf("Resource Inventory (%d total)\n", len(resources))
		fmt.Println(sep)
		if len(resources) == 0 {
			fmt.Println("  No resources in state.")
		} else {
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Type", "Name", "Identifier"})
			table.SetBorder(false)
			for _, r := range resources {
				parts := strings.SplitN(r, ".", 2)
				rType, rName := "unknown", r
				if len(parts) == 2 {
					rType = parts[0]
					rName = parts[1]
				}
				table.Append([]string{rType, rName, ""})
			}
			table.Render()
		}
		fmt.Println()
	}

	// GitHub workflow runs
	if !statusNoGitHub {
		fmt.Println("Recent Workflow Runs (Last 10)")
		fmt.Println(sep)
		ghToken := os.Getenv("GITHUB_TOKEN")
		if ghToken == "" {
			color.Yellow("⚠  GITHUB_TOKEN not set. Skipping workflow history.\n")
			fmt.Println("  Set GITHUB_TOKEN or use --no-github to suppress this message.")
		} else {
			ghClient := ghclient.NewClient(ghToken, cfg.Repository.Org, cfg.Repository.Name)
			runs, err := ghClient.GetWorkflowRuns(env.WorkflowFile, 10)
			if err != nil {
				color.Yellow("⚠  Could not fetch workflow runs: %v\n", err)
			} else {
				table := tablewriter.NewWriter(os.Stdout)
				table.SetHeader([]string{"Run", "Commit", "Status", "Duration", "Date"})
				table.SetBorder(false)
				for _, run := range runs {
					statusIcon := "✓ Success"
					if run.Conclusion == "failure" {
						statusIcon = "✗ Failed"
					} else if run.Conclusion == "skipped" {
						statusIcon = "⊗ Skipped"
					}
					table.Append([]string{
						fmt.Sprintf("#%d", run.RunNumber),
						run.HeadCommitMessage,
						statusIcon,
						run.Duration,
						run.CreatedAt,
					})
				}
				table.Render()
			}
		}
	}

	if statusJSON {
		out, _ := json.MarshalIndent(env, "", "  ")
		fmt.Println(string(out))
	}

	return nil
}
