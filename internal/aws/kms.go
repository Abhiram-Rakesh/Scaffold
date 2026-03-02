package aws

import (
	"context"
	"encoding/json"
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
		Description: aws.String("Terraform state encryption key"),
		KeyUsage:    kmstypes.KeyUsageTypeEncryptDecrypt,
		KeySpec:     kmstypes.KeySpecSymmetricDefault,
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

	// Enable key rotation
	if _, err := svc.EnableKeyRotation(ctx, &kms.EnableKeyRotationInput{
		KeyId: aws.String(keyID),
	}); err != nil {
		return keyID, fmt.Errorf("failed to enable key rotation: %w", err)
	}

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

	spokeAccountARN := fmt.Sprintf("arn:aws:iam::%s:root", spokeAccountID)
	sid := fmt.Sprintf("SpokeAccount%s", spokeAccountID)

	var policy map[string]interface{}
	if err := json.Unmarshal([]byte(aws.ToString(result.Policy)), &policy); err != nil {
		return fmt.Errorf("failed to parse KMS policy JSON: %w", err)
	}

	// KMS policy Statement may be an array or a single object.
	var statements []interface{}
	switch s := policy["Statement"].(type) {
	case []interface{}:
		statements = s
	case map[string]interface{}:
		statements = []interface{}{s}
	default:
		return fmt.Errorf("invalid KMS policy: Statement must be object or array")
	}

	// Idempotent: do not add duplicate statements.
	for _, stmt := range statements {
		statement, ok := stmt.(map[string]interface{})
		if !ok {
			continue
		}
		if statementSid, ok := statement["Sid"].(string); ok && statementSid == sid {
			return nil
		}
		if principalIncludesAWSArn(statement["Principal"], spokeAccountARN) {
			return nil
		}
	}

	statements = append(statements, map[string]interface{}{
		"Sid":    sid,
		"Effect": "Allow",
		"Principal": map[string]interface{}{
			"AWS": spokeAccountARN,
		},
		"Action": []string{
			"kms:Decrypt",
			"kms:Encrypt",
			"kms:GenerateDataKey",
			"kms:DescribeKey",
		},
		"Resource": "*",
	})
	policy["Statement"] = statements

	updatedPolicy, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("failed to serialize KMS policy JSON: %w", err)
	}

	if _, err := svc.PutKeyPolicy(ctx, &kms.PutKeyPolicyInput{
		KeyId:      aws.String(keyID),
		PolicyName: aws.String("default"),
		Policy:     aws.String(string(updatedPolicy)),
	}); err != nil {
		return fmt.Errorf("failed to update KMS policy: %w", err)
	}
	return nil
}

func principalIncludesAWSArn(principal interface{}, arn string) bool {
	principalMap, ok := principal.(map[string]interface{})
	if !ok {
		return false
	}

	awsPrincipal, ok := principalMap["AWS"]
	if !ok {
		return false
	}

	switch p := awsPrincipal.(type) {
	case string:
		return p == arn
	case []interface{}:
		for _, v := range p {
			if s, ok := v.(string); ok && s == arn {
				return true
			}
		}
	}
	return false
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
