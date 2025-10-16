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
//go:embed e2e/conf/config-success.yaml
var ConfigSuccess string

//go:embed e2e/conf/config-fail.yaml
var ConfigFail string

//go:embed e2e/conf/config-timeout.yaml
var ConfigTimeout string

//go:embed e2e/conf/config-dispatch-one-success.yaml
var ConfigDispatchOneSuccess string

//go:embed e2e/conf/config-dispatch-all.yaml
var ConfigDispatchAll string

//go:embed e2e/conf/config-quick.yaml
var ConfigQuick string

//go:embed e2e/conf/config-normal.yaml
var ConfigNormal string

//go:embed e2e/conf/config-long.yaml
var ConfigLong string

//go:embed e2e/test-expectations.yaml
var TestExpectations string
