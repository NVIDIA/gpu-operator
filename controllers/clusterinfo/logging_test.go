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

package clusterinfo

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/NVIDIA/gpu-operator/internal/consts"
)

type logRecordLevel string

const (
	logRecordLevelInfo  logRecordLevel = "info"
	logRecordLevelError logRecordLevel = "error"
)

type logRecord struct {
	level         logRecordLevel
	verbosity     int
	message       string
	err           error
	keysAndValues []any
}

// logRecorder is held by pointer because logr copies a sink on every WithName and WithValues call,
// and every copy has to append to the same slice.
type logRecorder struct {
	mu      sync.Mutex
	records []logRecord
}

func (r *logRecorder) record(newRecord logRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, newRecord)
}

func (r *logRecorder) capturedRecords() []logRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.records)
}

func (r *logRecorder) capturedRecordsAtLevel(level logRecordLevel) []logRecord {
	var matchingRecords []logRecord
	for _, capturedRecord := range r.capturedRecords() {
		if capturedRecord.level == level {
			matchingRecords = append(matchingRecords, capturedRecord)
		}
	}
	return matchingRecords
}

func (r *logRecorder) capturedMessages(level logRecordLevel) []string {
	var messages []string
	for _, capturedRecord := range r.capturedRecordsAtLevel(level) {
		messages = append(messages, capturedRecord.message)
	}
	return messages
}

// funcr.New is not used here because it routes Info and Error through one write function, which
// would leave the level recoverable only by parsing the formatted output.
type capturingLogSink struct {
	funcr.Formatter
	recorder *logRecorder
}

func (s capturingLogSink) WithName(name string) logr.LogSink {
	s.AddName(name)
	return &s
}

func (s capturingLogSink) WithValues(kvList ...any) logr.LogSink {
	s.AddValues(kvList)
	return &s
}

func (s capturingLogSink) Info(verbosity int, message string, keysAndValues ...any) {
	s.recorder.record(logRecord{
		level:         logRecordLevelInfo,
		verbosity:     verbosity,
		message:       message,
		keysAndValues: keysAndValues,
	})
}

func (s capturingLogSink) Error(err error, message string, keysAndValues ...any) {
	s.recorder.record(logRecord{
		level:         logRecordLevelError,
		message:       message,
		err:           err,
		keysAndValues: keysAndValues,
	})
}

func capturingLogger() (logr.Logger, *logRecorder) {
	recorder := &logRecorder{}
	capturingSink := capturingLogSink{
		Formatter: funcr.NewFormatter(funcr.Options{Verbosity: consts.LogLevelDebug}),
		recorder:  recorder,
	}
	return logr.New(&capturingSink), recorder
}

func testContextWithCapturedLogs(t *testing.T) (context.Context, *logRecorder) {
	logger, recorder := capturingLogger()
	return log.IntoContext(t.Context(), logger), recorder
}
