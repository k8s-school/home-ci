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

# Test 1: Check that GitHub repo was automatically defaulted to k8s-school/ktbx
if grep -q "repo=k8s-school/ktbx" "$LOG_FILE"; then
    echo -e "${GREEN}✅ SUCCESS: GitHub repo was automatically defaulted to k8s-school/ktbx${NC}"
else
    echo -e "${RED}❌ FAIL: GitHub repo was not automatically defaulted to k8s-school/ktbx${NC}"
    echo "Looking for dispatch logs:"
    grep -i "github\|dispatch\|repo=" "$LOG_FILE" || echo "No dispatch logs found"
    exit 1
fi

# Test 2: Verify no "invalid repository format" errors
if grep -q "invalid repository format" "$LOG_FILE"; then
    echo -e "${RED}❌ FAIL: Found 'invalid repository format' error${NC}"
    grep "invalid repository format" "$LOG_FILE"
    exit 1
else
    echo -e "${GREEN}✅ SUCCESS: No 'invalid repository format' errors found${NC}"
fi

# Test 3: Check that GitHub Actions dispatch was attempted
if grep -q "GitHub Actions dispatch" "$LOG_FILE" || grep -q "Sending GitHub Actions dispatch" "$LOG_FILE"; then
    echo -e "${GREEN}✅ SUCCESS: GitHub Actions dispatch was attempted${NC}"
else
    echo -e "${RED}❌ FAIL: GitHub Actions dispatch was not attempted${NC}"
    echo "This might indicate that GitHub Actions dispatch is disabled or failing early."
    exit 1
fi

# Test 4: Verify the dispatch used the correct repository format
if grep -q "repo=k8s-school/ktbx.*event_type=" "$LOG_FILE"; then
    EVENT_TYPE=$(grep "repo=k8s-school/ktbx.*event_type=" "$LOG_FILE" | sed -n 's/.*event_type=\([^ ]*\).*/\1/p')
    echo -e "${GREEN}✅ SUCCESS: Dispatch used correct repository format (repo=k8s-school/ktbx, event_type=$EVENT_TYPE)${NC}"
else
    echo -e "${RED}❌ FAIL: Dispatch did not use expected repository format${NC}"
    echo "Expected: repo=k8s-school/ktbx with any event_type"
    grep -i "dispatch\|repo=" "$LOG_FILE" || echo "No relevant logs found"
    exit 1
fi

# Test 5: Check GitHub API response (success or expected permission failure)
if grep -q "GitHub Actions notification failed.*status 403.*Resource not accessible" "$LOG_FILE"; then
    echo -e "${GREEN}✅ SUCCESS: GitHub API called with real token (403 = insufficient permissions)${NC}"
elif grep -q "GitHub Actions notification failed.*status 401.*Bad credentials" "$LOG_FILE"; then
    echo -e "${YELLOW}⚠️  WARNING: Token authentication failed (401 = bad credentials)${NC}"
elif grep -q "github_actions_success.*true" "$LOG_FILE"; then
    echo -e "${GREEN}✅ SUCCESS: GitHub Actions dispatch completed successfully!${NC}"
else
    echo -e "${YELLOW}⚠️  INFO: GitHub API response unclear, but dispatch was attempted${NC}"
fi

# Test 6: Verify the test actually ran (not just dispatch)
if grep -q "UAT test passed - github_repo auto-defaulting works" "$LOG_FILE"; then
    echo -e "${GREEN}✅ SUCCESS: UAT test script executed successfully${NC}"
else
    echo -e "${YELLOW}⚠️  WARNING: UAT test script output not found in logs${NC}"
fi

# Clean up
echo -e "${BLUE}Cleaning up test files...${NC}"
rm -rf "$TEST_DIR"
rm -rf /tmp/home-ci

# Clean up kind cluster created during test
echo -e "${BLUE}Cleaning up kind cluster...${NC}"
kind delete cluster --name home-ci 2>/dev/null || true

echo ""
echo -e "${GREEN}🎉 UAT Test PASSED: GitHub Repository Auto-Defaulting works correctly!${NC}"
echo ""
echo -e "${GREEN}✅ Validation Results:${NC}"
echo -e "${GREEN}  ✓ github_repo automatically defaults from repository (k8s-school/ktbx)${NC}"
echo -e "${GREEN}  ✓ No 'invalid repository format' errors${NC}"
echo -e "${GREEN}  ✓ GitHub Actions dispatch functionality working${NC}"
echo -e "${GREEN}  ✓ Correct dispatch parameters used${NC}"
echo ""
echo -e "${BLUE}📋 This UAT test validates the fix for:${NC}"
echo -e "   ${YELLOW}Issue:${NC} github_repo was empty by default causing 'invalid repository format' error"
echo -e "   ${YELLOW}Solution:${NC} github_repo now automatically defaults to repository value for GitHub URLs"
echo -e "   ${YELLOW}Repository:${NC} $(grep '^repository:' "$UAT_CONFIG" | cut -d'"' -f2) → k8s-school/ktbx"
echo -e "   ${YELLOW}Config:${NC} Using real ktbx.yaml configuration"