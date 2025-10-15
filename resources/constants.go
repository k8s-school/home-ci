package resources

import (
	_ "embed"
)

// Test scripts embedded as string resources
//
//go:embed e2e/src/run-e2e.sh
var RunE2EScript string

//go:embed e2e/cleanup_e2e.sh
var CleanupE2EScript string

// Configuration files embedded as string resources
//
//go:embed e2e/conf/config-normal.yaml
var ConfigNormal string

//go:embed e2e/conf/config-timeout.yaml
var ConfigTimeout string

//go:embed e2e/conf/config-dispatch.yaml
var ConfigDispatch string

//go:embed e2e/test-expectations.yaml
var TestExpectations string
