package acceptance_test

import (
	"testing"

	"github.com/cucumber/godog"
)

// TestFeatures is the single entry point for all acceptance tests.
// Each feature file in ../../features/ maps to step definitions registered
// in the InitializeScenario function of its own _steps_test.go file.
//
// Run:  go test ./internal/acceptance/... -v
// Tags: go test ./internal/acceptance/... -v -godog.tags=@mvp
func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Name:                "cobalt-dingo",
		ScenarioInitializer: initializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../features"},
			TestingT: t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("acceptance tests failed")
	}
}

// initializeScenario wires all step definition packages.
// Add a call here each time a new _steps_test.go file is created.
func initializeScenario(sc *godog.ScenarioContext) {
	// step packages register themselves here as the project grows
	_ = sc
}
