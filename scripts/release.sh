#!/usr/bin/env bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
info() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Check if required tools are installed
command -v svu >/dev/null 2>&1 || error "svu is not installed. Please install it first."
command -v git >/dev/null 2>&1 || error "git is not installed."
command -v gh >/dev/null 2>&1 || error "gh (GitHub CLI) is not installed. Please install it first."
command -v jq >/dev/null 2>&1 || error "jq is not installed. Please install it first."

# Ensure we're on the main branch
CURRENT_BRANCH=$(git branch --show-current)
if [[ "$CURRENT_BRANCH" != "main" ]]; then
    error "You must be on the main branch to release. Current branch: $CURRENT_BRANCH"
fi

# Ensure working directory is clean
if [[ -n $(git status --porcelain) ]]; then
    error "Working directory is not clean. Please commit or stash your changes."
fi

# Fetch latest tags
info "Fetching latest tags..."
git fetch --tags

# Get current version and next version
CURRENT_VERSION=$(svu current)
info "Current version: $CURRENT_VERSION"

# Allow overriding the version bump type
BUMP_TYPE="${1:-}"
if [[ -z "$BUMP_TYPE" ]]; then
    # Use svu to determine next version based on commit messages
    NEXT_VERSION=$(svu next)
else
    # Use specific bump type
    case "$BUMP_TYPE" in
    major | minor | patch)
        NEXT_VERSION=$(svu "$BUMP_TYPE")
        ;;
    *)
        error "Invalid bump type: $BUMP_TYPE. Use 'major', 'minor', or 'patch'."
        ;;
    esac
fi

info "Next version will be: $NEXT_VERSION"

# Show commits since current version
echo
info "Changes since $CURRENT_VERSION:"
echo
git log --oneline --no-decorate "$CURRENT_VERSION"..HEAD
echo

# Confirm release
read -p "Do you want to release version $NEXT_VERSION? (y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    warn "Release cancelled."
    exit 0
fi

# Create and push tag
info "Creating tag $NEXT_VERSION..."
git tag -a "$NEXT_VERSION" -m "Release $NEXT_VERSION"

info "Pushing tag to origin..."
git push origin "$NEXT_VERSION"

info "Tag $NEXT_VERSION pushed successfully!"

# Poll for the release workflow run
info "Waiting for release workflow to start..."
POLL_ATTEMPTS=0
MAX_POLL_ATTEMPTS=10
WORKFLOW_URL=""

while [[ $POLL_ATTEMPTS -lt $MAX_POLL_ATTEMPTS ]]; do
    POLL_ATTEMPTS=$((POLL_ATTEMPTS + 1))

    # Get the latest run for the release workflow
    RUN_INFO=$(gh run list --workflow=release.yml --limit=1 --json headBranch,url,status 2>/dev/null || echo "")

    if [[ -n "$RUN_INFO" ]]; then
        # Check if this run is for our tag
        HEAD_BRANCH=$(echo "$RUN_INFO" | jq -r '.[0].headBranch' 2>/dev/null || echo "")
        if [[ "$HEAD_BRANCH" == "$NEXT_VERSION" ]]; then
            WORKFLOW_URL=$(echo "$RUN_INFO" | jq -r '.[0].url' 2>/dev/null || echo "")
            break
        fi
    fi

    # Wait a bit before polling again
    sleep 2
done

if [[ -n "$WORKFLOW_URL" ]]; then
    info "Release workflow started!"
    info "Monitor progress at: $WORKFLOW_URL"
else
    warn "Could not find release workflow run. Check manually at:"
    warn "https://github.com/phinze/bankshot/actions/workflows/release.yml"
fi

