package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// CredentialMethod represents how AWS credentials are provided.
type CredentialMethod string

const (
	CredentialProfile CredentialMethod = "profile"
	CredentialEnvVars CredentialMethod = "env"
	CredentialSSO     CredentialMethod = "sso"
)

// Identity holds the authenticated caller's identity.
type Identity struct {
	ARN       string
	AccountID string
	UserID    string
}

// Client wraps AWS SDK clients for Scaffold operations.
type Client struct {
	cfg    aws.Config
	region string
}

// NewClientWithCredentials creates a new AWS client with the specified credential method.
func NewClientWithCredentials(region string, method CredentialMethod, profile string) (*Client, *Identity, error) {
	ctx := context.Background()
	var opts []func(*config.LoadOptions) error

	opts = append(opts, config.WithRegion(region))

	switch method {
	case CredentialProfile:
		if profile != "" {
			opts = append(opts, config.WithSharedConfigProfile(profile))
		}
	case CredentialEnvVars:
		// Default SDK behavior already reads env vars
	case CredentialSSO:
		if profile != "" {
			opts = append(opts, config.WithSharedConfigProfile(profile))
		}
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := &Client{cfg: cfg, region: region}

	// Verify identity
	stsClient := sts.NewFromConfig(cfg)
	result, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to verify AWS credentials: %w", err)
	}

	identity := &Identity{
		ARN:       aws.ToString(result.Arn),
		AccountID: aws.ToString(result.Account),
		UserID:    aws.ToString(result.UserId),
	}

	return client, identity, nil
}
