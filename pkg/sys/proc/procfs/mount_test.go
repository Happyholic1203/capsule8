// Copyright 2018 Capsule8, Inc.
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
	"testing"

	"github.com/Happyholic1203/capsule8/pkg/sys/proc"

	"github.com/stretchr/testify/assert"
)

func TestParseMount(t *testing.T) {
	var err error

	_, err = parseMount("zero 1 2:3 4 5")
	assert.Error(t, err)

	_, err = parseMount("0 one 2:3 4 5")
	assert.Error(t, err)

	_, err = parseMount("0 1 two:3 4 5")
	assert.Error(t, err)

	_, err = parseMount("0 1 2:three 4 5")
	assert.Error(t, err)
}

func TestMounts(t *testing.T) {
	fs, err := NewFileSystem("testdata/proc")
	assert.NoError(t, err)

	expectedMounts := []proc.Mount{
		proc.Mount{
			MountID:        uint(260),
			ParentID:       uint(89),
			Major:          uint(253),
			Minor:          uint(6),
			Root:           "/containers/ab63f6eab155cf698a20e7d5e03298dd8baf442ff6e4e20e2dff320f3e2fee3e/mounts",
			MountPoint:     "/var/lib/docker/containers/ab63f6eab155cf698a20e7d5e03298dd8baf442ff6e4e20e2dff320f3e2fee3e/mounts",
			MountOptions:   []string{"rw", "relatime"},
			OptionalFields: map[string]string{"unbindable": ""},
			FilesystemType: "xfs",
			MountSource:    "/dev/mapper/rootvg-docker_lv",
			SuperOptions: map[string]string{
				"attr2":    "",
				"inode64":  "",
				"noquota":  "",
				"rw":       "",
				"seclabel": "",
			},
		},
		proc.Mount{
			MountID:        uint(287),
			ParentID:       uint(26),
			Major:          uint(0),
			Minor:          uint(47),
			Root:           "/",
			MountPoint:     "/run/user/42",
			MountOptions:   []string{"rw", "nosuid", "nodev", "relatime"},
			OptionalFields: map[string]string{"shared": "226"},
			FilesystemType: "tmpfs",
			MountSource:    "tmpfs",
			SuperOptions: map[string]string{
				"gid":      "42",
				"mode":     "700",
				"rw":       "",
				"seclabel": "",
				"size":     "1632548k",
				"uid":      "42",
			},
		},
		proc.Mount{
			MountID:        uint(43),
			ParentID:       uint(19),
			Major:          uint(0),
			Minor:          uint(7),
			Root:           "/",
			MountPoint:     "/sys/kernel/debug",
			MountOptions:   []string{"rw", "relatime"},
			OptionalFields: map[string]string{"shared": "25"},
			FilesystemType: "debugfs",
			MountSource:    "debugfs",
			SuperOptions: map[string]string{
				"rw": "",
			},
		},
		proc.Mount{
			MountID:        uint(138),
			ParentID:       uint(43),
			Major:          uint(0),
			Minor:          uint(9),
			Root:           "/",
			MountPoint:     "/sys/kernel/debug/tracing",
			MountOptions:   []string{"rw", "relatime"},
			OptionalFields: map[string]string{"shared": "118"},
			FilesystemType: "tracefs",
			MountSource:    "tracefs",
			SuperOptions: map[string]string{
				"rw": "",
			},
		},
	}

	actualMounts := fs.Mounts()
	assert.Equal(t, expectedMounts, actualMounts)
}

func TestHostFileSystem(t *testing.T) {
	nocgroupsProcFS, err := NewFileSystem("testdata/nocgroups")
	assert.NoError(t, err)

	fs := nocgroupsProcFS.HostFileSystem()
	assert.Equal(t, nocgroupsProcFS, fs)

	nohostProcFS, err := NewFileSystem("testdata/nohost")
	assert.NoError(t, err)

	var expected proc.FileSystem
	fs = nohostProcFS.HostFileSystem()
	assert.Equal(t, expected, fs)

	hostProcFS, err := NewFileSystem("testdata/host")
	assert.NoError(t, err)

	fs = hostProcFS.HostFileSystem()
	assert.Equal(t, "testdata/host", hostProcFS.MountPoint)

	pfs, err := NewFileSystem("testdata/proc")
	assert.NoError(t, err)

	fs = pfs.HostFileSystem()
	assert.Equal(t, pfs, fs)
}

func TestPerfEventDir(t *testing.T) {
	pfs, err := NewFileSystem("testdata/proc")
	assert.NoError(t, err)
	assert.Equal(t, "", pfs.PerfEventDir())

	hostProcFS, err := NewFileSystem("testdata/nohost")
	assert.NoError(t, err)
	assert.Equal(t, "/sys/fs/cgroup/perf_event", hostProcFS.PerfEventDir())
}

func TestTracingDir(t *testing.T) {
	pfs, err := NewFileSystem("testdata/proc")
	assert.NoError(t, err)
	assert.Equal(t, "/sys/kernel/debug/tracing", pfs.TracingDir())

	hostProcFS, err := NewFileSystem("testdata/nohost")
	assert.NoError(t, err)
	assert.Equal(t, "", hostProcFS.TracingDir())

	debugfsProcFS, err := NewFileSystem("testdata/debugfs")
	assert.NoError(t, err)
	assert.Equal(t, "testdata/debugfs/mnt/debugfs/tracing", debugfsProcFS.TracingDir())
}

func TestSupportedFilesystems(t *testing.T) {
	pfs, err := NewFileSystem("testdata/proc")
	assert.NoError(t, err)

	expected := []string{
		"sysfs", "proc", "cgroup", "debugfs", "iso9660", "ext4", "autofs",
	}
	actual := pfs.SupportedFilesystems()
	assert.Equal(t, expected, actual)
}
