# TaskFlow Desktop — CI/CD Setup Guide

## How It Works

```
Push tag v1.1.0
    ↓
GitHub Actions (3 parallel jobs):
  ├── windows-latest → builds .exe + NSIS installer (Windows 10/11)
  ├── ubuntu-22.04   → builds AppImage (runs on ALL Linux distros)
  └── macos-latest   → builds universal .dmg (Intel + Apple Silicon)
    ↓
Release job:
  ├── Creates GitHub Release with all 3 files attached
  └── Uploads to S3 + creates latest.json for website
```

## Step 1: Add GitHub Secrets

Go to: `github.com/Giridharan0624/taskflow-desktop/settings/secrets/actions`

Add these secrets:

| Secret Name | Value | Where to find |
|-------------|-------|---------------|
| `API_URL` | `https://3syc4x99a7.execute-api.ap-south-1.amazonaws.com/prod` | CDK output |
| `COGNITO_REGION` | `ap-south-1` | CDK output |
| `COGNITO_POOL_ID` | `ap-south-1_72qWKeSH5` | CDK output |
| `COGNITO_CLIENT_ID` | `pentcto4cmlfof93tsv738nct` | CDK output |
| `WEB_DASHBOARD_URL` | `https://taskflow-ns.vercel.app` | Your Vercel URL |
| `AWS_ACCESS_KEY_ID` | Your AWS access key | AWS IAM |
| `AWS_SECRET_ACCESS_KEY` | Your AWS secret key | AWS IAM |

## Step 2: Create AWS IAM User for CI/CD

Create a dedicated IAM user with **only S3 write access**:

```bash
aws iam create-user --user-name taskflow-ci-cd

aws iam put-user-policy --user-name taskflow-ci-cd \
  --policy-name S3UploadPolicy \
  --policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Action": ["s3:PutObject", "s3:PutObjectAcl"],
      "Resource": "arn:aws:s3:::taskflow-uploads-prod/releases/*"
    }]
  }'

aws iam create-access-key --user-name taskflow-ci-cd
```

Use the output access key/secret for the `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` secrets.

## Step 3: Trigger a Release

```bash
# Bump version in build scripts first
git add .
git commit -m "Release v1.1.0"
git tag v1.1.0
git push origin main
git push origin v1.1.0
```

GitHub Actions will automatically:
1. Build all 3 platforms
2. Create a GitHub Release with download links
3. Upload to S3 + create `latest.json`

## Step 4: Verify

After the workflow completes (~10-15 minutes):

**GitHub Release:**
`https://github.com/Giridharan0624/taskflow-desktop/releases/tag/v1.1.0`

**S3/CloudFront downloads:**
- Windows: `https://d32wbqjdb87hcf.cloudfront.net/releases/v1.1.0/TaskFlowDesktop-Setup-1.1.0.exe`
- Linux: `https://d32wbqjdb87hcf.cloudfront.net/releases/v1.1.0/TaskFlow-1.1.0-x86_64.AppImage`
- macOS: `https://d32wbqjdb87hcf.cloudfront.net/releases/v1.1.0/TaskFlowDesktop-1.1.0-universal.dmg`

**Latest version JSON (for website):**
`https://d32wbqjdb87hcf.cloudfront.net/releases/latest.json`

## Website Download Page

The website can fetch `latest.json` to show download buttons:

```json
{
  "version": "1.1.0",
  "released_at": "2026-04-05T10:00:00Z",
  "downloads": {
    "windows": "https://d32wbqjdb87hcf.cloudfront.net/releases/v1.1.0/TaskFlowDesktop-Setup-1.1.0.exe",
    "linux": "https://d32wbqjdb87hcf.cloudfront.net/releases/v1.1.0/TaskFlow-1.1.0-x86_64.AppImage",
    "macos": "https://d32wbqjdb87hcf.cloudfront.net/releases/v1.1.0/TaskFlowDesktop-1.1.0-universal.dmg"
  },
  "github_release": "https://github.com/Giridharan0624/taskflow-desktop/releases/tag/v1.1.0"
}
```

## Workflow File

Location: `.github/workflows/release.yml`

Triggered by: pushing a tag matching `v*` (e.g., `v1.0.0`, `v1.1.0`)
