# Troubleshooting

Common issues and solutions when using Scaffold.

---

## OIDC Authentication Issues

### "no identity-based policy allows the sts:AssumeRoleWithWebIdentity"

The OIDC sub claim doesn't match the IAM trust policy.

**Diagnosis:** Check the `Debug OIDC Token` step in your GitHub Actions workflow. It prints the `sub` claim your job is sending.

**Common sub claim formats:**
- Without environment: `repo:my-org/my-app:ref:refs/heads/main`
- With environment: `repo:my-org/my-app:environment:staging`

**Fix:** Scaffold's generated IAM role accepts both formats via `StringLike`:
```json
"token.actions.githubusercontent.com:sub": [
  "repo:<org>/<repo>:ref:refs/heads/*",
  "repo:<org>/<repo>:environment:*"
]
```

If you customized the trust policy, ensure both patterns are present.

---

## S3 State Issues

### "AccessDenied: Access Denied" when running Terraform

The GitHub Actions IAM role doesn't have cross-account S3 access.

**Check:**
1. S3 bucket policy includes the spoke account's IAM role ARN
2. KMS key policy allows the spoke account to use the key
3. The role's inline `terraform-backend-access` policy grants S3/DynamoDB/KMS permissions

**Re-run policy update:**
```bash
scaffold create staging  # Scaffold is idempotent — it will re-apply missing policies
```

### "BucketAlreadyOwnedByYou"

The S3 bucket name you chose already exists in your account. Scaffold generates a unique suffix to avoid this, but if you specified a custom name, try a different one.

---

## State Lock Issues

### "Error acquiring the state lock"

A previous Terraform run left a lock in DynamoDB.

**Option 1: Use Scaffold**
```bash
scaffold destroy staging  # Scaffold will offer to remove stale locks interactively
```

**Option 2: Manual removal**
```bash
aws dynamodb delete-item \
  --table-name tf-lock-myapp-a1b2c3d4 \
  --key '{"LockID": {"S": "tf-state-myapp-a1b2c3d4/staging/terraform.tfstate"}}' \
  --region us-east-1
```

**When is a lock safe to remove?**
- The GitHub Actions workflow run has completed (success or failure)
- No `terraform apply` is currently running locally
- The lock's timestamp is older than any active operation

---

## KMS Issues

### "InvalidKeyUsage: The request was rejected because the specified CMK..."

The KMS key isn't accessible from the spoke account.

**Fix:** Ensure the KMS key policy includes the spoke account:
```json
{
  "Sid": "SpokeAccount222222222222",
  "Effect": "Allow",
  "Principal": {"AWS": "arn:aws:iam::222222222222:root"},
  "Action": ["kms:Decrypt", "kms:Encrypt", "kms:GenerateDataKey", "kms:DescribeKey"],
  "Resource": "*"
}
```

Run `scaffold create staging` again to re-apply the policy update.

---

## IAM / SCP Issues

### "AccessDenied: User is not authorized to perform: iam:AttachRolePolicy"

Your AWS Organization has an SCP blocking managed policy attachments. This is why Scaffold defaults to **inline policies**.

If you're seeing this error, ensure you selected "Inline policies (SCP-compliant)" when running `scaffold create`, or re-create the environment.

---

## GitHub Actions Issues

### Workflow doesn't trigger on push

**Check:**
1. The `paths` filter in the workflow — it only triggers when `.tf` or `.tfvars` files change in the watch directory
2. The `branches` filter — it only triggers on the configured trigger branch
3. Ensure the workflow file was committed: `git add .github/workflows/`

### "Repository secret AWS_ROLE_ARN_STAGING not found"

The GitHub secret wasn't added. Follow the manual step shown by `scaffold create`:

1. Go to: **GitHub → Your Repo → Settings → Secrets and variables → Actions**
2. Click **New repository secret**
3. Name: `AWS_ROLE_ARN_STAGING`
4. Value: `arn:aws:iam::222222222222:role/github-actions-staging`

---

## Config File Issues

### ".scaffold/config.json not found"

You haven't initialized Scaffold yet. Run:
```bash
scaffold init
```

### "Environment 'staging' not found"

The environment wasn't created or config is out of sync. Check:
```bash
cat .scaffold/config.json | jq '.environments[].name'
```

---

## Getting More Help

- Open an issue: https://github.com/scaffold-tool/scaffold/issues
- Documentation: https://scaffold.sh/docs
