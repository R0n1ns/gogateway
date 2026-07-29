// Package policytest запускает декларативные policy cases в CI.
package policytest

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/simulator"
)

type File struct {
	Cases []Case `json:"cases"`
}
type Case struct {
	Name     string            `json:"name"`
	Request  simulator.Request `json:"request"`
	Expected Expected          `json:"expected"`
}
type Expected struct {
	Allowed  *bool  `json:"allowed,omitempty"`
	Status   int    `json:"status,omitempty"`
	Action   string `json:"action,omitempty"`
	PolicyID string `json:"policy_id,omitempty"`
	RouteID  string `json:"route_id,omitempty"`
}
type Result struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var file File
	err = json.Unmarshal(data, &file)
	return file, err
}
func Run(cfg *config.Config, file File) ([]Result, error) {
	sim, err := simulator.New(cfg)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(file.Cases))
	for _, testCase := range file.Cases {
		actual := sim.Simulate(testCase.Request)
		message := compare(testCase.Expected, actual)
		results = append(results, Result{Name: testCase.Name, Passed: message == "", Message: message})
	}
	return results, nil
}
func compare(expected Expected, actual simulator.Result) string {
	if expected.RouteID != "" && actual.RouteID != expected.RouteID {
		return fmt.Sprintf("route_id: получено %q, ожидалось %q", actual.RouteID, expected.RouteID)
	}
	decision := actual.Decision
	if expected.Allowed != nil && decision.Allowed != *expected.Allowed {
		return fmt.Sprintf("allowed: получено %v, ожидалось %v", decision.Allowed, *expected.Allowed)
	}
	if expected.Status != 0 && decision.Status != expected.Status {
		return fmt.Sprintf("status: получено %d, ожидалось %d", decision.Status, expected.Status)
	}
	if expected.Action != "" && decision.Action != expected.Action {
		return fmt.Sprintf("action: получено %q, ожидалось %q", decision.Action, expected.Action)
	}
	if expected.PolicyID != "" && decision.PolicyID != expected.PolicyID {
		return fmt.Sprintf("policy_id: получено %q, ожидалось %q", decision.PolicyID, expected.PolicyID)
	}
	return ""
}
