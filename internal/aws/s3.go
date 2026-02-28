package aws

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// StateInfo holds metadata about a Terraform state file.
type StateInfo struct {
	LastModified string
	Version      int
	Serial       int
	Size         int64
}

// CreateStateBucket creates the S3 state bucket with all required settings.
func (c *Client) CreateStateBucket(bucketName, region, accountID, kmsKeyID, githubOrg, githubRepo string) (string, error) {
	ctx := context.Background()
	svc := s3.NewFromConfig(c.cfg)

	// Generate unique suffix
	suffix := make([]byte, 4)
	rand.Read(suffix)
	finalName := fmt.Sprintf("%s-%s", bucketName, hex.EncodeToString(suffix))

	// Check if bucket already exists (idempotent)
	_, err := svc.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(finalName)})
	if err == nil {
		return finalName, nil // already exists
	}

	// Create bucket
	input := &s3.CreateBucketInput{
		Bucket: aws.String(finalName),
	}
	if region != "us-east-1" {
		input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(region),
		}
	}
	if _, err := svc.CreateBucket(ctx, input); err != nil {
		return "", fmt.Errorf("failed to create S3 bucket: %w", err)
	}

	// Enable versioning
	if _, err := svc.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(finalName),
		VersioningConfiguration: &s3types.VersioningConfiguration{
			Status: s3types.BucketVersioningStatusEnabled,
		},
	}); err != nil {
		return finalName, fmt.Errorf("failed to enable versioning: %w", err)
	}

	// Block public access
	if _, err := svc.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
		Bucket: aws.String(finalName),
		PublicAccessBlockConfiguration: &s3types.PublicAccessBlockConfiguration{
			BlockPublicAcls:       aws.Bool(true),
			BlockPublicPolicy:     aws.Bool(true),
			IgnorePublicAcls:      aws.Bool(true),
			RestrictPublicBuckets: aws.Bool(true),
		},
	}); err != nil {
		return finalName, fmt.Errorf("failed to block public access: %w", err)
	}

	// Enable KMS encryption
	if kmsKeyID != "" {
		if _, err := svc.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
			Bucket: aws.String(finalName),
			ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
				Rules: []s3types.ServerSideEncryptionRule{
					{
						ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
							SSEAlgorithm:   s3types.ServerSideEncryptionAwsKms,
							KMSMasterKeyID: aws.String(kmsKeyID),
						},
					},
				},
			},
		}); err != nil {
			return finalName, fmt.Errorf("failed to set encryption: %w", err)
		}
	}

	// Lifecycle policy
	if _, err := svc.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(finalName),
		LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{
			Rules: []s3types.LifecycleRule{
				{
					ID:     aws.String("expire-old-versions"),
					Status: s3types.ExpirationStatusEnabled,
					Filter: &s3types.LifecycleRuleFilterMemberPrefix{Value: ""},
					NoncurrentVersionExpiration: &s3types.NoncurrentVersionExpiration{
						NoncurrentDays: aws.Int32(90),
					},
				},
			},
		},
	}); err != nil {
		return finalName, fmt.Errorf("failed to set lifecycle policy: %w", err)
	}

	// Set bucket policy
	policy := buildInitialBucketPolicy(finalName, accountID)
	policyJSON, _ := json.Marshal(policy)
	if _, err := svc.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(finalName),
		Policy: aws.String(string(policyJSON)),
	}); err != nil {
		return finalName, fmt.Errorf("failed to set bucket policy: %w", err)
	}

	// Tag bucket
	if _, err := svc.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket: aws.String(finalName),
		Tagging: &s3types.Tagging{
			TagSet: []s3types.Tag{
				{Key: aws.String("Name"), Value: aws.String("Terraform State Bucket")},
				{Key: aws.String("ManagedBy"), Value: aws.String("Scaffold")},
				{Key: aws.String("Repository"), Value: aws.String(fmt.Sprintf("%s/%s", githubOrg, githubRepo))},
			},
		},
	}); err != nil {
		// Non-fatal
	}

	return finalName, nil
}

// AddSpokeAccountToS3Policy adds a spoke account to the S3 bucket policy.
func (c *Client) AddSpokeAccountToS3Policy(bucketName, spokeAccountID string) error {
	ctx := context.Background()
	svc := s3.NewFromConfig(c.cfg)

	// Get current policy
	result, err := svc.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return fmt.Errorf("failed to get bucket policy: %w", err)
	}

	var policy map[string]interface{}
	if err := json.Unmarshal([]byte(aws.ToString(result.Policy)), &policy); err != nil {
		return fmt.Errorf("failed to parse bucket policy: %w", err)
	}

	// Add spoke account statement
	statements, _ := policy["Statement"].([]interface{})
	spokeStatement := map[string]interface{}{
		"Sid":    fmt.Sprintf("SpokeAccount%s", spokeAccountID),
		"Effect": "Allow",
		"Principal": map[string]interface{}{
			"AWS": fmt.Sprintf("arn:aws:iam::%s:root", spokeAccountID),
		},
		"Action": []string{
			"s3:GetObject",
			"s3:PutObject",
			"s3:DeleteObject",
			"s3:ListBucket",
		},
		"Resource": []string{
			fmt.Sprintf("arn:aws:s3:::%s", bucketName),
			fmt.Sprintf("arn:aws:s3:::%s/*", bucketName),
		},
	}

	// Check if already added
	sid := fmt.Sprintf("SpokeAccount%s", spokeAccountID)
	for _, s := range statements {
		if stmt, ok := s.(map[string]interface{}); ok {
			if stmt["Sid"] == sid {
				return nil // already added
			}
		}
	}

	statements = append(statements, spokeStatement)
	policy["Statement"] = statements

	policyJSON, _ := json.Marshal(policy)
	_, err = svc.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucketName),
		Policy: aws.String(string(policyJSON)),
	})
	return err
}

// GetStateResources parses the Terraform state file and returns resource addresses.
func (c *Client) GetStateResources(bucketName, stateKey string) ([]string, error) {
	ctx := context.Background()
	svc := s3.NewFromConfig(c.cfg)

	result, err := svc.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(stateKey),
	})
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()

	var buf bytes.Buffer
	buf.ReadFrom(result.Body)

	var state struct {
		Resources []struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			Module    string `json:"module"`
			Instances []struct {
				Attributes map[string]interface{} `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}

	if err := json.Unmarshal(buf.Bytes(), &state); err != nil {
		return nil, err
	}

	var resources []string
	for _, r := range state.Resources {
		addr := fmt.Sprintf("%s.%s", r.Type, r.Name)
		if r.Module != "" {
			addr = fmt.Sprintf("%s.%s.%s", r.Module, r.Type, r.Name)
		}
		resources = append(resources, addr)
	}
	return resources, nil
}

// GetStateInfo returns metadata about a Terraform state file.
func (c *Client) GetStateInfo(bucketName, stateKey string) (*StateInfo, error) {
	ctx := context.Background()
	svc := s3.NewFromConfig(c.cfg)

	headResult, err := svc.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(stateKey),
	})
	if err != nil {
		return nil, err
	}

	info := &StateInfo{
		LastModified: headResult.LastModified.Format("2006-01-02 15:04:05 UTC"),
		Size:         aws.ToInt64(headResult.ContentLength),
	}

	result, err := svc.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(stateKey),
	})
	if err != nil {
		return info, nil
	}
	defer result.Body.Close()

	var buf bytes.Buffer
	buf.ReadFrom(result.Body)

	var state struct {
		Version int `json:"version"`
		Serial  int `json:"serial"`
	}
	if err := json.Unmarshal(buf.Bytes(), &state); err == nil {
		info.Version = state.Version
		info.Serial = state.Serial
	}

	return info, nil
}

// EmptyBucket deletes all objects in a bucket.
func (c *Client) EmptyBucket(bucketName string) error {
	ctx := context.Background()
	svc := s3.NewFromConfig(c.cfg)

	paginator := s3.NewListObjectsV2Paginator(svc, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, obj := range page.Contents {
			if _, err := svc.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucketName),
				Key:    obj.Key,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteBucket deletes an S3 bucket.
func (c *Client) DeleteBucket(bucketName string) error {
	ctx := context.Background()
	svc := s3.NewFromConfig(c.cfg)
	_, err := svc.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	return err
}

func buildInitialBucketPolicy(bucketName, accountID string) map[string]interface{} {
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
					"s3:ListBucket",
					"s3:GetObject",
					"s3:PutObject",
					"s3:DeleteObject",
				},
				"Resource": []string{
					fmt.Sprintf("arn:aws:s3:::%s", bucketName),
					fmt.Sprintf("arn:aws:s3:::%s/*", bucketName),
				},
			},
			map[string]interface{}{
				"Sid":    "DenyUnencryptedObjectUploads",
				"Effect": "Deny",
				"Principal": map[string]interface{}{
					"AWS": "*",
				},
				"Action": "s3:PutObject",
				"Resource": fmt.Sprintf("arn:aws:s3:::%s/*", bucketName),
				"Condition": map[string]interface{}{
					"StringNotEquals": map[string]interface{}{
						"s3:x-amz-server-side-encryption": "aws:kms",
					},
				},
			},
		},
	}
}

// Ensure strings package usage
var _ = strings.Contains
