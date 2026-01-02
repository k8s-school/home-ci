#!/bin/bash

# UAT Test: GitHub Repository Auto-Defaulting
# Tests that github_repo automatically defaults to the repository value when not explicitly set
# This test validates the fix for the "invalid repository format" error

set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TEST_DIR="/tmp/uat-github-repo-default"
HOME_CI_BINARY="${HOME_CI_BINARY:-$PROJECT_ROOT/home-ci}"
UAT_CONFIG="$SCRIPT_DIR/ktbx.yaml"
LOG_FILE="$TEST_DIR/uat-test.log"

echo -e "${BLUE}=== UAT Test: GitHub Repository Auto-Defaulting ===${NC}"
echo -e "${BLUE}Project root: $PROJECT_ROOT${NC}"
echo -e "${BLUE}UAT config: $UAT_CONFIG${NC}"

# Clean up from previous runs
rm -rf "$TEST_DIR"
mkdir -p "$TEST_DIR"

# Clean up any existing kind cluster that might conflict
echo -e "${BLUE}Cleaning up any existing kind cluster...${NC}"
kind delete cluster --name home-ci 2>/dev/null || true

# Verify real token file from ktbx project is available
KTBX_TOKEN="/home/fjammes/src/github.com/k8s-school/ktbx/secret.yaml"
if [[ -f "$KTBX_TOKEN" ]]; then
    echo -e "${BLUE}✅ Using real GitHub token from ktbx project${NC}"
    # No need to copy, ktbx.yaml already points to the right location
else
    echo -e "${RED}❌ FAIL: Real GitHub token not found at $KTBX_TOKEN${NC}"
    echo -e "${RED}This UAT test requires a real GitHub token to validate end-to-end functionality.${NC}"
    echo -e "${YELLOW}Please ensure the ktbx project token file exists, or skip this UAT test.${NC}"
    exit 1
fi

# Verify UAT configuration file exists
if [[ ! -f "$UAT_CONFIG" ]]; then
    echo -e "${RED}❌ FAIL: UAT configuration file not found: $UAT_CONFIG${NC}"
    exit 1
fi

# Verify home-ci binary exists
if [[ ! -x "$HOME_CI_BINARY" ]]; then
    echo -e "${RED}❌ FAIL: home-ci binary not found at $HOME_CI_BINARY${NC}"
    echo "Please build the binary first: make build"
    exit 1
fi

echo -e "${BLUE}Configuration summary:${NC}"
echo -e "  ${YELLOW}Repository:${NC} $(grep '^repository:' "$UAT_CONFIG" | cut -d'"' -f2)"
echo -e "  ${YELLOW}GitHub Repo:${NC} $(grep '^  github_repo:' "$UAT_CONFIG" || echo 'NOT SET (this is what we are testing!)')"
echo -e "  ${YELLOW}Dispatch Enabled:${NC} $(grep '^  enabled:' "$UAT_CONFIG" | awk '{print $2}')"

# Run the UAT test
echo -e "${BLUE}Running UAT test...${NC}"
cd "$PROJECT_ROOT"
"$HOME_CI_BINARY" run -c "$UAT_CONFIG" -b main -v 3 2>&1 | tee "$LOG_FILE"

echo -e "${BLUE}Analyzing test results...${NC}"

# Extract commit from log output
COMMIT=$(grep "Using latest commit" "$LOG_FILE" | sed -n 's/.*: \([a-f0-9]*\).*/\1/p' | head -1)
if [[ -z "$COMMIT" ]]; then
    COMMIT=$(grep "Running tests for branch" "$LOG_FILE" | sed -n "s/.*at commit '\\([a-f0-9]*\\)'.*/\\1/p" | head -1)
fi

echo -e "${BLUE}Detected commit: $COMMIT${NC}"

# Find the result JSON file using the detected commit
RESULT_FILE=""
if [[ -n "$COMMIT" ]]; then
    RESULT_FILE=$(find /tmp -name "*main_${COMMIT}*.json" -path "*/home-ci/*" 2>/dev/null | head -1)
fi

if [[ ! -f "$RESULT_FILE" ]]; then
    # Fallback: construct expected path from log file path
    RESULT_FILE="${LOG_FILE%.log}.json"
fi

echo -e "${BLUE}Using result file: $RESULT_FILE${NC}"

# Main test: Check GitHub API response using JSON result file
if [[ ! -f "$RESULT_FILE" ]]; then
    echo -e "${RED}❌ FAIL: JSON result file not found: $RESULT_FILE${NC}"
    echo "UAT test requires JSON result file for accurate validation."
    exit 1
fi

echo -e "${BLUE}Parsing JSON result file...${NC}"

# Check if GitHub Actions was notified and successful
GITHUB_NOTIFIED=$(jq -r '.github_actions_notified // false' "$RESULT_FILE" 2>/dev/null)
GITHUB_SUCCESS=$(jq -r '.github_actions_success // false' "$RESULT_FILE" 2>/dev/null)
GITHUB_ERROR=$(jq -r '.github_actions_error_message // ""' "$RESULT_FILE" 2>/dev/null)
SUCCESS=$(jq -r '.success // false' "$RESULT_FILE" 2>/dev/null)

echo -e "${BLUE}  Test success: $SUCCESS${NC}"
echo -e "${BLUE}  GitHub Actions notified: $GITHUB_NOTIFIED${NC}"
echo -e "${BLUE}  GitHub Actions success: $GITHUB_SUCCESS${NC}"

# Validate all conditions for UAT success
if [[ "$SUCCESS" == "true" && "$GITHUB_NOTIFIED" == "true" && "$GITHUB_SUCCESS" == "true" ]]; then
    echo -e "${GREEN}✅ SUCCESS: UAT test passed - GitHub repo auto-defaulting works correctly!${NC}"
elif [[ "$SUCCESS" == "false" ]]; then
    echo -e "${RED}❌ FAIL: Test execution failed${NC}"
    if [[ -n "$GITHUB_ERROR" ]]; then
        echo -e "${RED}Error: $GITHUB_ERROR${NC}"
    fi
    exit 1
elif [[ "$GITHUB_NOTIFIED" != "true" || "$GITHUB_SUCCESS" != "true" ]]; then
    echo -e "${RED}❌ FAIL: GitHub Actions dispatch failed${NC}"
    echo -e "${RED}Notified: $GITHUB_NOTIFIED, Success: $GITHUB_SUCCESS${NC}"
    if [[ -n "$GITHUB_ERROR" ]]; then
        echo -e "${RED}Error: $GITHUB_ERROR${NC}"
    fi
    exit 1
fi


# Clean up
echo -e "${BLUE}Cleaning up test files...${NC}"
rm -rf "$TEST_DIR"
rm -rf /tmp/home-ci

# Clean up kind cluster created during test
echo -e "${BLUE}Cleaning up kind cluster...${NC}"
kind delete cluster --name home-ci 2>/dev/null || true

echo ""
echo -e "${GREEN}🎉 UAT Test PASSED${NC}"
echo ""
echo -e "${GREEN}✅ Validation Results:${NC}"
echo -e "${GREEN}  ✓ Test execution successful${NC}"
echo -e "${GREEN}  ✓ GitHub Actions dispatch successful${NC}"
echo -e "${GREEN}  ✓ GitHub repo auto-defaulting functional${NC}"
echo ""
echo -e "${BLUE}📋 This UAT test validates the fix for:${NC}"
echo -e "   ${YELLOW}Issue:${NC} github_repo was empty by default causing 'invalid repository format' error"
echo -e "   ${YELLOW}Solution:${NC} github_repo now automatically defaults to repository value for GitHub URLs"
echo -e "   ${YELLOW}Repository:${NC} $(grep '^repository:' "$UAT_CONFIG" | cut -d'"' -f2) → k8s-school/ktbx"
echo -e "   ${YELLOW}Config:${NC} Using real ktbx.yaml configuration"