# Bankshot Release Process Plan

This document outlines the plan for establishing a comprehensive release process for bankshot, including semantic versioning, automated builds, and Homebrew tap management.

## Overview

The release process will:
1. Use semantic versioning (semver) for all releases
2. Automate builds and releases using GitHub Actions and GoReleaser
3. Automatically publish to a Homebrew tap repository
4. Use GitHub App tokens for cross-repository permissions

## Phase 1: Semantic Versioning Setup

### Tasks
- [ ] Create version.go file with version constants
- [ ] Update build process to embed version information
- [ ] Create git tags for existing releases (if any)
- [ ] Document versioning strategy in CONTRIBUTING.md

### Implementation Details
- Version format: `vMAJOR.MINOR.PATCH` (e.g., v1.0.0)
- Version will be embedded at build time using `-ldflags`
- Current version will be displayed in:
  - `bankshot --version` (needs to be added)
  - `bankshotd` startup logs (already implemented)
  - Daemon status response (already shows version)

### Current State
- `cmd/bankshotd/main.go` already has version variables (version, commit, date)
- Version is currently hardcoded as "0.1.0" in daemon
- No version command in bankshot CLI yet

## Phase 2: GitHub Actions Release Workflow

### Tasks
- [ ] Create `.github/workflows/release.yml`
- [ ] Configure GoReleaser with `.goreleaser.yml`
- [ ] Set up build matrix for multiple platforms:
  - macOS (amd64, arm64)
  - Linux (amd64, arm64)
- [ ] Configure artifact signing (optional)
- [ ] Set up changelog generation

### Trigger Conditions
- Manual workflow dispatch
- Push of tags matching `v*.*.*`

### Workflow Steps
1. Checkout code
2. Set up Go environment
3. Run tests
4. Build binaries with GoReleaser
5. Create GitHub release with artifacts
6. Generate and push Homebrew formula

## Phase 3: Homebrew Tap Setup

### Tasks
- [ ] Create `phinze/homebrew-bankshot` repository
- [ ] Create initial Formula structure
- [ ] Configure GoReleaser to generate formula
- [ ] Set up GitHub App for cross-repo access
- [ ] Configure automatic formula updates

### Repository Structure
```
homebrew-bankshot/
├── Formula/
│   └── bankshot.rb
├── README.md
└── .github/
    └── workflows/
        └── tests.yml
```

### GitHub App Configuration
1. Create a new GitHub App with:
   - Repository permissions:
     - Contents: Write (for homebrew-bankshot)
     - Pull requests: Write (optional, for formula PRs)
   - Repository access: Selected repositories
     - phinze/bankshot
     - phinze/homebrew-bankshot

2. Install the app on both repositories

3. Store credentials:
   - App ID → GitHub secret `APP_ID`
   - Private key → GitHub secret `APP_PRIVATE_KEY`

### GoReleaser Homebrew Configuration
```yaml
brews:
  - name: bankshot
    repository:
      owner: phinze
      name: homebrew-bankshot
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    folder: Formula
    homepage: "https://github.com/phinze/bankshot"
    description: "Automatic SSH port forwarding for remote development"
    license: "MIT"
    dependencies:
      - name: ssh
        type: optional
    install: |
      bin.install "bankshot"
      bin.install "bankshotd"
    test: |
      system "#{bin}/bankshot", "--version"
```

## Phase 4: Release Process Integration

### Automated Release Flow
1. Developer creates and pushes a semver tag
2. GitHub Actions workflow triggers
3. GoReleaser builds binaries for all platforms
4. GitHub release is created with:
   - Binary artifacts
   - Checksums
   - Changelog
5. Homebrew formula is generated and pushed to tap
6. Users can install via `brew install phinze/bankshot/bankshot`

### Manual Release Checklist
- [ ] Update CHANGELOG.md
- [ ] Bump version in version.go
- [ ] Create git tag: `git tag -a v1.0.0 -m "Release v1.0.0"`
- [ ] Push tag: `git push origin v1.0.0`
- [ ] Monitor GitHub Actions for successful completion
- [ ] Verify Homebrew formula was updated
- [ ] Test installation: `brew install phinze/bankshot/bankshot`

## Implementation Order

1. **Version Management** (Day 1)
   - Create version.go
   - Update build scripts
   - Update CLI to show version

2. **Basic GitHub Actions** (Day 1)
   - Create release workflow
   - Basic GoReleaser config
   - Test with manual trigger

3. **Homebrew Tap Setup** (Day 2)
   - Create tap repository
   - Initial formula
   - Manual test of formula

4. **GitHub App Integration** (Day 2)
   - Create and configure GitHub App
   - Update workflow to use app token
   - Test cross-repo push

5. **Full Integration Test** (Day 3)
   - Tag a release
   - Verify full automated flow
   - Document any issues

## Success Criteria

- [ ] Version information displayed in binaries
- [ ] GitHub Actions successfully builds on tag push
- [ ] Binaries available as GitHub release artifacts
- [ ] Homebrew formula automatically updated
- [ ] Users can install via `brew install phinze/bankshot/bankshot`
- [ ] Installation works on both Intel and Apple Silicon Macs

## Future Enhancements

- Automatic version bumping based on conventional commits
- Beta/pre-release channel support
- Linux package repositories (apt, yum)
- Container images (Docker Hub, GitHub Container Registry)
- Signed binaries for macOS notarization
- Automated testing of Homebrew formula