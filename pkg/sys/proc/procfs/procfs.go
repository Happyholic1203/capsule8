// Copyright 2017 Capsule8, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package procfs

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Happyholic1203/capsule8/pkg/sys/proc"

	"github.com/golang/glog"
)

var (
	// Default procfs mounted on /proc
	procFSOnce sync.Once
	procFS     *FileSystem
)

// FileSystem represents data accessible through the proc pseudo-filesystem.
type FileSystem struct {
	MountPoint string

	bootID     string
	bootIDOnce sync.Once

	maxPID     int
	maxPIDOnce sync.Once

	numCPU     int
	numCPUOnce sync.Once

	hostProcFS     proc.FileSystem
	hostProcFSOnce sync.Once

	selfTGID int
	selfOnce sync.Once
}

// NewFileSystem returns a new concrete procfs FileSystem instance attached to
// the specified mount point. If the mount point is unspecified, the shared
// instance for the default /proc mount point is returned.
func NewFileSystem(mountPoint string) (*FileSystem, error) {
	if mountPoint == "" {
		procFSOnce.Do(func() {
			var err error
			if procFS, err = NewFileSystem("/proc"); err != nil {
				glog.Fatal(err)
			}

			filename := filepath.Join(procFS.MountPoint, "self")
			self, err := os.Readlink(filename)
			if err != nil {
				glog.Fatalf("Cannot read %q: %v", filename, err)
			}

			_, file := filepath.Split(self)
			pid, err := strconv.Atoi(file)
			if err != nil {
				glog.Fatalf("Cannot parse %q as PID: %v",
					file, err)
			}
			if pid != os.Getpid() {
				glog.Fatalf("%q points to wrong pid: %d (expected %d)",
					filename, pid, os.Getpid())
			}
		})
		return procFS, nil
	}

	fi, err := os.Stat(mountPoint)
	if err != nil {
		return nil, fmt.Errorf("Cannot stat mount point %q: %v",
			mountPoint, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("Mount point %q is not a directory",
			mountPoint)
	}

	return &FileSystem{
		MountPoint: mountPoint,
	}, nil
}

// ReadFile returns the contents of the procfs file indicated by the
// given relative path.
func (fs *FileSystem) ReadFile(relativePath string) ([]byte, error) {
	filename := filepath.Join(fs.MountPoint, relativePath)
	return ioutil.ReadFile(filename)
}

// ReadFileOrPanic returns the contents of the procfs file indicated by the
// given relative path. If the file cannot be read, glog.Fatal will be called.
func (fs *FileSystem) ReadFileOrPanic(relativePath string) []byte {
	filename := filepath.Join(fs.MountPoint, relativePath)
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		glog.Fatalf("Cannot read %q: %v", filename, err)
	}
	return data
}

// BootID returns the kernel's boot identifier retrieved from the file
// sys/kernel/random/boot_id.
func (fs *FileSystem) BootID() string {
	fs.bootIDOnce.Do(func() {
		data := fs.ReadFileOrPanic("sys/kernel/random/boot_id")
		fs.bootID = strings.TrimSpace(string(data))
	})

	return fs.bootID
}

// MaxPID returns the maximum PID that the kernel will use.
func (fs *FileSystem) MaxPID() uint {
	fs.maxPIDOnce.Do(func() {
		data := fs.ReadFileOrPanic("sys/kernel/pid_max")
		value, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			glog.Fatalf("Cannot parse /proc/sys/kernel/pid_max: %s", err)
		}
		fs.maxPID = value
	})

	return uint(fs.maxPID)
}

// NumCPU returns the number of CPUs on the system. This differs from
// runtime.NumCPU in that runtime.NumCPU returns the number of logical CPUs
// available to the calling process.
func (fs *FileSystem) NumCPU() int {
	fs.numCPUOnce.Do(func() {
		data := fs.ReadFileOrPanic("cpuinfo")
		ncpu := 0
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			x := strings.Split(scanner.Text(), ":")
			if len(x) == 2 && strings.TrimSpace(x[0]) == "processor" {
				ncpu++
			}
		}
		if err := scanner.Err(); err != nil {
			glog.Fatalf("Could not parse cpuinfo data: %v", err)
		}
		fs.numCPU = ncpu
	})

	return fs.numCPU
}
