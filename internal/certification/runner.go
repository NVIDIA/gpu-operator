/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package certification

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/NVIDIA/gpu-operator/internal/certification/environment"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
	timeutils "github.com/NVIDIA/gpu-operator/internal/certification/utils/time"
)

type Runner struct {
	clients *k8s.Clients
	config  Config
}

func NewRunner(clients *k8s.Clients, cfg Config) *Runner {
	return &Runner{
		clients: clients,
		config:  cfg,
	}
}

func (r *Runner) Run(ctx context.Context) *SuiteResult {
	suite := &SuiteResult{
		RunID:     os.Getenv("POD_UID"),
		StartTime: time.Now(),
	}

	// Capture environment snapshot before running tests
	log.Println("=== Capturing Environment Snapshot ===")
	suite.Environment, _ = environment.NewSnapshotBuilder(r.clients, r.config.Namespace).
		Build(ctx)

	// Capture original settings for restoration after each TestSet.
	// Retry on transient errors — without this, all restoration is silently
	// disabled and tests mutate the cluster with no rollback.
	log.Println("=== Capturing Original Settings ===")
	timeout := r.config.ParseTimeout()
	originalSettings, err := CaptureOriginalSettings(ctx, r.clients, timeout)
	if err != nil {
		log.Printf("FATAL: cannot capture original settings: %v", err)
		log.Println("Aborting — running tests without rollback protection risks leaving the cluster in a modified state.")
		suite.EndTime = time.Now()
		suite.Duration = timeutils.GetElapsedTime(suite.StartTime, suite.EndTime)
		return suite
	}

	testSets := r.selectTestSets()

	for _, ts := range testSets {
		log.Printf("=== TestSet [%s]: %s ===", ts.Name, ts.Description)

		tsResult := TestSetResult{
			ID:          ts.Name,
			Description: ts.Description,
			StartTime:   time.Now(),
			Status:      StatusPassed,
		}
		anySkipped := false

		for i, t := range ts.Tests {
			header := fmt.Sprintf("[%d/%d] %s", i+1, len(ts.Tests), t.Name())
			log.Printf("  %s RUNNING", header)
			tsResult.Details = append(tsResult.Details, "▸ "+header+" RUNNING")

			result := t.Run(ctx, r.clients, r.config)
			log.Printf("  %s %s", header, result.Status)

			// Capture test details and status
			tsResult.Details = append(tsResult.Details, result.Details...)
			tsResult.Details = append(tsResult.Details, "▸ "+header+" "+string(result.Status))

			tsResult.Tests = append(tsResult.Tests, *result)

			if result.Status == StatusFailed {
				tsResult.Status = StatusFailed
				tsResult.Error = result.Error
				break
			}
			if result.Status == StatusSkipped {
				anySkipped = true
			}
		}

		if anySkipped && tsResult.Status == StatusPassed {
			tsResult.Status = StatusSkipped
		}

		tsResult.EndTime = time.Now()
		tsResult.Duration = timeutils.GetElapsedTime(tsResult.StartTime, tsResult.EndTime)
		suite.TestSets = append(suite.TestSets, tsResult)

		// Restore original settings after each TestSet
		log.Printf("=== Restoring Original Settings after [%s] ===", ts.Name)
		originalSettings.Restore(ctx, r.clients, r.config.Namespace, timeout)
	}

	suite.EndTime = time.Now()
	suite.Duration = timeutils.GetElapsedTime(suite.StartTime, suite.EndTime)
	suite.ComputeSummary()

	if suite.Summary.Failed > 0 {
		r.printFailureGuidance()
	}

	return suite
}

func (r *Runner) printFailureGuidance() {
	log.Println("")
	log.Println("=== Debugging Assistance ===")
	log.Println("For debugging assistance, collect cluster diagnostics:")
	log.Println("  curl -sLO https://raw.githubusercontent.com/NVIDIA/gpu-operator/main/hack/must-gather.sh")
	log.Println("  chmod +x must-gather.sh && ./must-gather.sh")
	log.Println("")
	log.Println("Attach the generated tarball when opening a support case.")
}

func (r *Runner) selectTestSets() []*TestSet {
	if len(r.config.TestSets) == 0 {
		log.Printf("No testSets specified, nothing to run")
		return nil
	}

	include := make(map[string]bool)
	exclude := make(map[string]bool)

	for _, entry := range r.config.TestSets {
		switch {
		case entry == "all":
			for name := range AllTestSets() {
				include[name] = true
			}
		case strings.HasPrefix(entry, "!"):
			exclude[strings.TrimPrefix(entry, "!")] = true
		default:
			include[entry] = true
		}
	}

	// Exclusions override inclusions
	for name := range exclude {
		delete(include, name)
	}

	var selected []*TestSet
	for name := range include {
		if ts := TestSetByName(name); ts != nil {
			selected = append(selected, ts)
		} else {
			log.Printf("TestSet [%s] not found in registry", name)
		}
	}

	sort.Slice(selected, func(i, j int) bool {
		return selected[i].Name < selected[j].Name
	})
	return selected
}
