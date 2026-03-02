package aws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// StateLock holds information about a Terraform state lock.
type StateLock struct {
	LockID    string
	Operation string
	Who       string
	Created   string
}

// CreateLockTable creates the DynamoDB lock table with all required settings.
func (c *Client) CreateLockTable(tableName, region, githubOrg, githubRepo string) (string, error) {
	ctx := context.Background()
	svc := dynamodb.NewFromConfig(c.cfg)

	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("failed to generate table suffix: %w", err)
	}
	finalName := fmt.Sprintf("%s-%s", tableName, hex.EncodeToString(suffix))

	// Check if already exists
	_, err := svc.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(finalName),
	})
	if err == nil {
		return finalName, nil
	}

	// Create table
	if _, err := svc.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   aws.String(finalName),
		BillingMode: dynamodbtypes.BillingModePayPerRequest,
		AttributeDefinitions: []dynamodbtypes.AttributeDefinition{
			{
				AttributeName: aws.String("LockID"),
				AttributeType: dynamodbtypes.ScalarAttributeTypeS,
			},
		},
		KeySchema: []dynamodbtypes.KeySchemaElement{
			{
				AttributeName: aws.String("LockID"),
				KeyType:       dynamodbtypes.KeyTypeHash,
			},
		},
		Tags: []dynamodbtypes.Tag{
			{Key: aws.String("Name"), Value: aws.String("Terraform Lock Table")},
			{Key: aws.String("ManagedBy"), Value: aws.String("Scaffold")},
			{Key: aws.String("Repository"), Value: aws.String(fmt.Sprintf("%s/%s", githubOrg, githubRepo))},
		},
	}); err != nil {
		return "", fmt.Errorf("failed to create DynamoDB table: %w", err)
	}

	// Wait for table to be active
	waiter := dynamodb.NewTableExistsWaiter(svc)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(finalName),
	}, 2*time.Minute); err != nil {
		return finalName, fmt.Errorf("timeout waiting for table: %w", err)
	}

	// Enable PITR
	if _, err := svc.UpdateContinuousBackups(ctx, &dynamodb.UpdateContinuousBackupsInput{
		TableName: aws.String(finalName),
		PointInTimeRecoverySpecification: &dynamodbtypes.PointInTimeRecoverySpecification{
			PointInTimeRecoveryEnabled: aws.Bool(true),
		},
	}); err != nil {
		// Non-fatal: table creation succeeded even if PITR toggle is denied.
		_ = err
	}

	return finalName, nil
}

// AddSpokeAccountToDynamoPolicy adds a spoke account to the DynamoDB resource policy.
func (c *Client) AddSpokeAccountToDynamoPolicy(tableName, region, backendAccountID, spokeAccountID string) error {
	ctx := context.Background()
	svc := dynamodb.NewFromConfig(c.cfg)

	tableARN := fmt.Sprintf("arn:aws:dynamodb:%s:%s:table/%s", region, backendAccountID, tableName)

	// Get current policy
	result, err := svc.GetResourcePolicy(ctx, &dynamodb.GetResourcePolicyInput{
		ResourceArn: aws.String(tableARN),
	})

	var policy map[string]interface{}
	if err != nil {
		policy = buildInitialDynamoPolicy(tableARN, backendAccountID)
	} else {
		if err := json.Unmarshal([]byte(aws.ToString(result.Policy)), &policy); err != nil {
			return err
		}
	}

	// Add spoke account statement
	statements, _ := policy["Statement"].([]interface{})
	sid := fmt.Sprintf("SpokeAccount%s", spokeAccountID)

	for _, s := range statements {
		if stmt, ok := s.(map[string]interface{}); ok {
			if stmt["Sid"] == sid {
				return nil
			}
		}
	}

	statements = append(statements, map[string]interface{}{
		"Sid":    sid,
		"Effect": "Allow",
		"Principal": map[string]interface{}{
			"AWS": fmt.Sprintf("arn:aws:iam::%s:root", spokeAccountID),
		},
		"Action": []string{
			"dynamodb:PutItem",
			"dynamodb:GetItem",
			"dynamodb:DeleteItem",
			"dynamodb:DescribeTable",
		},
		"Resource": tableARN,
	})
	policy["Statement"] = statements

	policyJSON, _ := json.Marshal(policy)
	_, err = svc.PutResourcePolicy(ctx, &dynamodb.PutResourcePolicyInput{
		ResourceArn: aws.String(tableARN),
		Policy:      aws.String(string(policyJSON)),
	})
	return err
}

// GetStateLock checks for an active Terraform state lock.
func (c *Client) GetStateLock(tableName, bucketName, stateKey string) (*StateLock, error) {
	ctx := context.Background()
	svc := dynamodb.NewFromConfig(c.cfg)

	lockID := fmt.Sprintf("%s/%s", bucketName, stateKey)
	result, err := svc.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key: map[string]dynamodbtypes.AttributeValue{
			"LockID": &dynamodbtypes.AttributeValueMemberS{Value: lockID},
		},
	})
	if err != nil {
		return nil, err
	}
	if result.Item == nil {
		return nil, nil
	}

	lock := &StateLock{LockID: lockID}
	if info, ok := result.Item["Info"]; ok {
		if infoStr, ok := info.(*dynamodbtypes.AttributeValueMemberS); ok {
			var lockInfo map[string]interface{}
			if err := json.Unmarshal([]byte(infoStr.Value), &lockInfo); err == nil {
				if op, ok := lockInfo["Operation"].(string); ok {
					lock.Operation = op
				}
				if who, ok := lockInfo["Who"].(string); ok {
					lock.Who = who
				}
				if created, ok := lockInfo["Created"].(string); ok {
					lock.Created = created
				}
			}
		}
	}
	return lock, nil
}

// RemoveStateLock removes a Terraform state lock from DynamoDB.
func (c *Client) RemoveStateLock(tableName, lockID string) error {
	ctx := context.Background()
	svc := dynamodb.NewFromConfig(c.cfg)
	_, err := svc.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(tableName),
		Key: map[string]dynamodbtypes.AttributeValue{
			"LockID": &dynamodbtypes.AttributeValueMemberS{Value: lockID},
		},
	})
	return err
}

// DeleteLockTable deletes the DynamoDB lock table.
func (c *Client) DeleteLockTable(tableName string) error {
	ctx := context.Background()
	svc := dynamodb.NewFromConfig(c.cfg)
	_, err := svc.DeleteTable(ctx, &dynamodb.DeleteTableInput{
		TableName: aws.String(tableName),
	})
	return err
}

func buildInitialDynamoPolicy(tableARN, accountID string) map[string]interface{} {
	return map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []interface{}{
			map[string]interface{}{
				"Sid":    "BackendAccountAccess",
				"Effect": "Allow",
				"Principal": map[string]interface{}{
					"AWS": fmt.Sprintf("arn:aws:iam::%s:root", accountID),
				},
				"Action": []string{
					"dynamodb:PutItem",
					"dynamodb:GetItem",
					"dynamodb:DeleteItem",
					"dynamodb:DescribeTable",
				},
				"Resource": tableARN,
			},
		},
	}
}
