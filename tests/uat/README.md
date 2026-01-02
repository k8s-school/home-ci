# UAT Tests (User Acceptance Tests)

This directory contains User Acceptance Tests that validate end-to-end functionality of home-ci features.

## Structure

```
tests/uat/
├── README.md                      # This documentation
├── test-github-repo-default.sh    # UAT for GitHub repo auto-defaulting
└── ktbx.yaml                      # Real ktbx.yaml configuration (copy of examples/ktbx.yaml)
```

## Tests Available

### GitHub Repository Auto-Defaulting UAT

**File:** `test-github-repo-default.sh`
**Config:** `ktbx.yaml` (copy of `examples/ktbx.yaml` without explicit `github_repo`)

**Purpose:** Validates that `github_actions_dispatch.github_repo` automatically defaults to the repository value when not explicitly set.

**What it tests:**
- ✅ `github_repo` is automatically set from `repository: "https://github.com/k8s-school/ktbx.git"` to `k8s-school/ktbx`
- ✅ No "invalid repository format" errors occur
- ✅ GitHub Actions dispatch is attempted with correct parameters
- ✅ End-to-end functionality works as expected

**Prerequisites:**
- Real GitHub token file must exist at: `/home/fjammes/src/github.com/k8s-school/ktbx/secret.yaml`
- ktbx project must be available
- `kind` must be installed for Kubernetes testing

**How to run:**
```bash
# Via Makefile (recommended)
make test-uat-github-repo-default

# Or directly
./tests/uat/test-github-repo-default.sh
```

**Note:** This test will fail if the real GitHub token is not available. This is intentional to ensure UAT tests use real credentials and provide accurate validation.

**Expected behavior:**
The test uses the real `examples/ktbx.yaml` configuration but with `github_repo` deliberately omitted (removed from the copy). The system should automatically extract `k8s-school/ktbx` from the repository URL `https://github.com/k8s-school/ktbx.git` and use it for GitHub Actions dispatch.

## Design Philosophy

These UAT tests are designed to:

1. **Test real-world scenarios** - Use realistic configurations similar to production
2. **Validate fixes** - Ensure bugs don't regress
3. **End-to-end validation** - Test complete workflows, not just individual functions
4. **Clear feedback** - Provide detailed, colored output showing exactly what passed/failed
5. **CI/CD integration** - Can be run both locally and in GitHub Actions

## Configuration Notes

- **Repository URLs**: Tests use real GitHub repositories to ensure URL parsing works correctly
- **Real tokens required**: Requires real GitHub token from ktbx project - test fails if not available (no fake token fallback)
- **Temporary files**: All tests clean up after themselves
- **Relative paths**: Configuration files use relative paths for better portability

## Adding New UAT Tests

When adding new UAT tests:

1. Create a descriptive test script: `test-{feature-name}.sh`
2. Add corresponding configuration file: `{feature-name}.yaml`
3. Update this README with test description
4. Add make target in root Makefile
5. Add GitHub Actions job in `.github/workflows/e2e.yaml`
6. Ensure proper cleanup and error handling