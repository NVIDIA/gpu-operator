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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	// DefaultDir is the host tmpfs directory that holds the state, lock and temp files.
	DefaultDir = "/run/nvidia/validations"

	// The names start with a dot so that the validators' preStop cleanup
	// globs leave them in place.
	StateFileName = ".gpu-config-state"
	lockFileName  = ".gpu-config.lock"
	tempFileName  = ".gpu-config-state.tmp"
)

// ErrLockBusy is returned by TryLock when another process holds the lock.
var ErrLockBusy = errors.New("state file lock is busy")

// Store gives access to the state, lock and temp files in one directory.
type Store struct {
	dir string
	now func() time.Time
}

// New returns a Store for the state, lock and temp files in dir.
func New(dir string) *Store {
	return &Store{dir: dir, now: time.Now}
}

// Read reads the state file without the lock. A missing file reads as one with
// neither object; a file that doesn't parse returns an error wrapping ErrParse.
func (s *Store) Read() (*File, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, StateFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return &File{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}
	return parse(data)
}

// Lock takes the lock, blocking until it is free.
func (s *Store) Lock() (*LockedStore, error) {
	return s.lock(syscall.LOCK_EX)
}

// TryLock takes the lock without blocking, returning ErrLockBusy when it is
// held.
func (s *Store) TryLock() (*LockedStore, error) {
	return s.lock(syscall.LOCK_EX | syscall.LOCK_NB)
}

func (s *Store) lock(how int) (*LockedStore, error) {
	f, err := os.OpenFile(filepath.Join(s.dir, lockFileName), os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}
	for {
		err = syscall.Flock(int(f.Fd()), how)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLockBusy
		}
		return nil, fmt.Errorf("error obtaining lock %s: %w", lockFileName, err)
	}
	return &LockedStore{store: s, f: f}, nil
}

// LockedStore is a Store whose lock is held. Every write goes through it.
type LockedStore struct {
	store *Store
	f     *os.File
	// base is the file as read in this hold, the only base a write may use.
	base *File
}

// Unlock releases the lock and closes the lock file. Closing alone would
// release it too, but only once no other reference to the file remains.
func (l *LockedStore) Unlock() error {
	if l.f == nil {
		return nil
	}
	err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	if cerr := l.f.Close(); err == nil {
		err = cerr
	}
	l.f = nil
	l.base = nil
	return err
}

// Read reads the state file under the lock. Writes in this hold build on the
// result of the latest Read; after a failed Read they refuse to write.
func (l *LockedStore) Read() (*File, error) {
	f, err := l.store.Read()
	l.base = f
	return f, err
}

// WriteApply records an applied or skipped decision at module, keeping every
// other top-level key.
func (l *LockedStore) WriteApply(module uint64, value Decision, config string) error {
	if value != Applied && value != Skipped {
		return fmt.Errorf("apply.value must be %s or %s, got %q", Applied, Skipped, value)
	}
	return l.write("apply", func(now time.Time) any {
		return Apply{Module: module, Value: value, Config: config, Time: now}
	})
}

// WriteGatePass records a gate pass at module, which must be the load ID the
// gate read in this hold, or 0.
func (l *LockedStore) WriteGatePass(module uint64) error {
	return l.write("gate", func(now time.Time) any {
		return Gate{Module: module, Time: now}
	})
}

func (l *LockedStore) write(key string, build func(now time.Time) any) error {
	if l.f == nil {
		return errors.New("state file write without the lock")
	}
	if l.base == nil {
		return errors.New("state file write with no successful read in this lock hold")
	}
	now := l.store.timestamp()
	content, err := encode(l.base, key, build(now), now)
	if err != nil {
		return err
	}
	// A base that parsed can still encode to content that doesn't, such as one
	// with a duplicate or case-folded version key; never put that on disk.
	written, err := parse(content)
	if err != nil {
		return fmt.Errorf("refusing to write an unparsable state file: %w", err)
	}

	tmp := filepath.Join(l.store.dir, tempFileName)
	// Only the lock holder writes the temp file, so truncating a leftover from
	// a killed writer is safe.
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("opening temp state file: %w", err)
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing temp state file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing temp state file: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(l.store.dir, StateFileName)); err != nil {
		return fmt.Errorf("renaming temp state file: %w", err)
	}
	l.base = written
	return nil
}

// encode builds the new file content: base's top-level keys with key replaced
// by obj, or a new file when base doesn't exist.
func encode(base *File, key string, obj any, now time.Time) ([]byte, error) {
	out := make(map[string]json.RawMessage, len(base.raw)+1)
	if base.Exists() {
		maps.Copy(out, base.raw)
	} else {
		out["version"] = json.RawMessage(fmt.Sprint(stateVersion))
		created, err := json.Marshal(now)
		if err != nil {
			return nil, fmt.Errorf("encoding created time: %w", err)
		}
		out["created"] = created
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encoding %s object: %w", key, err)
	}
	out[key] = b
	content, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encoding state file: %w", err)
	}
	return content, nil
}

func (s *Store) timestamp() time.Time {
	return s.now().UTC().Truncate(time.Second)
}
