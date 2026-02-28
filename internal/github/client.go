package github

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	gh "github.com/google/go-github/v58/github"
	"golang.org/x/oauth2"
)

// WorkflowRun holds information about a GitHub Actions workflow run.
type WorkflowRun struct {
	RunNumber         int
	HeadCommitMessage string
	Status            string
	Conclusion        string
	Duration          string
	CreatedAt         string
}

// Client wraps the GitHub API.
type Client struct {
	client *gh.Client
	org    string
	repo   string
}

// NewClient creates a new GitHub API client.
func NewClient(token, org, repo string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	return &Client{
		client: gh.NewClient(tc),
		org:    org,
		repo:   repo,
	}
}

// GetWorkflowRuns returns recent workflow runs for a workflow file.
func (c *Client) GetWorkflowRuns(workflowFile string, limit int) ([]WorkflowRun, error) {
	ctx := context.Background()

	// Use the filename, not the full path
	fileName := filepath.Base(workflowFile)

	opts := &gh.ListWorkflowRunsOptions{
		ListOptions: gh.ListOptions{PerPage: limit},
	}

	runs, _, err := c.client.Actions.ListWorkflowRunsByFileName(ctx, c.org, c.repo, fileName, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflow runs: %w", err)
	}

	var result []WorkflowRun
	for _, run := range runs.WorkflowRuns {
		duration := "N/A"
		if run.UpdatedAt != nil && run.CreatedAt != nil {
			d := run.UpdatedAt.Sub(run.CreatedAt.Time)
			duration = formatDuration(d)
		}

		commitMsg := ""
		if run.HeadCommit != nil && run.HeadCommit.Message != nil {
			commitMsg = *run.HeadCommit.Message
			if len(commitMsg) > 40 {
				commitMsg = commitMsg[:40] + "..."
			}
		}

		createdAt := "N/A"
		if run.CreatedAt != nil {
			createdAt = timeAgo(run.CreatedAt.Time)
		}

		result = append(result, WorkflowRun{
			RunNumber:         int(run.GetRunNumber()),
			HeadCommitMessage: commitMsg,
			Status:            run.GetStatus(),
			Conclusion:        run.GetConclusion(),
			Duration:          duration,
			CreatedAt:         createdAt,
		})
	}

	return result, nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", m, s)
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}
