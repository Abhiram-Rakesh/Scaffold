package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

// CreateKMSKey creates a KMS key for Terraform state encryption.
func (c *Client) CreateKMSKey(repoName, githubOrg, githubRepo string) (string, error) {
	ctx := context.Background()
	svc := kms.NewFromConfig(c.cfg)

	// Check if alias already exists
	aliasName := fmt.Sprintf("alias/terraform-state-%s", repoName)
	aliases, err := svc.ListAliases(ctx, &kms.ListAliasesInput{})
	if err == nil {
		for _, alias := range aliases.Aliases {
			if aws.ToString(alias.AliasName) == aliasName {
				return aws.ToString(alias.TargetKeyId), nil
			}
		}
	}

	// Create key
	result, err := svc.CreateKey(ctx, &kms.CreateKeyInput{
		Description:           aws.String("Terraform state encryption key"),
		KeyUsage:              kmstypes.KeyUsageTypeEncryptDecrypt,
		EnableKeyRotation:     aws.Bool(true),
		DeletionWindowInDays:  aws.Int32(7),
		Tags: []kmstypes.Tag{
			{TagKey: aws.String("Name"), TagValue: aws.String("Terraform State Encryption")},
			{TagKey: aws.String("ManagedBy"), TagValue: aws.String("Scaffold")},
			{TagKey: aws.String("Repository"), TagValue: aws.String(fmt.Sprintf("%s/%s", githubOrg, githubRepo))},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create KMS key: %w", err)
	}

	keyID := aws.ToString(result.KeyMetadata.KeyId)

	// Create alias
	if _, err := svc.CreateAlias(ctx, &kms.CreateAliasInput{
		AliasName:   aws.String(aliasName),
		TargetKeyId: aws.String(keyID),
	}); err != nil {
		return keyID, fmt.Errorf("failed to create KMS alias: %w", err)
	}

	return keyID, nil
}

// AddSpokeAccountToKMSPolicy adds a spoke account to the KMS key policy.
func (c *Client) AddSpokeAccountToKMSPolicy(keyID, spokeAccountID string) error {
	ctx := context.Background()
	svc := kms.NewFromConfig(c.cfg)

	// Get current policy
	result, err := svc.GetKeyPolicy(ctx, &kms.GetKeyPolicyInput{
		KeyId:      aws.String(keyID),
		PolicyName: aws.String("default"),
	})
	if err != nil {
		return fmt.Errorf("failed to get KMS policy: %w", err)
	}

	// For simplicity, just append the spoke account statement
	// In production, proper JSON manipulation would be used
	currentPolicy := aws.ToString(result.Policy)
	_ = currentPolicy

	// Build new policy with spoke account
	spokeAccountARN := fmt.Sprintf("arn:aws:iam::%s:root", spokeAccountID)
	newStatement := fmt.Sprintf(`{
		"Sid": "SpokeAccount%s",
		"Effect": "Allow",
		"Principal": {"AWS": "%s"},
		"Action": ["kms:Decrypt","kms:Encrypt","kms:GenerateDataKey","kms:DescribeKey"],
		"Resource": "*"
	}`, spokeAccountID, spokeAccountARN)

	// Simple insertion before last closing bracket
	insertPoint := len(currentPolicy) - 2
	for currentPolicy[insertPoint] == ']' || currentPolicy[insertPoint] == '}' || currentPolicy[insertPoint] == ' ' || currentPolicy[insertPoint] == '\n' {
		insertPoint--
	}

	newPolicy := currentPolicy[:insertPoint+1] + ",\n" + newStatement + "\n]}"

	if _, err := svc.PutKeyPolicy(ctx, &kms.PutKeyPolicyInput{
		KeyId:      aws.String(keyID),
		PolicyName: aws.String("default"),
		Policy:     aws.String(newPolicy),
	}); err != nil {
		return fmt.Errorf("failed to update KMS policy: %w", err)
	}
	return nil
}

// ScheduleKMSKeyDeletion schedules a KMS key for deletion.
func (c *Client) ScheduleKMSKeyDeletion(keyID string, days int) error {
	ctx := context.Background()
	svc := kms.NewFromConfig(c.cfg)
	_, err := svc.ScheduleKeyDeletion(ctx, &kms.ScheduleKeyDeletionInput{
		KeyId:               aws.String(keyID),
		PendingWindowInDays: aws.Int32(int32(days)),
	})
	return err
}
