package terraform

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// WorkflowConfig holds the parameters for generating a GitHub Actions workflow.
type WorkflowConfig struct {
	Environment   string
	TriggerBranch string
	WatchDir      string
	AWSRegion     string
	S3Bucket      string
	DynamoDBTable string
	StateKey      string
}

const workflowTemplate = `name: Terraform - {{.EnvironmentTitle}}

on:
  push:
    branches:
      - {{.TriggerBranch}}
    paths:
      - '{{.WatchDir}}/**/*.tf'
      - '{{.WatchDir}}/**/*.tfvars'
  workflow_dispatch:

permissions:
  id-token: write
  contents: read

env:
  AWS_REGION: {{.AWSRegion}}
  TF_BUCKET: {{.S3Bucket}}
  TF_LOCK_TABLE: {{.DynamoDBTable}}
  TF_STATE_KEY: {{.StateKey}}

jobs:
  terraform:
    name: Terraform Apply
    runs-on: ubuntu-latest
    environment: {{.Environment}}

    defaults:
      run:
        working-directory: {{.WatchDir}}

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Debug OIDC Token
        run: |
          TOKEN=$(curl -sSfL -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
            "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=sts.amazonaws.com" | jq -r '.value')
          echo "=== OIDC sub claim (must match IAM trust policy) ==="
          echo "$TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null | jq -r '.sub'

      - name: Configure AWS Credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{"{{"}} secrets.AWS_ROLE_ARN_{{.EnvironmentUpper}} {{"}}"}}
          aws-region: ${{"{{"}} env.AWS_REGION {{"}}"}}
          audience: sts.amazonaws.com

      - name: Setup Terraform
        uses: hashicorp/setup-terraform@v3
        with:
          terraform_version: ~> 1.7

      - name: Terraform Init
        run: |
          terraform init \
            -backend-config="bucket=${{"{{"}} env.TF_BUCKET {{"}}"}}" \
            -backend-config="key=${{"{{"}} env.TF_STATE_KEY {{"}}"}}" \
            -backend-config="region=${{"{{"}} env.AWS_REGION {{"}}"}}" \
            -backend-config="dynamodb_table=${{"{{"}} env.TF_LOCK_TABLE {{"}}"}}" \
            -backend-config="encrypt=true"

      - name: Terraform Validate
        run: terraform validate

      - name: Terraform Plan
        run: terraform plan -out=tfplan

      - name: Terraform Apply
        run: terraform apply -auto-approve tfplan
`

const providersTemplate = `terraform {
  required_version = ">= 1.7.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  backend "s3" {}
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Environment = "{{.Environment}}"
      ManagedBy   = "Terraform"
      Repository  = "{{.GithubOrg}}/{{.GithubRepo}}"
    }
  }
}

variable "aws_region" {
  description = "AWS region for resources"
  type        = string
  default     = "{{.AWSRegion}}"
}
`

type workflowVars struct {
	Environment      string
	EnvironmentTitle string
	EnvironmentUpper string
	TriggerBranch    string
	WatchDir         string
	AWSRegion        string
	S3Bucket         string
	DynamoDBTable    string
	StateKey         string
}

type providersVars struct {
	Environment string
	GithubOrg   string
	GithubRepo  string
	AWSRegion   string
}

// GenerateWorkflow creates the GitHub Actions workflow file.
func GenerateWorkflow(cfg WorkflowConfig) error {
	workflowDir := ".github/workflows"
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return fmt.Errorf("failed to create workflow directory: %w", err)
	}

	filePath := filepath.Join(workflowDir, fmt.Sprintf("terraform-%s.yaml", cfg.Environment))

	vars := workflowVars{
		Environment:      cfg.Environment,
		EnvironmentTitle: cases.Title(language.English).String(cfg.Environment),
		EnvironmentUpper: strings.ToUpper(cfg.Environment),
		TriggerBranch:    cfg.TriggerBranch,
		WatchDir:         cfg.WatchDir,
		AWSRegion:        cfg.AWSRegion,
		S3Bucket:         cfg.S3Bucket,
		DynamoDBTable:    cfg.DynamoDBTable,
		StateKey:         cfg.StateKey,
	}

	tmpl, err := template.New("workflow").Parse(workflowTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse workflow template: %w", err)
	}

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create workflow file: %w", err)
	}
	defer f.Close()

	return tmpl.Execute(f, vars)
}

// GenerateProvidersFile creates the Terraform providers.tf file.
func GenerateProvidersFile(watchDir, envName, region, githubOrg, githubRepo string) error {
	if err := os.MkdirAll(watchDir, 0755); err != nil {
		return fmt.Errorf("failed to create watch directory: %w", err)
	}

	filePath := filepath.Join(watchDir, "providers.tf")

	// Don't overwrite if exists
	if _, err := os.Stat(filePath); err == nil {
		return nil
	}

	vars := providersVars{
		Environment: envName,
		GithubOrg:   githubOrg,
		GithubRepo:  githubRepo,
		AWSRegion:   region,
	}

	tmpl, err := template.New("providers").Parse(providersTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse providers template: %w", err)
	}

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create providers.tf: %w", err)
	}
	defer f.Close()

	return tmpl.Execute(f, vars)
}

// Runner wraps terraform CLI commands.
type Runner struct {
	workDir       string
	bucket        string
	stateKey      string
	region        string
	dynamoDBTable string
	kmsKeyID      string
	env           []string
}

// NewRunner creates a new Terraform runner.
func NewRunner(workDir, bucket, stateKey, region, dynamoDBTable, kmsKeyID string, env []string) *Runner {
	return &Runner{
		workDir:       workDir,
		bucket:        bucket,
		stateKey:      stateKey,
		region:        region,
		dynamoDBTable: dynamoDBTable,
		kmsKeyID:      kmsKeyID,
		env:           env,
	}
}

// Init runs terraform init.
func (r *Runner) Init() error {
	args := []string{
		"init",
		fmt.Sprintf("-backend-config=bucket=%s", r.bucket),
		fmt.Sprintf("-backend-config=key=%s", r.stateKey),
		fmt.Sprintf("-backend-config=region=%s", r.region),
		fmt.Sprintf("-backend-config=dynamodb_table=%s", r.dynamoDBTable),
	}
	if r.kmsKeyID != "" {
		args = append(args, fmt.Sprintf("-backend-config=kms_key_id=%s", r.kmsKeyID))
	}
	args = append(args, "-backend-config=encrypt=true", "-reconfigure", "-input=false")

	cmd := exec.Command("terraform", args...)
	cmd.Dir = r.workDir
	if len(r.env) > 0 {
		cmd.Env = r.env
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// PlanDestroy generates a destroy plan and returns resources to be destroyed.
func (r *Runner) PlanDestroy() ([]string, string, error) {
	cmd := exec.Command("terraform", "plan", "-destroy", "-out=tfplan", "-input=false")
	cmd.Dir = r.workDir
	if len(r.env) > 0 {
		cmd.Env = r.env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, string(out), fmt.Errorf("terraform plan failed: %w\n%s", err, out)
	}

	// Parse planned destroys from JSON plan for robust detection.
	showCmd := exec.Command("terraform", "show", "-json", "tfplan")
	showCmd.Dir = r.workDir
	if len(r.env) > 0 {
		showCmd.Env = r.env
	}
	showOut, showErr := showCmd.CombinedOutput()
	if showErr == nil {
		var plan struct {
			ResourceChanges []struct {
				Address string `json:"address"`
				Change  struct {
					Actions []string `json:"actions"`
				} `json:"change"`
			} `json:"resource_changes"`
		}
		if json.Unmarshal(showOut, &plan) == nil {
			var resources []string
			for _, rc := range plan.ResourceChanges {
				if hasAction(rc.Change.Actions, "delete") && rc.Address != "" {
					resources = append(resources, rc.Address)
				}
			}
			return resources, string(out), nil
		}
	}

	// Fallback parser for older terraform output.
	var resources []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "# ") && strings.Contains(line, "will be destroyed") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				resources = append(resources, strings.TrimPrefix(parts[1], "#"))
			}
		}
	}
	return resources, string(out), nil
}

// Destroy runs terraform destroy.
func (r *Runner) Destroy() error {
	cmd := exec.Command("terraform", "apply", "-destroy", "-auto-approve", "-input=false")
	cmd.Dir = r.workDir
	if len(r.env) > 0 {
		cmd.Env = r.env
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func hasAction(actions []string, target string) bool {
	for _, a := range actions {
		if a == target {
			return true
		}
	}
	return false
}
