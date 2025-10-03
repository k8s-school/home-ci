package resources

import (
	_ "embed"
)

// Test scripts embedded as string resources
//go:embed run-e2e.sh
var RunE2EScript string

//go:embed run-slow-test.sh
var RunSlowTestScript string

//go:embed setup-test-repo.sh
var SetupTestRepoScript string

//go:embed cleanup_e2e.sh
var CleanupE2EScript string

// Configuration files embedded as string resources
//go:embed config-normal.yaml
var ConfigNormal string

//go:embed config-timeout.yaml
var ConfigTimeout string