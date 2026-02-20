# Scaffold

**Production-grade Terraform CI/CD in 3 commands.**

Scaffold bootstraps GitHub Actions pipelines for Terraform — OIDC auth, S3 remote state, DynamoDB locking — without touching stored AWS credentials.

```bash
git clone https://github.com/your-org/scaffold ~/.scaffold-cli
export PATH="$HOME/.scaffold-cli/bin:$PATH"
scaffold init
```

---

## Table of Contents

1. [Quick Start](#quick-start)
2. [Installation](#installation)
3. [Commands Reference](#commands-reference)
4. [Multi-Environment Setup](#multi-environment-setup)
5. [SCP Compliance Mode](#scp-compliance-mode)
6. [Architecture](#architecture)
7. [Troubleshooting](#troubleshooting)
8. [Contributing](#contributing)

---

## Quick Start

```bash
# 1. Install
git clone https://github.com/your-org/scaffold ~/.scaffold-cli
export PATH="$HOME/.scaffold-cli/bin:$PATH"

# 2. Bootstrap (run from inside your application repo)
cd my-app
scaffold init

# 3. Ship it
git add . && git commit -m "feat: add Scaffold CI/CD" && git push
```

Scaffold creates everything — S3 bucket, DynamoDB table, IAM OIDC role, and GitHub Actions workflow — then generates a `providers.tf` so Terraform knows to use S3 backend from day one.

---

## Installation

### Requirements

- Bash 4+
- AWS CLI v2
- Terraform 1.7+
- `git` with a GitHub remote named `origin`
- `python3` (stdlib only)

### Install

```bash
git clone https://github.com/your-org/scaffold ~/.scaffold-cli
echo 'export PATH="$HOME/.scaffold-cli/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

Verify:

```bash
scaffold version
```

### Upgrade

```bash
cd ~/.scaffold-cli && git pull
```

---

## Commands Reference

### `scaffold init`

Interactively provisions AWS resources and generates GitHub Actions workflows for each environment.

**What it creates:**

| Resource | Example Name |
|---|---|
| S3 bucket | `tf-state-my-app-a1b2c3d4` |
| DynamoDB table | `tf-lock-my-app-a1b2c3d4` |
| IAM OIDC role | `github-actions-my-app` |
| GitHub Actions workflow | `.github/workflows/terraform-production.yaml` |
| Terraform backend config | `infra/production/providers.tf` |
| Scaffold config | `.scaffold/config.json` |

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--use-inline-policies=false` | `true` | Disable inline IAM policies (uses managed policy attachments instead) |
| `--shared-role` | `false` | Create one IAM role shared across all environments |

**Example session:**

```
$ scaffold init

╭─────────────────────────────────────╮
│   Scaffold - Infrastructure CI/CD  │
╰─────────────────────────────────────╯

Auto-detected repository: my-org/my-app

→ AWS Configuration
  AWS Credentials:
  [1] Use existing AWS CLI profile
  [2] Enter access key/secret (stored in memory only)
  Choice [1]: 1
  Profile [default]: production
  ✓  Authenticated as: arn:aws:iam::123456789:user/alice

→ Terraform Configuration
  How many environments? [1]: 1
  Environment 1:
    Name: production
    Watch directory: infra/production
    Trigger branch [main]: main

→ IAM Policy Mode
  Use inline policies (SCP-compliant)? [Y/n]: y

→ Provisioning
  ✓  S3 bucket: tf-state-my-app-a1b2c3d4
  ✓  DynamoDB table: tf-lock-my-app-a1b2c3d4
  ✓  IAM role: github-actions-my-app
  ✓  Workflow: .github/workflows/terraform-production.yaml
  ✓  providers.tf: infra/production/providers.tf
  ✓  Config: .scaffold/config.json

→ Next Steps
  1. Review:  git status
  2. Commit:  git add . && git commit -m "feat: add Scaffold"
  3. Push:    git push origin main
```

---

### `scaffold destroy`

Destroys Terraform-managed **application** infrastructure for a chosen environment. Platform resources (S3 state bucket, DynamoDB, IAM role) are preserved.

```
$ scaffold destroy

→ Select Environment
  [1] staging (infra/staging)
  [2] production (infra/production)
  [3] All environments
  Choice: 1

→ Checking for state locks...
  [WARN] Found 1 active state lock(s)
  Lock ID: tf-state-my-app-a1b2c3d4/staging/terraform.tfstate-md5

  This lock may be stale if:
    - GitHub Actions workflow completed
    - Pipeline crashed mid-apply
    - No terraform operations running

  Remove this lock? [y/N]: y
  [INFO] Removing stale lock...
  ✓  Lock removed. Continuing with destroy...

→ Type DESTROY to confirm: DESTROY
→ Destroying...
  ✓  Complete (53s)

  Note: Platform resources remain intact.
  Run `scaffold uninstall` to remove everything.
```

---

### `scaffold uninstall`

Destroys **everything**: application infrastructure, S3 bucket (all versions), DynamoDB table, IAM role(s), workflows, and config.

```
$ scaffold uninstall

⚠️  WARNING: This will destroy ALL Scaffold resources:
  - S3 state bucket (including all state history)
  - DynamoDB lock table
  - IAM OIDC role
  - All workflows
  - Configuration files

Type DESTROY EVERYTHING to confirm: DESTROY EVERYTHING
```

---

## Multi-Environment Setup

Scaffold supports any number of environments. Each environment gets:

- Its own GitHub Actions workflow with a scoped trigger (branch + path filter)
- Its own Terraform state key in the shared S3 bucket
- Its own IAM role (default) or a shared role (`--shared-role`)

```
$ scaffold init

  How many environments? [1]: 2

  Environment 1:
    Name: staging
    Watch directory: infra/staging
    Trigger branch [main]: develop

  Environment 2:
    Name: production
    Watch directory: infra/production
    Trigger branch [main]: main
```

This creates:

```
.github/workflows/
  terraform-staging.yaml      # triggers on push to develop, paths: infra/staging/**
  terraform-production.yaml   # triggers on push to main,    paths: infra/production/**

infra/
  staging/providers.tf
  production/providers.tf

.scaffold/config.json
```

State keys are isolated per environment:

```
s3://tf-state-my-app-a1b2c3d4/
  staging/terraform.tfstate
  production/terraform.tfstate
```

---

## SCP Compliance Mode

Many enterprise AWS accounts use Service Control Policies (SCPs) that deny `iam:AttachRolePolicy`. Scaffold's default `--use-inline-policies` mode works around this by using `aws_iam_role_policy` (inline policies) instead of `aws_iam_managed_policy` attachments.

The inline policy grants PowerUser-equivalent access with a deny for IAM write actions:

```hcl
# Allow everything...
statement {
  actions   = ["*"]
  resources = ["*"]
}

# ...except IAM/org mutations
statement {
  effect    = "Deny"
  actions   = ["iam:*", "organizations:*", "account:*"]
  resources = ["*"]
}

# ...but allow IAM reads so Terraform can introspect its own role
statement {
  actions   = ["iam:Get*", "iam:List*", "iam:Describe*"]
  resources = ["*"]
}
```

To disable (use managed policy attachments instead):

```bash
scaffold init --use-inline-policies=false
```

---

## Architecture

```
GitHub Push
    │
    ▼
GitHub Actions Runner (ephemeral)
    │
    ├── OIDC Token ──────────────────────────► AWS STS
    │                                              │
    │                                         AssumeRoleWithWebIdentity
    │                                              │
    │    ◄────────────── Temp Credentials ─────────┘
    │
    ├── terraform init ──► S3 (remote state, DynamoDB lock)
    │
    ├── terraform plan
    │
    └── terraform apply ──► Your AWS Resources
```

**No stored credentials.** GitHub uses OIDC to exchange a short-lived JWT for temporary AWS credentials. The trust policy accepts both branch refs (`ref:refs/heads/*`) and environment-scoped claims (`environment:*`).

**File layout after `scaffold init`:**

```
your-repo/
├── .scaffold/
│   └── config.json          # resource inventory
├── .github/workflows/
│   ├── terraform-staging.yaml
│   └── terraform-production.yaml
└── infra/
    ├── staging/
    │   ├── providers.tf     # auto-generated — enables S3 backend
    │   └── *.tf             # your Terraform code here
    └── production/
        ├── providers.tf
        └── *.tf
```

---

## Troubleshooting

### "Missing backend configuration" / local state used

**Symptom:** State file is 181 bytes, `terraform.tfstate` appears locally.

**Cause:** `providers.tf` is missing from the watch directory so Terraform ignores `-backend-config` flags.

**Fix:** Re-run `scaffold init` — it auto-creates `providers.tf` with an empty `backend "s3" {}` block.

---

### OIDC authentication failure

**Symptom:** `Error: No OpenIDConnect provider found in your account for...`

**Cause:** GitHub's `sub` claim format doesn't match the IAM trust policy.

**Fix:** Scaffold's trust policy accepts both formats. Check the "Debug OIDC sub claim" workflow step for the actual value:

```
Expected sub: repo:my-org/my-app:ref:refs/heads/main
```

Compare with what your IAM trust policy allows. You may need to re-run `scaffold init` to regenerate the role with the updated trust policy.

---

### `AlreadyExists` errors on re-run

**Symptom:** `EntityAlreadyExists: Role with name github-actions-my-app already exists`

**Cause:** `scaffold init` was interrupted mid-way.

**Fix:** Scaffold has import guards — resources are imported into Terraform state before applying. Simply re-run `scaffold init`.

---

### Orphaned state lock

**Symptom:** `Error: Error acquiring the state lock`

**Cause:** A GitHub Actions workflow crashed mid-apply, leaving a DynamoDB lock record.

**Fix:** Run `scaffold destroy` — it interactively detects and removes stale locks before proceeding.

Alternatively, remove manually:

```bash
aws dynamodb delete-item \
  --table-name tf-lock-my-app-a1b2c3d4 \
  --key '{"LockID":{"S":"tf-state-my-app-a1b2c3d4/production/terraform.tfstate-md5"}}'
```

---

### SCP blocking `iam:AttachRolePolicy`

**Symptom:** `AccessDenied: iam:AttachRolePolicy is not permitted`

**Cause:** Your organization's SCP denies managed policy attachments.

**Fix:** Scaffold defaults to inline policies which bypass this restriction. Confirm you ran `scaffold init` without `--use-inline-policies=false`.

---

## Contributing

```bash
git clone https://github.com/your-org/scaffold
cd scaffold
```

### Testing checklist

- [ ] Fresh repo, single environment
- [ ] Fresh repo, multiple environments
- [ ] Re-run `scaffold init` after partial failure (import guards)
- [ ] Destroy with active lock (interactive removal)
- [ ] Destroy with no resources (graceful empty state)
- [ ] Uninstall with resources running (forces destroy first)
- [ ] SCP-restricted account (inline policies)
- [ ] Non-default AWS region
- [ ] Non-default branch trigger
- [ ] Watch directory with subdirectories

### Structure

```
scaffold/
├── bin/scaffold              # Dispatcher
├── lib/
│   ├── init.sh
│   ├── destroy.sh
│   ├── uninstall.sh
│   └── common.sh
├── templates/
│   ├── workflow.yaml         # GitHub Actions template
│   ├── providers.tf          # Terraform backend template
│   └── terraform/
│       ├── backend/          # S3 + DynamoDB
│       └── iam/              # OIDC role + policies
└── README.md
```

---

## License

MIT
