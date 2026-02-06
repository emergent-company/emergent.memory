# Emergent CLI - GitHub Release Process

This document describes how to create and publish releases for the Emergent CLI tool.

## Release Workflow

### 1. Version Tagging

Create and push a version tag with the `cli-v` prefix:

```bash
git tag cli-v0.1.0
git push origin cli-v0.1.0
```

The tag format is: `cli-v{MAJOR}.{MINOR}.{PATCH}`

### 2. Automated Build Process

Once the tag is pushed, GitHub Actions automatically:

1. **Runs Tests**: Unit tests with race detection
2. **Lints Code**: Using golangci-lint
3. **Cross-Compiles Binaries** for:
   - Linux: amd64, arm64, arm
   - macOS: amd64 (Intel), arm64 (Apple Silicon)
   - Windows: amd64, arm64
   - FreeBSD: amd64
4. **Creates Archives**: `.tar.gz` for Unix, `.zip` for Windows
5. **Generates Checksums**: SHA256 for all artifacts
6. **Publishes Docker Image**: Multi-arch image to GHCR
7. **Creates GitHub Release**: With auto-generated release notes

### 3. Release Artifacts

Each release includes:

- **Binary Archives**: One per platform/architecture
- **Checksums File**: `checksums.txt` with SHA256 hashes
- **Docker Image**: `ghcr.io/eyedea-io/emergent-cli:{version}`
- **Release Notes**: Installation instructions and changelog

### 4. Installation Methods

#### Direct Binary Download

Users can download pre-built binaries from the GitHub Releases page:

```bash
# Linux amd64
curl -L -o emergent-cli.tar.gz \
  https://github.com/eyedea-io/emergent/releases/download/cli-v0.1.0/emergent-cli-linux-amd64.tar.gz
tar xzf emergent-cli.tar.gz
sudo mv emergent-cli-linux-amd64 /usr/local/bin/emergent-cli
```

#### Docker Container

```bash
docker pull ghcr.io/eyedea-io/emergent-cli:latest

docker run --rm -e EMERGENT_SERVER_URL=http://host.docker.internal:9090 \
  -e EMERGENT_API_KEY=your-key \
  ghcr.io/eyedea-io/emergent-cli:latest projects list
```

#### Homebrew (macOS/Linux) - Future

Once we create a Homebrew tap:

```bash
brew tap eyedea-io/emergent
brew install emergent-cli
```

## Creating a Homebrew Tap (Manual Step)

After the first stable release (v1.0.0), create a Homebrew formula:

### 1. Create Tap Repository

```bash
gh repo create eyedea-io/homebrew-emergent --public
cd homebrew-emergent
```

### 2. Create Formula

File: `Formula/emergent-cli.rb`

```ruby
class EmergentCli < Formula
  desc "CLI tool for Emergent Knowledge Base platform"
  homepage "https://github.com/eyedea-io/emergent"
  version "1.0.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/eyedea-io/emergent/releases/download/cli-v1.0.0/emergent-cli-darwin-arm64.tar.gz"
      sha256 "ACTUAL_SHA256_HERE"
    else
      url "https://github.com/eyedea-io/emergent/releases/download/cli-v1.0.0/emergent-cli-darwin-amd64.tar.gz"
      sha256 "ACTUAL_SHA256_HERE"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/eyedea-io/emergent/releases/download/cli-v1.0.0/emergent-cli-linux-arm64.tar.gz"
      sha256 "ACTUAL_SHA256_HERE"
    else
      url "https://github.com/eyedea-io/emergent/releases/download/cli-v1.0.0/emergent-cli-linux-amd64.tar.gz"
      sha256 "ACTUAL_SHA256_HERE"
    end
  end

  def install
    bin.install "emergent-cli-#{OS.kernel_name.downcase}-#{Hardware::CPU.arch}" => "emergent-cli"
  end

  test do
    system "#{bin}/emergent-cli", "version"
  end
end
```

### 3. Update Formula for New Releases

Use `brew bump-formula-pr` after each release:

```bash
brew bump-formula-pr \
  --url=https://github.com/eyedea-io/emergent/releases/download/cli-v1.1.0/emergent-cli-darwin-arm64.tar.gz \
  eyedea-io/emergent/emergent-cli
```

## Versioning Strategy

Following Semantic Versioning (SemVer):

- **MAJOR**: Breaking API changes (e.g., removing commands, changing flags)
- **MINOR**: New features, backward compatible (e.g., new commands)
- **PATCH**: Bug fixes, backward compatible

Examples:

- `cli-v1.0.0`: First stable release
- `cli-v1.1.0`: Added `documents` command
- `cli-v1.1.1`: Fixed OAuth token refresh bug
- `cli-v2.0.0`: Changed config file format (breaking)

## Pre-Release Tags

For testing before stable release:

```bash
git tag cli-v0.1.0-beta.1
git push origin cli-v0.1.0-beta.1
```

The workflow automatically marks tags with `-alpha`, `-beta`, `-rc` as pre-releases.

## Manual Release Creation (Emergency)

If automated workflow fails:

```bash
cd tools/emergent-cli

# Build for target platform
GOOS=linux GOARCH=amd64 go build -o emergent-cli-linux-amd64 ./cmd

# Create archive
tar czf emergent-cli-linux-amd64.tar.gz emergent-cli-linux-amd64

# Generate checksum
sha256sum emergent-cli-linux-amd64.tar.gz > checksums.txt

# Upload to GitHub Releases manually
gh release create cli-v0.1.0 \
  emergent-cli-linux-amd64.tar.gz \
  checksums.txt \
  --title "Emergent CLI v0.1.0" \
  --notes "Manual release due to workflow failure"
```

## Testing Releases

Before pushing tags to production:

### 1. Test Local Build

```bash
cd tools/emergent-cli
go build -o emergent-cli ./cmd
./emergent-cli version
./emergent-cli projects list  # Test with real server
```

### 2. Test Cross-Compilation

```bash
GOOS=linux GOARCH=amd64 go build -o emergent-cli-linux ./cmd
GOOS=darwin GOARCH=arm64 go build -o emergent-cli-macos ./cmd
GOOS=windows GOARCH=amd64 go build -o emergent-cli.exe ./cmd
```

### 3. Test Docker Build

```bash
cd tools/emergent-cli
docker build -t emergent-cli:test .
docker run --rm emergent-cli:test version
```

## Rollback Procedure

If a release has critical bugs:

### 1. Mark Release as Draft

```bash
gh release edit cli-v0.2.0 --draft
```

### 2. Fix the Issue

```bash
git checkout -b hotfix/cli-v0.2.1
# Fix the bug
git commit -m "fix: critical bug in projects command"
git push origin hotfix/cli-v0.2.1
```

### 3. Create Patch Release

```bash
git tag cli-v0.2.1
git push origin cli-v0.2.1
```

### 4. Deprecation Notice

Add to the buggy release notes:

```markdown
## ⚠️ DEPRECATION NOTICE

This release has been deprecated due to [critical bug description].
Please use v0.2.1 instead.
```

## Monitoring Releases

### Download Statistics

Check download counts:

```bash
gh release view cli-v0.1.0
```

### Docker Image Usage

Check GHCR package insights:
https://github.com/orgs/eyedea-io/packages/container/emergent-cli

### CI/CD Logs

Monitor workflow runs:
https://github.com/eyedea-io/emergent/actions/workflows/emergent-cli.yml

## Future Enhancements

- [ ] Automated Homebrew formula updates via GitHub Actions
- [ ] Chocolatey package for Windows (winget)
- [ ] Snap package for Linux
- [ ] AUR (Arch User Repository) package
- [ ] Automated changelog generation from commits
- [ ] Release candidate testing period (7 days)
- [ ] Automated security scanning of binaries

## References

- GitHub Actions Workflow: `.github/workflows/emergent-cli.yml`
- CLI Documentation: `docs/EMERGENT_CLI_STANDALONE.md`
- Technical Details: `docs/STANDALONE_CLI_IMPLEMENTATION.md`
- Homebrew Formula Writing: https://docs.brew.sh/Formula-Cookbook
- GoReleaser (alternative tool): https://goreleaser.com/
