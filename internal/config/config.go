package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	ConfigDir  = ".scaffold"
	ConfigFile = ".scaffold/config.json"
	Version    = "1.0"
)

// Config is the top-level Scaffold configuration.
type Config struct {
	Version      string        `json:"version"`
	Backend      Backend       `json:"backend"`
	Repository   Repository    `json:"repository"`
	Environments []Environment `json:"environments"`
}

// Backend holds the centralized state backend configuration.
type Backend struct {
	AccountID     string `json:"account_id"`
	Region        string `json:"region"`
	S3Bucket      string `json:"s3_bucket"`
	DynamoDBTable string `json:"dynamodb_table"`
	KMSKeyID      string `json:"kms_key_id"`
	CreatedAt     string `json:"created_at"`
	CreatedBy     string `json:"created_by"`
}

// Repository holds GitHub repository information.
type Repository struct {
	Org           string `json:"org"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
}

// Environment holds per-environment configuration.
type Environment struct {
	Name          string `json:"name"`
	AccountID     string `json:"account_id"`
	Region        string `json:"region"`
	WatchDir      string `json:"watch_directory"`
	TriggerBranch string `json:"trigger_branch"`
	IAMRoleARN    string `json:"iam_role_arn"`
	StateKey      string `json:"state_key"`
	WorkflowFile  string `json:"workflow_file"`
	CreatedAt     string `json:"created_at"`
	PolicyMode    string `json:"policy_mode"`
}

// DetectedRepo holds auto-detected repository info.
type DetectedRepo struct {
	Org           string
	Name          string
	DefaultBranch string
}

// Load reads the config from disk. Returns nil, nil if not found.
func Load() (*Config, error) {
	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config not found: run 'scaffold init' first")
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config file: %w", err)
	}
	return &cfg, nil
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	if err := os.MkdirAll(ConfigDir, 0755); err != nil {
		return err
	}
	cfg.Backend.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigFile, data, 0600)
}

// GetEnvironment returns the environment config by name, or nil.
func (c *Config) GetEnvironment(name string) *Environment {
	for i := range c.Environments {
		if c.Environments[i].Name == name {
			return &c.Environments[i]
		}
	}
	return nil
}

// RemoveEnvironment removes the environment config by name.
func (c *Config) RemoveEnvironment(name string) {
	envs := make([]Environment, 0, len(c.Environments))
	for _, e := range c.Environments {
		if e.Name != name {
			envs = append(envs, e)
		}
	}
	c.Environments = envs
}

// DetectRepository auto-detects org and repo name from git remote.
func DetectRepository() (*DetectedRepo, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return nil, fmt.Errorf("not in a git repository or no origin remote")
	}
	remoteURL := strings.TrimSpace(string(out))

	// Parse GitHub URL formats:
	// https://github.com/org/repo.git
	// git@github.com:org/repo.git
	remoteURL = strings.TrimSuffix(remoteURL, ".git")
	var org, name string
	if strings.Contains(remoteURL, "github.com/") {
		parts := strings.Split(remoteURL, "github.com/")
		if len(parts) == 2 {
			orgRepo := strings.Split(parts[1], "/")
			if len(orgRepo) == 2 {
				org = orgRepo[0]
				name = orgRepo[1]
			}
		}
	} else if strings.Contains(remoteURL, "github.com:") {
		parts := strings.Split(remoteURL, "github.com:")
		if len(parts) == 2 {
			orgRepo := strings.Split(parts[1], "/")
			if len(orgRepo) == 2 {
				org = orgRepo[0]
				name = orgRepo[1]
			}
		}
	}

	if org == "" || name == "" {
		return nil, fmt.Errorf("could not parse GitHub repository from remote URL: %s", remoteURL)
	}

	// Detect default branch
	defaultBranch := "main"
	branchOut, err := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		branch := strings.TrimSpace(string(branchOut))
		branch = filepath.Base(branch)
		if branch != "" {
			defaultBranch = branch
		}
	}

	return &DetectedRepo{
		Org:           org,
		Name:          name,
		DefaultBranch: defaultBranch,
	}, nil
}
