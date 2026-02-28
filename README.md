# 🏗️ Scaffold

**Multi-Account Terraform CI/CD Platform**

[![CI](https://github.com/scaffold-tool/scaffold/actions/workflows/ci.yml/badge.svg)](https://github.com/scaffold-tool/scaffold/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/scaffold-tool/scaffold)](https://goreportcard.com/report/github.com/scaffold-tool/scaffold)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Scaffold is an open-source CLI tool written in Go that bootstraps production-grade, multi-account Terraform CI/CD pipelines using **GitHub Actions**, **centralized S3 remote state**, and **IAM OIDC authentication** — in minutes, not hours.

---

## ✨ Features

- 🏦 **Centralized State Backend** — One S3 + DynamoDB + KMS backend, many spoke accounts
- 🔐 **IAM OIDC Authentication** — No long-lived credentials. GitHub Actions assumes roles via federated identity
- 🔒 **SCP-Compliant** — Uses inline IAM policies to work in restricted AWS Organizations environments
- 🌐 **Multi-Account** — Dynamically grants cross-account access when you add environments
- 👁️ **Operational Visibility** — `scaffold status` shows resource inventory, workflow history, and lock status
- 🔄 **Idempotent** — Safe to re-run. Imports existing resources instead of failing

---

## ⚡ Quick Start

```bash
# 1. Install
curl -sSL https://scaffold.sh/install.sh | bash

# 2. Navigate to your infrastructure repo
cd your-infra-repo

# 3. Bootstrap the state backend (runs in your platform account)
scaffold init

# 4. Create your first environment (runs in a spoke account)
scaffold create staging

# 5. Add the GitHub secret shown by scaffold create, then push:
git add . && git commit -m "feat: add staging environment"
git push origin develop
```

Terraform runs automatically via GitHub Actions on every push. ✅

---

## 📦 Installation

### Quick Install (recommended)
```bash
curl -sSL https://scaffold.sh/install.sh | bash
```

### Manual Download
Download the latest binary for your platform from [GitHub Releases](https://github.com/scaffold-tool/scaffold/releases).

```bash
# Linux amd64
curl -sSfL https://github.com/scaffold-tool/scaffold/releases/latest/download/scaffold_linux_amd64.tar.gz | tar -xz
sudo mv scaffold /usr/local/bin/
```

### Build from Source
```bash
git clone https://github.com/scaffold-tool/scaffold
cd scaffold
make install
```

**Requirements:** Go 1.21+, AWS CLI, Terraform 1.7+

---

## 🏛️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     GitHub Repository                        │
│                                                             │
│  .github/workflows/                                         │
│  ├── terraform-staging.yaml    (auto-generated)             │
│  ├── terraform-production.yaml (auto-generated)             │
│  └── terraform-dev.yaml        (auto-generated)             │
│                                                             │
│  .scaffold/config.json         (tracks configuration)       │
└────────────────────────┬────────────────────────────────────┘
                         │ GitHub Actions (OIDC)
          ┌──────────────┼──────────────┐
          │              │              │
          ▼              ▼              ▼
┌─────────────┐  ┌─────────────┐  ┌─────────────┐
│   Staging   │  │ Production  │  │     Dev     │
│  Account    │  │   Account   │  │   Account   │
│ 222222222   │  │ 333333333   │  │ 444444444   │
│             │  │             │  │             │
│ IAM Role:   │  │ IAM Role:   │  │ IAM Role:   │
│ gh-actions- │  │ gh-actions- │  │ gh-actions- │
│   staging   │  │  production │  │    dev      │
└──────┬──────┘  └──────┬──────┘  └──────┬──────┘
       │                │                │
       └────────────────┼────────────────┘
                        │ Cross-account S3/DynamoDB/KMS access
                        ▼
             ┌──────────────────┐
             │  Platform Account │
             │   111111111111   │
             │                  │
             │  S3:  tf-state-* │  ← Centralized state
             │  DDB: tf-lock-*  │  ← State locking
             │  KMS: Key        │  ← Encryption
             └──────────────────┘
```

---

## 📖 Commands

### `scaffold init`
Bootstrap the centralized Terraform state backend in your platform account.

```bash
scaffold init
```

Creates:
- S3 bucket with versioning, KMS encryption, public access block, lifecycle policies
- DynamoDB table with on-demand billing and PITR
- KMS key with rotation enabled
- `.scaffold/config.json`

### `scaffold create <environment>`
Create a new environment with a dedicated GitHub Actions workflow and cross-account IAM access.

```bash
scaffold create staging
scaffold create production
scaffold create dev
```

Creates:
- `.github/workflows/terraform-<env>.yaml`
- `<watch-dir>/providers.tf`
- IAM OIDC provider in target account (idempotent)
- IAM role with inline policies in target account
- Cross-account S3/KMS/DynamoDB policy updates

### `scaffold status [environment]`
Display environment status, resource inventory, and recent workflow runs.

```bash
scaffold status staging
scaffold status --all
scaffold status staging --json
scaffold status staging --watch   # Live refresh every 5s
```

### `scaffold destroy <environment>`
Destroy all Terraform-managed infrastructure in an environment.

```bash
scaffold destroy staging
scaffold destroy staging --auto-approve   # Skip confirmation (CI use)
```

### `scaffold remove <environment>`
Remove environment workflow and config (does NOT destroy infrastructure).

```bash
scaffold remove staging              # Fails if active resources exist
scaffold remove staging --force      # Remove even with active resources
```

### `scaffold uninstall`
Remove all Scaffold resources including the state backend.

```bash
scaffold uninstall           # Requires all environments to be empty
scaffold uninstall --force   # Extremely dangerous — orphans resources
```

### Global Flags
```
--verbose, -v    Enable verbose logging
--quiet, -q      Suppress non-error output
--help, -h       Show command help
```

---

## 🔐 AWS Authentication

Scaffold supports three credential methods:

| Method | Description |
|--------|-------------|
| AWS CLI Profile | `~/.aws/credentials` named profile |
| Environment Variables | `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` |
| AWS SSO | Named SSO session from `~/.aws/config` |

Credentials are **never stored** in `.scaffold/config.json`. They're used only for the duration of each command.

---

## 🏦 Multi-Account Setup

### Account Model
- **Platform account** — Hosts S3 + DynamoDB + KMS backend. You need admin access here once for `scaffold init`.
- **Spoke accounts** — Each has one IAM role per environment. `scaffold create` handles policy setup automatically.

### Cross-Account Access
When you run `scaffold create staging`:
1. Platform account credentials → update S3 bucket policy to allow spoke account
2. Platform account credentials → update KMS key policy
3. Platform account credentials → update DynamoDB resource policy
4. Spoke account credentials → create OIDC provider (once per account)
5. Spoke account credentials → create IAM role with inline policies

See [docs/multi-account.md](docs/multi-account.md) for detailed guidance.

---

## 🗂️ Configuration File

`.scaffold/config.json` is committed to your repository and tracks all Scaffold state.

```json
{
  "version": "1.0",
  "backend": {
    "account_id": "111111111111",
    "region": "us-east-1",
    "s3_bucket": "tf-state-myapp-a1b2c3d4",
    "dynamodb_table": "tf-lock-myapp-a1b2c3d4",
    "kms_key_id": "arn:aws:kms:us-east-1:111111111111:key/..."
  },
  "repository": {
    "org": "my-org",
    "name": "my-app",
    "default_branch": "main"
  },
  "environments": [
    {
      "name": "staging",
      "account_id": "222222222222",
      "region": "us-east-1",
      "watch_directory": "infra/staging",
      "trigger_branch": "develop",
      "iam_role_arn": "arn:aws:iam::222222222222:role/github-actions-staging",
      "state_key": "staging/terraform.tfstate",
      "workflow_file": ".github/workflows/terraform-staging.yaml",
      "policy_mode": "inline"
    }
  ]
}
```

---

## 🐛 Troubleshooting

### "Error: no identity-based policy allows the sts:AssumeRoleWithWebIdentity action"
The OIDC trust policy doesn't match the GitHub sub claim. Check the Debug OIDC Token step in your workflow — the `sub` claim must match one of:
- `repo:<org>/<repo>:ref:refs/heads/*`
- `repo:<org>/<repo>:environment:*`

### "Error: S3 bucket policy update failed"
Ensure your backend account credentials have `s3:GetBucketPolicy` and `s3:PutBucketPolicy` permissions.

### "Error: state is locked"
Use `scaffold destroy` which offers interactive lock removal, or:
```bash
aws dynamodb delete-item \
  --table-name <table> \
  --key '{"LockID": {"S": "<bucket>/<state-key>"}}'
```

See [docs/troubleshooting.md](docs/troubleshooting.md) for more.

---

## 🛠️ Development

```bash
# Clone
git clone https://github.com/scaffold-tool/scaffold
cd scaffold

# Install dependencies
go mod download

# Build
make build

# Test
make test

# Lint
make lint
```

### Project Structure
```
scaffold/
├── cmd/scaffold/          # CLI entry point
├── internal/
│   ├── aws/               # AWS SDK wrappers (S3, DynamoDB, KMS, IAM)
│   ├── commands/          # Cobra command implementations
│   ├── config/            # Config file management
│   ├── github/            # GitHub API client
│   ├── terraform/         # Terraform runner + template generation
│   └── ui/                # Interactive prompts + terminal UI
├── pkg/version/           # Version info (set at build time)
├── templates/terraform/   # Terraform module templates
│   ├── backend/           # S3 + DynamoDB + KMS module
│   └── iam/               # GitHub Actions OIDC role module
├── scripts/install.sh     # Installer script
└── docs/                  # Additional documentation
```

---

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feat/my-feature`
3. Make your changes with tests
4. Run `make test lint`
5. Submit a pull request

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for code style and PR guidelines.

---

## 📄 License

MIT License — see [LICENSE](LICENSE) for details.

---

## 📚 Documentation

- [Getting Started](docs/getting-started.md)
- [Multi-Account Setup](docs/multi-account.md)
- [SCP Compliance](docs/scp-compliance.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Architecture](docs/architecture.md)
