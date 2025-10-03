#!/bin/bash
set -e

# Disable git pager to avoid interactions
export GIT_PAGER=cat

# Configuration
REPO_PATH="/tmp/test-repo-local"

echo "=== Setting up test repository ==="
echo "Repository path: $REPO_PATH"

# Clean up existing repository if there is one
if [ -d "$REPO_PATH" ]; then
    echo "Removing existing test repository..."
    rm -rf "$REPO_PATH"
fi

# Create the new repository
echo "Creating new test repository..."
mkdir -p "$REPO_PATH"
cd "$REPO_PATH"

# Initialiser git
git init
git config user.name "CI Test"
git config user.email "ci-test@example.com"
# Configuration to avoid interactions
git config advice.detachedHead false
git config init.defaultBranch main
git config pager.branch false
git config pager.log false
git config core.pager cat

# Create basic structure
echo "Creating basic structure..."
mkdir -p _e2e

# Copier le script e2e depuis le template du projet home-ci
SCRIPT_DIR="$(dirname "$0")"
cp "$SCRIPT_DIR/run-e2e.sh" _e2e/run-e2e.sh

chmod +x _e2e/run-e2e.sh

# Create some basic files
echo "# Test Repository" > README.md
echo "node_modules/" > .gitignore
echo "*.log" >> .gitignore

# Premier commit et renommer la branche en main
git add .
git commit -m "Initial commit"
git branch -m main

# Create some test branches with commits
echo "Creating test branches..."

# Branch feature/test1
git checkout -b feature/test1
echo "Feature 1 content" > feature1.txt
git add feature1.txt
git commit -m "Add feature 1"

echo "Feature 1 update" >> feature1.txt
git add feature1.txt
git commit -m "Update feature 1"

# Branch feature/test2
git checkout -b feature/test2
echo "Feature 2 content" > feature2.txt
git add feature2.txt
git commit -m "Add feature 2"

# Branch bugfix/critical
git checkout -b bugfix/critical
echo "Bug fix content" > bugfix.txt
git add bugfix.txt
git commit -m "Fix critical bug"

# Retourner sur main et faire quelques commits
git checkout main
echo "Main branch update 1" > main-update.txt
git add main-update.txt
git commit -m "Main update 1"

echo "Main branch update 2" >> main-update.txt
git add main-update.txt
git commit -m "Main update 2"

# Display final state
echo ""
echo "=== Repository setup complete ==="
echo "Available branches:"
git branch -a
echo ""
echo "Recent commits on main:"
git log --oneline -5
echo ""
echo "Repository ready at: $REPO_PATH"