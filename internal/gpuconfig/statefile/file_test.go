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

package statefile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var testNow = time.Date(2026, 9, 26, 8, 15, 2, 500, time.UTC)

func newTestStore(t *testing.T) *Store {
	s := New(t.TempDir())
	s.now = func() time.Time { return testNow }
	return s
}

func writeState(t *testing.T, s *Store, content string) {
	require.NoError(t, os.WriteFile(filepath.Join(s.dir, StateFileName), []byte(content), 0o600))
}

func readState(t *testing.T, s *Store) string {
	b, err := os.ReadFile(filepath.Join(s.dir, StateFileName))
	require.NoError(t, err)
	return string(b)
}

func TestReadMissingFile(t *testing.T) {
	f, err := newTestStore(t).Read()
	require.NoError(t, err)
	require.False(t, f.Exists())
	require.Nil(t, f.Apply)
	require.Nil(t, f.Gate)
}

func TestReadIOErrorIsNotParseError(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, os.Mkdir(filepath.Join(s.dir, StateFileName), 0o700))
	_, err := s.Read()
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrParse)
}

func TestWrites(t *testing.T) {
	cases := map[string]struct {
		existing string
		write    func(*LockedStore) error
		want     string
	}{
		"gate creates the file": {
			write: func(l *LockedStore) error { return l.WriteGatePass(7) },
			want:  `{"created":"2026-09-26T08:15:02Z","gate":{"module":7,"time":"2026-09-26T08:15:02Z"},"version":1}`,
		},
		"applier creates the file": {
			write: func(l *LockedStore) error { return l.WriteApply(7, Skipped, "") },
			want:  `{"apply":{"module":7,"value":"skipped","config":"","time":"2026-09-26T08:15:02Z"},"created":"2026-09-26T08:15:02Z","version":1}`,
		},
		"gate writes module 0 explicitly": {
			existing: `{"version":1,"created":"2026-01-01T00:00:00Z"}`,
			write:    func(l *LockedStore) error { return l.WriteGatePass(0) },
			want:     `{"created":"2026-01-01T00:00:00Z","gate":{"module":0,"time":"2026-09-26T08:15:02Z"},"version":1}`,
		},
		"applier carries gate and top-level keys, drops its own unknown fields": {
			existing: `{"version":1,"created":"2026-01-01T00:00:00Z","future":{"module":3, "x":"<"},` +
				`"apply":{"module":3,"value":"pending","config":"sha256:aa","time":"2026-01-01T00:00:00Z","extra":1},` +
				`"gate":{"module":3,"time":"2026-01-01T00:00:01Z","extra":2}}`,
			write: func(l *LockedStore) error { return l.WriteApply(3, Applied, "sha256:bb") },
			want: `{"apply":{"module":3,"value":"applied","config":"sha256:bb","time":"2026-09-26T08:15:02Z"},` +
				// Carried keys keep their meaning, re-emitted compacted and HTML-escaped.
				`"created":"2026-01-01T00:00:00Z","future":{"module":3,"x":"` + "\\u003c" + `"},` +
				`"gate":{"module":3,"time":"2026-01-01T00:00:01Z","extra":2},"version":1}`,
		},
		"gate carries apply and drops its own unknown fields": {
			existing: `{"version":1,"created":"2026-01-01T00:00:00Z",` +
				`"apply":{"module":3,"value":"applied","config":"","time":"2026-01-01T00:00:00Z","extra":1},` +
				`"gate":{"module":2,"time":"2026-01-01T00:00:01Z","extra":2}}`,
			write: func(l *LockedStore) error { return l.WriteGatePass(3) },
			want: `{"apply":{"module":3,"value":"applied","config":"","time":"2026-01-01T00:00:00Z","extra":1},` +
				`"created":"2026-01-01T00:00:00Z","gate":{"module":3,"time":"2026-09-26T08:15:02Z"},"version":1}`,
		},
		"null objects are replaced or carried": {
			existing: `{"version":1,"apply":null,"gate":null}`,
			write:    func(l *LockedStore) error { return l.WriteApply(3, Skipped, "") },
			want:     `{"apply":{"module":3,"value":"skipped","config":"","time":"2026-09-26T08:15:02Z"},"gate":null,"version":1}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t)
			if tc.existing != "" {
				writeState(t, s, tc.existing)
			}
			l, err := s.Lock()
			require.NoError(t, err)
			defer func() { _ = l.Unlock() }()

			_, err = l.Read()
			require.NoError(t, err)
			require.NoError(t, tc.write(l))
			require.Equal(t, tc.want, readState(t, s))

			_, err = s.Read()
			require.NoError(t, err)
		})
	}
}

func TestWriteTruncatesLeftoverTempFile(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, os.WriteFile(filepath.Join(s.dir, tempFileName), make([]byte, 4096), 0o600))

	l, err := s.Lock()
	require.NoError(t, err)
	defer func() { _ = l.Unlock() }()
	_, err = l.Read()
	require.NoError(t, err)
	require.NoError(t, l.WriteGatePass(7))

	got, err := s.Read()
	require.NoError(t, err)
	require.True(t, got.Passed(7))
	require.NoFileExists(t, filepath.Join(s.dir, tempFileName))
}

func TestWriteAfterUnlockFails(t *testing.T) {
	s := newTestStore(t)
	l, err := s.Lock()
	require.NoError(t, err)
	_, err = l.Read()
	require.NoError(t, err)
	require.NoError(t, l.Unlock())

	require.Error(t, l.WriteGatePass(7))
	require.NoFileExists(t, filepath.Join(s.dir, StateFileName))
}

func TestTryLockBusy(t *testing.T) {
	s := newTestStore(t)
	held, err := s.Lock()
	require.NoError(t, err)

	_, err = s.TryLock()
	require.ErrorIs(t, err, ErrLockBusy)

	require.NoError(t, held.Unlock())
	l, err := s.TryLock()
	require.NoError(t, err)
	require.NoError(t, l.Unlock())
	require.FileExists(t, filepath.Join(s.dir, lockFileName))
}

func TestLockBlocksUntilUnlock(t *testing.T) {
	s := newTestStore(t)
	held, err := s.TryLock()
	require.NoError(t, err)

	type lockResult struct {
		l   *LockedStore
		err error
	}
	acquired := make(chan lockResult, 1)
	go func() {
		l, err := s.Lock()
		acquired <- lockResult{l, err}
	}()

	select {
	case r := <-acquired:
		require.NoError(t, r.err)
		t.Fatal("Lock returned while the lock was held")
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, held.Unlock())
	select {
	case r := <-acquired:
		require.NoError(t, r.err)
		require.NoError(t, r.l.Unlock())
	case <-time.After(5 * time.Second):
		t.Fatal("Lock didn't return after Unlock")
	}
}

func TestWriteNeedsAReadInThisHold(t *testing.T) {
	s := newTestStore(t)
	l, err := s.Lock()
	require.NoError(t, err)
	defer func() { _ = l.Unlock() }()

	require.Error(t, l.WriteGatePass(7))
	require.NoFileExists(t, filepath.Join(s.dir, StateFileName))
}

func TestWriteAfterAFailedReadFails(t *testing.T) {
	s := newTestStore(t)
	writeState(t, s, `{"version":1}`)
	l, err := s.Lock()
	require.NoError(t, err)
	defer func() { _ = l.Unlock() }()

	_, err = l.Read()
	require.NoError(t, err)
	writeState(t, s, `{"version":2}`)
	_, err = l.Read()
	require.ErrorIs(t, err, ErrParse)
	require.Error(t, l.WriteGatePass(7))
	require.Equal(t, `{"version":2}`, readState(t, s))
}

func TestWriteNeverReplacesAFileWithAnUnparsableOne(t *testing.T) {
	cases := map[string]string{
		"duplicate version key":   `{"version":1,"version":null}`,
		"case-folded version key": `{"version":2,"Version":1}`,
	}
	for name, existing := range cases {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t)
			writeState(t, s, existing)
			l, err := s.Lock()
			require.NoError(t, err)
			defer func() { _ = l.Unlock() }()

			_, err = l.Read()
			require.NoError(t, err)
			require.Error(t, l.WriteGatePass(7))
			require.Equal(t, existing, readState(t, s))
		})
	}
}

func TestWritesInOneHoldBuildOnEachOther(t *testing.T) {
	s := newTestStore(t)
	l, err := s.Lock()
	require.NoError(t, err)
	defer func() { _ = l.Unlock() }()
	_, err = l.Read()
	require.NoError(t, err)

	require.NoError(t, l.WriteGatePass(3))
	require.NoError(t, l.WriteApply(3, Applied, "sha256:aa"))

	got, err := s.Read()
	require.NoError(t, err)
	require.True(t, got.Passed(3))
	require.True(t, got.AppliedAt(3))
}

func TestWriteApplyOnlyWritesDecisions(t *testing.T) {
	cases := map[string]Decision{
		"pending":       "pending",
		"missing value": "",
		"unknown value": "future",
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t)
			l, err := s.Lock()
			require.NoError(t, err)
			defer func() { _ = l.Unlock() }()
			_, err = l.Read()
			require.NoError(t, err)

			require.Error(t, l.WriteApply(3, v, ""))
			require.NoFileExists(t, filepath.Join(s.dir, StateFileName))
		})
	}
}

func TestCreationUsesOneTimestamp(t *testing.T) {
	s := New(t.TempDir())
	tick := testNow
	s.now = func() time.Time {
		tick = tick.Add(time.Second)
		return tick
	}
	l, err := s.Lock()
	require.NoError(t, err)
	defer func() { _ = l.Unlock() }()
	_, err = l.Read()
	require.NoError(t, err)
	require.NoError(t, l.WriteGatePass(7))

	got, err := s.Read()
	require.NoError(t, err)
	require.Equal(t, got.Created, got.Gate.Time)
}
