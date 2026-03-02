package ui

import (
	"fmt"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
	"github.com/scaffold-tool/scaffold/internal/aws"
)

// Spinner is a simple terminal spinner.
type Spinner struct {
	message string
	done    chan struct{}
}

// NewSpinner creates a new spinner with the given message.
func NewSpinner(message string) *Spinner {
	return &Spinner{message: message, done: make(chan struct{})}
}

// Start begins the spinner animation.
func (s *Spinner) Start() {
	go func() {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		cyan := color.New(color.FgCyan)
		for {
			select {
			case <-s.done:
				fmt.Print("\r\033[K")
				return
			default:
				cyan.Printf("\r  %s %s", frames[i%len(frames)], s.message)
				i++
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
}

// Stop halts the spinner.
func (s *Spinner) Stop() {
	s.done <- struct{}{}
}

// SelectAWSCredentials prompts the user to choose an AWS credential method.
func SelectAWSCredentials() (aws.CredentialMethod, string, error) {
	options := []string{
		"Use AWS CLI profile",
		"Use environment variables",
		"AWS SSO session",
	}

	var choice string
	if err := survey.AskOne(&survey.Select{
		Message: "AWS Credentials:",
		Options: options,
		Default: options[0],
	}, &choice); err != nil {
		return "", "", err
	}

	switch choice {
	case options[0]:
		var profile string
		if err := survey.AskOne(&survey.Input{
			Message: "AWS Profile:",
			Default: "default",
		}, &profile); err != nil {
			return "", "", err
		}
		return aws.CredentialProfile, profile, nil

	case options[1]:
		return aws.CredentialEnvVars, "", nil

	case options[2]:
		var session string
		if err := survey.AskOne(&survey.Input{
			Message: "SSO Session name:",
		}, &session); err != nil {
			return "", "", err
		}
		return aws.CredentialSSO, session, nil
	}

	return aws.CredentialEnvVars, "", nil
}
