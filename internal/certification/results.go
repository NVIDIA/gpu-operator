package certification

import (
	"fmt"
	"log"
	"time"

	"github.com/NVIDIA/gpu-operator-self-certification/internal/environment"
	timeutils "github.com/NVIDIA/gpu-operator-self-certification/internal/utils/time"
)

type Status string

const (
	StatusPassed  Status = "passed"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

// TestResult is used internally by individual tests.
type TestResult struct {
	Name      string    `json:"name"`
	Status    Status    `json:"status"`
	StartTime time.Time `json:"startTime"`
	EndTime   time.Time `json:"endTime"`
	Duration  string    `json:"duration"`
	Error     string    `json:"error,omitempty"`
	Details   []string  `json:"details,omitempty"`
}

func NewTestResult(name string) *TestResult {
	return &TestResult{
		Name:      name,
		StartTime: time.Now(),
		Status:    StatusPassed,
	}
}

func (r *TestResult) Finalize() {
	r.EndTime = time.Now()
	r.Duration = timeutils.GetElapsedTime(r.StartTime, r.EndTime)
}

func (r *TestResult) Fail(format string, args ...any) {
	r.Status = StatusFailed
	r.Error = fmt.Sprintf(format, args...)
}

func (r *TestResult) Skip(format string, args ...any) {
	r.Status = StatusSkipped
	r.AddDetail(format, args...)
}

func (r *TestResult) AddDetail(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("  → %s", msg)
	r.Details = append(r.Details, "→ "+msg)
}

type TestSetResult struct {
	ID          string       `json:"id"`
	Description string       `json:"description"`
	Status      Status       `json:"status"`
	StartTime   time.Time    `json:"startTime"`
	EndTime     time.Time    `json:"endTime"`
	Duration    string       `json:"duration"`
	Error       string       `json:"error,omitempty"`
	Details     []string     `json:"details,omitempty"`
	Tests       []TestResult `json:"tests"`
}

type SuiteResult struct {
	RunID       string                           `json:"runId"`
	StartTime   time.Time                        `json:"startTime"`
	EndTime     time.Time                        `json:"endTime"`
	Duration    string                           `json:"duration"`
	Environment *environment.EnvironmentSnapshot `json:"environment,omitempty"`
	TestSets    []TestSetResult                  `json:"testSets"`
	Summary     Summary                          `json:"summary"`
}

type Summary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

func (s *SuiteResult) ComputeSummary() {
	for _, ts := range s.TestSets {
		s.Summary.Total++
		switch ts.Status {
		case StatusPassed:
			s.Summary.Passed++
		case StatusFailed:
			s.Summary.Failed++
		case StatusSkipped:
			s.Summary.Skipped++
		}
	}
}
