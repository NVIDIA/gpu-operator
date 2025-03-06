// Package conffile wraps the read and write of configuration files
package conffile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type File struct {
	perms    int
	fullname string
}

type Opt func(*File)

// New returns a new File
func New(opts ...Opt) *File {
	f := File{perms: 0600}
	for _, fn := range opts {
		fn(&f)
	}
	if f.fullname == "" {
		return nil
	}
	return &f
}

// WithDirName determines the filename from a subdirectory in the user's HOME
// e.g. dir=".app", name="config.json", sets the fullname to "$HOME/.app/config.json"
func WithDirName(dir, name string) Opt {
	return func(f *File) {
		f.fullname = filepath.Join(homedir(), dir, name)
	}
}

// WithEnvFile sets the fullname to the environment value if defined
func WithEnvFile(envVar string) Opt {
	return func(f *File) {
		val := os.Getenv(envVar)
		if val != "" {
			f.fullname = val
		}
	}
}

// WithEnvDir sets the fullname to the environment value + filename if the environment variable is defined
func WithEnvDir(envVar, name string) Opt {
	return func(f *File) {
		val := os.Getenv(envVar)
		if val != "" {
			f.fullname = filepath.Join(val, name)
		}
	}
}

// WithFullname specifies the filename
func WithFullname(fullname string) Opt {
	return func(f *File) {
		f.fullname = fullname
	}
}

// WithPerms specifies the permissions to create a file with (default 0600)
func WithPerms(perms int) Opt {
	return func(f *File) {
		f.perms = perms
	}
}

func (f *File) Name() string {
	return f.fullname
}

func (f *File) Open() (io.ReadCloser, error) {
	return os.Open(f.fullname)
}

func (f *File) Write(rdr io.Reader) error {
	// create temp file/open
	dir := filepath.Dir(f.fullname)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(f.fullname))
	if err != nil {
		return err
	}
	tmpStat, err := tmp.Stat()
	if err != nil {
		return err
	}
	tmpName := tmpStat.Name()
	tmpFullname := filepath.Join(dir, tmpName)
	defer os.Remove(tmpFullname)

	// copy from rdr to temp file
	_, err = io.Copy(tmp, rdr)
	errC := tmp.Close()
	if err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	if errC != nil {
		return fmt.Errorf("failed to close config: %w", errC)
	}

	// adjust file ownership/permissions
	mode := os.FileMode(0600)
	uid := os.Getuid()
	gid := os.Getgid()
	// adjust defaults based on existing file if available
	stat, err := os.Stat(f.fullname)
	if err == nil {
		// adjust mode to existing file
		if stat.Mode().IsRegular() {
			mode = stat.Mode()
		}
		uid, gid, _ = getFileOwner(stat)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	// update mode and owner of temp file
	if err := os.Chmod(tmpFullname, mode); err != nil {
		return err
	}
	if uid > 0 && gid > 0 {
		_ = os.Chown(tmpFullname, uid, gid)
	}
	// move temp file to target filename
	return os.Rename(tmpFullname, f.fullname)
}
