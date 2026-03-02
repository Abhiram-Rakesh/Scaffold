package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

const (
	githubOIDCURL        = "https://token.actions.githubusercontent.com"
	githubOIDCThumbprint1 = "6938fd4d98bab03faadb97b34396831e3780aea1"
	githubOIDCThumbprint2 = "1c58a3a8518e8759bf075b76b750d4f2df264fcd"
)

// EnsureOIDCProvider creates the GitHub Actions OIDC provider if it doesn't exist.
func (c *Client) EnsureOIDCProvider(githubOrg, githubRepo string) error {
	ctx := context.Background()
	svc := iam.NewFromConfig(c.cfg)

	// Check if already exists
	providers, err := svc.ListOpenIDConnectProviders(ctx, &iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return err
	}
	for _, p := range providers.OpenIDConnectProviderList {
		info, err := svc.GetOpenIDConnectProvider(ctx, &iam.GetOpenIDConnectProviderInput{
			OpenIDConnectProviderArn: p.Arn,
		})
		if err == nil && sameOIDCProviderURL(aws.ToString(info.Url), githubOIDCURL) {
			return nil // already exists
		}
	}

	// Create OIDC provider
	if _, err := svc.CreateOpenIDConnectProvider(ctx, &iam.CreateOpenIDConnectProviderInput{
		Url:            aws.String(githubOIDCURL),
		ClientIDList:   []string{"sts.amazonaws.com"},
		ThumbprintList: []string{githubOIDCThumbprint1, githubOIDCThumbprint2},
		Tags: []iamtypes.Tag{
			{Key: aws.String("Name"), Value: aws.String("GitHub Actions OIDC")},
			{Key: aws.String("ManagedBy"), Value: aws.String("Scaffold")},
		},
	}); err != nil {
		return fmt.Errorf("failed to create OIDC provider: %w", err)
	}
	return nil
}

// CreateGitHubActionsRole creates an IAM role for GitHub Actions with the appropriate policies.
func (c *Client) CreateGitHubActionsRole(
	roleName, envName, policyMode,
	githubOrg, githubRepo,
	backendBucket, backendTable, backendKMSKey,
	backendRegion, backendAccountID string,
) (string, error) {
	ctx := context.Background()
	svc := iam.NewFromConfig(c.cfg)

	// Get OIDC provider ARN
	providers, err := svc.ListOpenIDConnectProviders(ctx, &iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return "", err
	}
	var oidcARN string
	for _, p := range providers.OpenIDConnectProviderList {
		info, err := svc.GetOpenIDConnectProvider(ctx, &iam.GetOpenIDConnectProviderInput{
			OpenIDConnectProviderArn: p.Arn,
		})
		if err == nil && sameOIDCProviderURL(aws.ToString(info.Url), githubOIDCURL) {
			oidcARN = aws.ToString(p.Arn)
			break
		}
	}
	if oidcARN == "" {
		return "", fmt.Errorf("OIDC provider not found")
	}

	// Build trust policy
	trustPolicy := map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []interface{}{
			map[string]interface{}{
				"Effect": "Allow",
				"Principal": map[string]interface{}{
					"Federated": oidcARN,
				},
				"Action": "sts:AssumeRoleWithWebIdentity",
				"Condition": map[string]interface{}{
					"StringEquals": map[string]interface{}{
						"token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
					},
					"StringLike": map[string]interface{}{
						"token.actions.githubusercontent.com:sub": []string{
							fmt.Sprintf("repo:%s/%s:ref:refs/heads/*", githubOrg, githubRepo),
							fmt.Sprintf("repo:%s/%s:environment:*", githubOrg, githubRepo),
						},
					},
				},
			},
		},
	}
	trustPolicyJSON, _ := json.Marshal(trustPolicy)

	// Check if role already exists
	var roleARN string
	existingRole, err := svc.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(roleName)})
	if err == nil {
		roleARN = aws.ToString(existingRole.Role.Arn)
	} else {
		// Create role
		result, err := svc.CreateRole(ctx, &iam.CreateRoleInput{
			RoleName:                 aws.String(roleName),
			AssumeRolePolicyDocument: aws.String(string(trustPolicyJSON)),
			Tags: []iamtypes.Tag{
				{Key: aws.String("Name"), Value: aws.String(fmt.Sprintf("GitHub Actions Role - %s", envName))},
				{Key: aws.String("ManagedBy"), Value: aws.String("Scaffold")},
				{Key: aws.String("Environment"), Value: aws.String(envName)},
			},
		})
		if err != nil {
			return "", fmt.Errorf("failed to create IAM role: %w", err)
		}
		roleARN = aws.ToString(result.Role.Arn)
	}

	if policyMode == "inline" {
		// Attach power user inline policy
		powerUserPolicy := buildPowerUserInlinePolicy()
		powerUserJSON, _ := json.Marshal(powerUserPolicy)
		if _, err := svc.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
			RoleName:       aws.String(roleName),
			PolicyName:     aws.String("power-user-permissions"),
			PolicyDocument: aws.String(string(powerUserJSON)),
		}); err != nil {
			return roleARN, fmt.Errorf("failed to attach power user policy: %w", err)
		}

		// Attach backend access inline policy
		backendPolicy := buildBackendAccessPolicy(backendBucket, backendTable, backendKMSKey, backendRegion, backendAccountID, envName)
		backendJSON, _ := json.Marshal(backendPolicy)
		if _, err := svc.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
			RoleName:       aws.String(roleName),
			PolicyName:     aws.String("terraform-backend-access"),
			PolicyDocument: aws.String(string(backendJSON)),
		}); err != nil {
			return roleARN, fmt.Errorf("failed to attach backend access policy: %w", err)
		}
	} else {
		// Managed policy attachment
		if _, err := svc.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: aws.String("arn:aws:iam::aws:policy/PowerUserAccess"),
		}); err != nil {
			return roleARN, fmt.Errorf("failed to attach managed policy: %w", err)
		}
	}

	return roleARN, nil
}

func buildPowerUserInlinePolicy() map[string]interface{} {
	return map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []interface{}{
			map[string]interface{}{
				"Sid":      "PowerUserAccess",
				"Effect":   "Allow",
				"Action":   []string{"*"},
				"Resource": "*",
			},
			map[string]interface{}{
				"Sid":    "DenyIAMAndOrgs",
				"Effect": "Deny",
				"Action": []string{"iam:*", "organizations:*", "account:*"},
				"Resource": "*",
			},
			map[string]interface{}{
				"Sid":    "AllowIAMRead",
				"Effect": "Allow",
				"Action": []string{"iam:Get*", "iam:List*", "iam:Describe*"},
				"Resource": "*",
			},
		},
	}
}

func buildBackendAccessPolicy(bucket, table, kmsKey, region, accountID, envName string) map[string]interface{} {
	kmsResource := normalizeKMSResource(kmsKey, region, accountID)

	return map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []interface{}{
			map[string]interface{}{
				"Sid":    "S3StateAccess",
				"Effect": "Allow",
				"Action": []string{
					"s3:ListBucket",
					"s3:GetObject",
					"s3:PutObject",
					"s3:DeleteObject",
				},
				"Resource": []string{
					fmt.Sprintf("arn:aws:s3:::%s", bucket),
					fmt.Sprintf("arn:aws:s3:::%s/%s/*", bucket, envName),
				},
			},
			map[string]interface{}{
				"Sid":    "DynamoDBLockAccess",
				"Effect": "Allow",
				"Action": []string{
					"dynamodb:PutItem",
					"dynamodb:GetItem",
					"dynamodb:DeleteItem",
					"dynamodb:DescribeTable",
				},
				"Resource": fmt.Sprintf("arn:aws:dynamodb:%s:%s:table/%s", region, accountID, table),
			},
			map[string]interface{}{
				"Sid":    "KMSKeyAccess",
				"Effect": "Allow",
				"Action": []string{
					"kms:Decrypt",
					"kms:Encrypt",
					"kms:GenerateDataKey",
					"kms:DescribeKey",
				},
				"Resource": kmsResource,
			},
			map[string]interface{}{
				"Sid":      "GetCallerIdentity",
				"Effect":   "Allow",
				"Action":   []string{"sts:GetCallerIdentity"},
				"Resource": "*",
			},
		},
	}
}

func normalizeKMSResource(kmsKey, region, accountID string) string {
	if kmsKey == "" || kmsKey == "*" {
		return "*"
	}
	// Already an ARN (key or alias ARN).
	if strings.HasPrefix(kmsKey, "arn:") {
		return kmsKey
	}
	// Alias name provided (e.g., alias/terraform-state-myrepo).
	if strings.HasPrefix(kmsKey, "alias/") {
		return fmt.Sprintf("arn:aws:kms:%s:%s:%s", region, accountID, kmsKey)
	}
	// Raw key ID provided; convert to key ARN.
	return fmt.Sprintf("arn:aws:kms:%s:%s:key/%s", region, accountID, kmsKey)
}

// urlEncode is used for URL-encoding policy documents
var _ = url.QueryEscape

func sameOIDCProviderURL(a, b string) bool {
	return normalizeOIDCProviderURL(a) == normalizeOIDCProviderURL(b)
}

func normalizeOIDCProviderURL(u string) string {
	normalized := strings.TrimSpace(strings.ToLower(u))
	normalized = strings.TrimPrefix(normalized, "https://")
	normalized = strings.TrimPrefix(normalized, "http://")
	normalized = strings.TrimSuffix(normalized, "/")
	return normalized
}
