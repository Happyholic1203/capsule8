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

package sensor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"
	"github.com/Happyholic1203/capsule8/pkg/sys/proc/procfs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var kprobeFormats = map[string]string{
	"dofork": `name: sensor_^^PID^^_dofork
ID: 1616
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:u64 clone_flags;	offset:16;	size:8;	signed:0;

print fmt: "clone_flags=%d", REC->clone_flags`,
	"_dofork": `name: sensor_^^PID^^__dofork
ID: 1617
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:u64 clone_flags;	offset:16;	size:8;	signed:0;

print fmt: "clone_flags=%d", REC->clone_flags`,
	"doexit": `name: sensor_^^PID^^_doexit
ID: 1618
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:s64 code;	offset:16;	size:8;	signed:1;

print fmt: "(%lx) code=%Ld", REC->__probe_ip, REC->code`,
	"creds": `name: sensor_^^PID^^_creds
ID: 1619
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:u64 usage;	offset:16;	size:8;	signed:0;
	field:u32 uid;	offset:24;	size:4;	signed:0;
	field:u32 gid;	offset:28;	size:4;	signed:0;
	field:u32 suid;	offset:32;	size:4;	signed:0;
	field:u32 sgid;	offset:36;	size:4;	signed:0;
	field:u32 euid;	offset:40;	size:4;	signed:0;
	field:u32 egid;	offset:44;	size:4;	signed:0;
	field:u32 fsuid;	offset:48;	size:4;	signed:0;
	field:u32 fsgid;	offset:52;	size:4;	signed:0;

print fmt: "(%lx) usage=%Lu uid=%u gid=%u suid=%u sgid=%u euid=%u egid=%u fsuid=%u fsgid=%u", REC->__probe_ip, REC->usage, REC->uid, REC->gid, REC->suid, REC->sgid, REC->euid, REC->egid, REC->fsuid, REC->fsgid`,
	"setfspwd": `name: sensor_^^PID^^_setfspwd
ID: 1620
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_func;	offset:8;	size:8;	signed:0;
	field:unsigned long __probe_ret_ip;	offset:16;	size:8;	signed:0;

print fmt: "(%lx <- %lx)", REC->__probe_func, REC->__probe_ret_ip`,
	"execve1": `name: sensor_^^PID^^_execve1
ID: 1621
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:__data_loc char[] filename;	offset:16;	size:4;	signed:1;
	field:__data_loc char[] argv0;	offset:20;	size:4;	signed:1;
	field:__data_loc char[] argv1;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] argv2;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] argv3;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] argv4;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] argv5;	offset:40;	size:4;	signed:1;

print fmt: "(%lx) filename=\"%s\" argv0=\"%s\" argv1=\"%s\" argv2=\"%s\" argv3=\"%s\" argv4=\"%s\" argv5=\"%s\"", REC->__probe_ip, __get_str(filename), __get_str(argv0), __get_str(argv1), __get_str(argv2), __get_str(argv3), __get_str(argv4), __get_str(argv5)`,
	"execve2": `name: sensor_^^PID^^_execve2
ID: 1622
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:__data_loc char[] filename;	offset:16;	size:4;	signed:1;
	field:__data_loc char[] argv0;	offset:20;	size:4;	signed:1;
	field:__data_loc char[] argv1;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] argv2;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] argv3;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] argv4;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] argv5;	offset:40;	size:4;	signed:1;

print fmt: "(%lx) filename=\"%s\" argv0=\"%s\" argv1=\"%s\" argv2=\"%s\" argv3=\"%s\" argv4=\"%s\" argv5=\"%s\"", REC->__probe_ip, __get_str(filename), __get_str(argv0), __get_str(argv1), __get_str(argv2), __get_str(argv3), __get_str(argv4), __get_str(argv5)`,
	"execve3": `name: sensor_^^PID^^_execve3
ID: 1623
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:__data_loc char[] filename;	offset:16;	size:4;	signed:1;
	field:__data_loc char[] argv0;	offset:20;	size:4;	signed:1;
	field:__data_loc char[] argv1;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] argv2;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] argv3;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] argv4;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] argv5;	offset:40;	size:4;	signed:1;

print fmt: "(%lx) filename=\"%s\" argv0=\"%s\" argv1=\"%s\" argv2=\"%s\" argv3=\"%s\" argv4=\"%s\" argv5=\"%s\"", REC->__probe_ip, __get_str(filename), __get_str(argv0), __get_str(argv1), __get_str(argv2), __get_str(argv3), __get_str(argv4), __get_str(argv5)`,
	"cgroups1": `name: sensor_^^PID^^_cgroups1
ID: 1625
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:__data_loc char[] container_id;	offset:16;	size:4;	signed:1;
	field:__data_loc char[] buf;	offset:20;	size:4;	signed:1;
	field:s32 threadgroup;	offset:24;	size:4;	signed:1;

print fmt: "(%lx) container_id=\"%s\" buf=\"%s\" threadgroup=%d", REC->__probe_ip, __get_str(container_id), __get_str(buf), REC->threadgroup`,
	"cgroups2": `name: sensor_^^PID^^_cgroups2
ID: 1625
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:__data_loc char[] container_id;	offset:16;	size:4;	signed:1;
	field:__data_loc char[] buf;	offset:20;	size:4;	signed:1;
	field:s32 threadgroup;	offset:24;	size:4;	signed:1;

print fmt: "(%lx) container_id=\"%s\" buf=\"%s\" threadgroup=%d", REC->__probe_ip, __get_str(container_id), __get_str(buf), REC->threadgroup`,
	"docker1": `name: sensor_^^PID^^_docker1
ID: 1626
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:__data_loc char[] newname;	offset:16;	size:4;	signed:1;

print fmt: "(%lx) newname=\"%s\"", REC->__probe_ip, __get_str(newname)`,
	"docker2": `name: sensor_^^PID^^_docker2
ID: 1627
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:__data_loc char[] pathname;	offset:16;	size:4;	signed:1;

print fmt: "(%lx) pathname=\"%s\"", REC->__probe_ip, __get_str(pathname)`,
	"wake_up_new_task": `name: sensor_^^PI^^_wake_up_new_task
ID: 1628
format:
field:unsigned short common_type;	offset:0;	size:2;	signed:0;
field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
field:int common_pid;	offset:4;	size:4;	signed:1;

	field:s32 pid;	offset:8;	size:4;	signed:1;
	field:s64 start_time_sec;	offset:12;	size:8;	signed:1;
	field:s64 start_time_nsec;	offset:20;	size:8;	signed:1;

print fmt: "(%lx) pid=%d start_time_sec=%d start_time_nsec=%d", REC->__probe_ip, REC->pid, REC->start_time_sec, REC->start_time_nsec)`,
}

func writeFile(t *testing.T, filename string, data []byte) {
	err := os.MkdirAll(filepath.Dir(filename), 0777)
	require.NoError(t, err)

	err = ioutil.WriteFile(filename, data, 0666)
	require.NoError(t, err)
}

var nextProbeID uint64 = 8800

func newUnitTestKprobe(t *testing.T, sensor *Sensor, delta uint64, format string) {
	require.True(t, strings.HasPrefix(format, "name: ^^NAME^^"))

	nextProbeName := sensor.Monitor().NextProbeName(delta)
	probeNameParts := strings.Split(nextProbeName, "/")
	nextProbeID++

	name := probeNameParts[1]
	format = strings.Replace(format, "^^NAME^^", name, -1)
	format = strings.Replace(format, "^^ID^^", fmt.Sprintf("%d", nextProbeID), -1)
	filename := filepath.Join(sensor.tracingDir, "events", probeNameParts[0],
		probeNameParts[1], "format")

	writeFile(t, filename, ([]byte)(format))
}

func newUnitTestUprobe(t *testing.T, sensor *Sensor, delta uint64, format string) {
	require.True(t, strings.HasPrefix(format, "name: ^^NAME^^"))

	nextProbeName := sensor.Monitor().NextProbeName(delta)
	probeNameParts := strings.Split(nextProbeName, "/")
	nextProbeID++

	name := probeNameParts[1]
	format = strings.Replace(format, "^^NAME^^", name, -1)
	format = strings.Replace(format, "^^ID^^", fmt.Sprintf("%d", nextProbeID), -1)
	filename := filepath.Join(sensor.tracingDir, "events", probeNameParts[0],
		probeNameParts[1], "format")

	writeFile(t, filename, ([]byte)(format))
}

func recursiveCopy(t *testing.T, sourceDir, targetDir string) {
	err := filepath.Walk(sourceDir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			sourceFilename := path
			targetFilename := filepath.Join(targetDir, path[len(sourceDir):])
			if info.IsDir() {
				err = os.MkdirAll(targetFilename, 0777)
			} else {
				var data []byte
				data, err = ioutil.ReadFile(sourceFilename)
				if err == nil {
					writeFile(t, targetFilename, data)
				}
			}
			return err
		})
	require.NoError(t, err)
}

func newUnstartedUnitTestSensor(t *testing.T) *Sensor {
	procFS, err := procfs.NewFileSystem("testdata")
	require.NoError(t, err)

	runtimeDir, err := ioutil.TempDir("", "capsule8_")
	require.NoError(t, err)

	defer func() {
		if err != nil {
			os.RemoveAll(runtimeDir)
		}
	}()

	dockerDir := filepath.Join(runtimeDir, "docker")
	err = os.MkdirAll(dockerDir, 0777)
	require.NoError(t, err)

	tracingDir := filepath.Join(runtimeDir, "tracing")
	err = os.MkdirAll(tracingDir, 0777)
	require.NoError(t, err)

	kprobeEvents := filepath.Join(tracingDir, "kprobe_events")
	writeFile(t, kprobeEvents, []byte{})

	uprobeEvents := filepath.Join(tracingDir, "uprobe_events")
	writeFile(t, uprobeEvents, []byte{})

	sourceDir := filepath.Join("testdata", "events")
	targetDir := filepath.Join(tracingDir, "events")
	recursiveCopy(t, sourceDir, targetDir)

	sourceDir = filepath.Join("testdata", "docker")
	recursiveCopy(t, sourceDir, dockerDir)

	pidString := fmt.Sprintf("%d", os.Getpid())
	for k, format := range kprobeFormats {
		format = strings.Replace(format, "^^PID^^", pidString, -1)
		filename := filepath.Join(tracingDir, "events", "capsule8",
			fmt.Sprintf("sensor_%d_%s", os.Getpid(), k), "format")
		writeFile(t, filename, ([]byte)(format))
	}

	sensor, err := NewSensor(
		WithRuntimeDir(runtimeDir),
		WithDockerContainerDir(dockerDir),
		WithProcFileSystem(procFS),
		WithEventSourceController(perf.NewStubEventSourceController()),
		WithTracingDir(tracingDir),
		WithCleanupFunc(func() { os.RemoveAll(runtimeDir) }))
	require.NoError(t, err)

	// Set sensorPID to 76989 so that PID 76989 is considered to be the
	// sensor. This is what is in the testdata.
	sensorPID = 76989

	return sensor
}

func newUnitTestSensor(t *testing.T) *Sensor {
	sensor := newUnstartedUnitTestSensor(t)

	err := sensor.Start()
	require.NoError(t, err)

	return sensor
}

// setSampleRawData enumerates the values in expression.FieldValueMap and
// dynamically creates a perf.TraceEventFormat format and encodes the data into
// a sample's RawData field. It currently only supports strings and integer
// field types. For simplicity and because they're not currently needed, arrays
// are not supported.
func setSampleRawData(sample *perf.Sample, data expression.FieldValueMap) {
	// First pass: Figure out static size of buffer
	dataOffset := uint16(0)
	for _, v := range data {
		switch v.(type) {
		case int8, uint8:
			dataOffset++
		case int16, uint16:
			dataOffset += 2
		case int32, uint32, string, []byte, []int8:
			dataOffset += 4
		case int64, uint64:
			dataOffset += 8
		default:
			panic(fmt.Sprintf("unsupported data type: %s", reflect.TypeOf(v)))
		}
	}

	var (
		offset    int
		extraData [][]byte
	)

	format := make(perf.TraceEventFormat)
	rawData := &bytes.Buffer{}
	for k, v := range data {
		field := perf.TraceEventField{
			FieldName: k,
			Offset:    offset,
		}

		switch a := v.(type) {
		case []byte:
			binary.Write(rawData, binary.LittleEndian, dataOffset)
			binary.Write(rawData, binary.LittleEndian, uint16(len(a)))
			dataOffset += uint16(len(a))
			extraData = append(extraData, a)
			field.Size = 4
			field.DataType = expression.ValueTypeUnsignedInt8
			field.DataTypeSize = 1
			field.DataLocSize = 4
		case []int8:
			binary.Write(rawData, binary.LittleEndian, dataOffset)
			binary.Write(rawData, binary.LittleEndian, uint16(len(a)))
			dataOffset += uint16(len(a))
			var slice = struct {
				addr     uintptr
				len, cap int
			}{uintptr(unsafe.Pointer(&a[0])), len(a), len(a)}
			extraData = append(extraData, *(*[]byte)(unsafe.Pointer(&slice)))
			field.Size = 4
			field.IsSigned = true
			field.DataType = expression.ValueTypeSignedInt8
			field.DataTypeSize = 1
			field.DataLocSize = 4
		case string:
			binary.Write(rawData, binary.LittleEndian, dataOffset)
			binary.Write(rawData, binary.LittleEndian, uint16(len(a)))
			dataOffset += uint16(len(a))
			extraData = append(extraData, []byte(a))
			field.Size = 4
			field.DataType = expression.ValueTypeString
			field.DataTypeSize = 1
			field.DataLocSize = 4
		case int8:
			binary.Write(rawData, binary.LittleEndian, a)
			field.Size = 1
			field.IsSigned = true
			field.DataType = expression.ValueTypeSignedInt8
			field.DataTypeSize = 1
		case int16:
			binary.Write(rawData, binary.LittleEndian, a)
			field.Size = 2
			field.IsSigned = true
			field.DataType = expression.ValueTypeSignedInt16
			field.DataTypeSize = 2
		case int32:
			binary.Write(rawData, binary.LittleEndian, a)
			field.Size = 4
			field.IsSigned = true
			field.DataType = expression.ValueTypeSignedInt32
			field.DataTypeSize = 4
		case int64:
			binary.Write(rawData, binary.LittleEndian, a)
			field.Size = 8
			field.IsSigned = true
			field.DataType = expression.ValueTypeSignedInt64
			field.DataTypeSize = 8
		case uint8:
			binary.Write(rawData, binary.LittleEndian, a)
			field.Size = 1
			field.DataType = expression.ValueTypeUnsignedInt8
			field.DataTypeSize = 1
		case uint16:
			binary.Write(rawData, binary.LittleEndian, a)
			field.Size = 2
			field.DataType = expression.ValueTypeUnsignedInt16
			field.DataTypeSize = 2
		case uint32:
			binary.Write(rawData, binary.LittleEndian, a)
			field.Size = 4
			field.DataType = expression.ValueTypeUnsignedInt32
			field.DataTypeSize = 4
		case uint64:
			binary.Write(rawData, binary.LittleEndian, a)
			field.Size = 8
			field.DataType = expression.ValueTypeUnsignedInt64
			field.DataTypeSize = 8
		}
		offset += field.Size
		format[field.FieldName] = field
	}

	for _, b := range extraData {
		rawData.Write(b)
	}

	sample.TraceFormat = format
	sample.RawData = rawData.Bytes()
}

func TestNewSensorOptions(t *testing.T) {
	procFS, err := procfs.NewFileSystem("testdata")
	require.NoError(t, err)

	expOptions := newSensorOptions{
		runtimeDir:            "runtimeDir",
		supportDir:            "supportDir",
		sensorID:              "sensorID",
		perfEventDir:          "perfEventDir",
		tracingDir:            "tracingDir",
		dockerContainerDir:    "dockerContainerDir",
		ociContainerDir:       "ociContainerDir",
		procFS:                procFS,
		eventSourceController: perf.NewStubEventSourceController(),
		cgroupNames:           []string{"abc", "def", "ghi"},
		ringBufferNumPages:    298736,
	}

	options := []NewSensorOption{
		WithRuntimeDir(expOptions.runtimeDir),
		WithSupportDir(expOptions.supportDir),
		WithSensorID(expOptions.sensorID),
		WithDockerContainerDir(expOptions.dockerContainerDir),
		WithOciContainerDir(expOptions.ociContainerDir),
		WithProcFileSystem(expOptions.procFS),
		WithEventSourceController(expOptions.eventSourceController),
		WithPerfEventDir(expOptions.perfEventDir),
		WithTracingDir(expOptions.tracingDir),
		WithRingBufferNumPages(expOptions.ringBufferNumPages),
	}
	for _, n := range expOptions.cgroupNames {
		options = append(options, WithCgroupName(n))
	}

	actOptions := newSensorOptions{}
	for _, option := range options {
		option(&actOptions)
	}

	assert.Equal(t, expOptions, actOptions)
}

func TestBuildMonitorGroups(t *testing.T) {
	sensor := Sensor{perfEventDir: "perfEventDir"}
	cgroupList, pidList, err := sensor.buildMonitorGroups()
	assert.Zero(t, cgroupList)
	assert.Equal(t, []int{-1}, pidList)
	assert.NoError(t, err)

	sensor.cgroupNames = []string{"/"}
	cgroupList, pidList, err = sensor.buildMonitorGroups()
	assert.Zero(t, cgroupList)
	assert.Equal(t, []int{-1}, pidList)
	assert.NoError(t, err)

	sensor.cgroupNames = []string{"foo", "foo", "bar"}
	cgroupList, pidList, err = sensor.buildMonitorGroups()
	assert.Equal(t, []string{"foo", "bar"}, cgroupList)
	assert.Zero(t, pidList)
	assert.NoError(t, err)
}

func TestDetermineTracingMountInfo(t *testing.T) {
	sensor := Sensor{runtimeDir: "/var/run/capsule8"}

	var (
		err                          error
		filesystems                  []string
		fsType, mountDir, tracingDir string
	)

	filesystems = []string{"tracefs"}
	fsType, mountDir, tracingDir, err = sensor.determineTracingMountInfo(filesystems)
	assert.NoError(t, err)
	assert.Equal(t, "tracefs", fsType)
	assert.Equal(t, "/var/run/capsule8/tracing", mountDir)
	assert.Equal(t, "/var/run/capsule8/tracing", tracingDir)

	filesystems = []string{"debugfs"}
	fsType, mountDir, tracingDir, err = sensor.determineTracingMountInfo(filesystems)
	assert.NoError(t, err)
	assert.Equal(t, "debugfs", fsType)
	assert.Equal(t, "/var/run/capsule8/debug", mountDir)
	assert.Equal(t, "/var/run/capsule8/debug/tracing", tracingDir)

	// When both debugfs and tracefs are available, tracefs should win
	filesystems = []string{"debugfs", "tracefs"}
	fsType, mountDir, tracingDir, err = sensor.determineTracingMountInfo(filesystems)
	assert.NoError(t, err)
	assert.Equal(t, "tracefs", fsType)
	assert.Equal(t, "/var/run/capsule8/tracing", mountDir)
	assert.Equal(t, "/var/run/capsule8/tracing", tracingDir)

	filesystems = []string{"ext4"}
	fsType, mountDir, tracingDir, err = sensor.determineTracingMountInfo(filesystems)
	assert.Error(t, err)
}

func TestActualKernelSymbol(t *testing.T) {
	s := Sensor{}
	s.kallsyms = map[string]string{
		"create_dev":           "create_dev.constprop.6",
		"__x64_sys_setuid":     "__x64_sys_setuid",
		"__cgroup_procs_write": "__cgroup_procs_write",
	}

	tests := map[string]string{
		"__cgroup_procs_write": "__cgroup_procs_write",
		"create_dev":           "create_dev.constprop.6",
		"sys_setuid":           "__x64_sys_setuid",
	}
	for sym, exp := range tests {
		got, err := s.ActualKernelSymbol(sym)
		if assert.NoError(t, err) {
			assert.Equal(t, exp, got)
		}
	}
}

func TestRewriteSyscallFetchargs(t *testing.T) {
	args := map[string]string{
		"a=+0(%di):string": "a=+0(+0x70(%di)):string",
		"b=%si:s32":        "b=+0x68(%di):s32",
		"c=%dx:u64":        "c=+0x60(%di):u64",
		"d=%cx:u16":        "d=+0x38(%di):u16",
		"e=%r8:s8":         "e=+0x48(%di):s8",
		"f=+0(%r9):string": "f=+0(+0x40(%di)):string",
		"g=%ax:s32":        "g=+0x50(%di):s32",
	}

	var inputArray, expArray []string
	for k, v := range args {
		inputArray = append(inputArray, k)
		expArray = append(expArray, v)
	}
	input := strings.Join(inputArray, " ")
	exp := strings.Join(expArray, " ")
	got := rewriteSyscallFetchargs(input)
	assert.Equal(t, exp, got)
}

func TestLookupTaskStructFields(t *testing.T) {
	sensor := newUnstartedUnitTestSensor(t)
	sensor.supportDir = filepath.Join("testdata", "support")

	goodDataFile := filepath.Join(sensor.supportDir, "good_task_struct.json")
	badDataFile := filepath.Join(sensor.supportDir, "bad_task_struct.json")
	dneDataFile := filepath.Join(sensor.supportDir, "dne_task_struct.json") // does not exist
	goodRelease := "2.6.32-696.el6.x86_64"
	badRelease := "2.6.32-754.el6.x86_64"

	// Test that we get a true result for a known release
	fieldData, ok := sensor.lookupTaskStructFields(goodDataFile, goodRelease)
	assert.True(t, ok)
	assert.NotZero(t, fieldData)

	// Test that a non-existant data file returns a zero/false result
	fieldData, ok = sensor.lookupTaskStructFields(dneDataFile, goodRelease)
	assert.False(t, ok)
	assert.Zero(t, fieldData)

	// Test that a badly formed JSON file returns a zero/false result
	fieldData, ok = sensor.lookupTaskStructFields(badDataFile, badRelease)
	assert.False(t, ok)
	assert.Zero(t, fieldData)

	// Test that an unknown release returns a zero/false result
	fieldData, ok = sensor.lookupTaskStructFields(goodDataFile, badRelease)
	assert.False(t, ok)
	assert.Zero(t, fieldData)
}

func TestCompareTaskStructSample(t *testing.T) {
	sensor := newUnstartedUnitTestSensor(t)

	fields := taskStructFields{
		PID:           StructField{1188, 4},
		TGID:          StructField{1192, 4},
		RealStartTime: StructField{1460, 16},
	}

	sample16 := expression.FieldValueMap{
		"pid":        int32(238746),
		"tgid":       int32(928374),
		"time_secs":  int64(827635),
		"time_nsecs": int64(2938742),
	}
	sample8 := expression.FieldValueMap{
		"pid":  sample16["pid"],
		"tgid": sample16["tgid"],
		"time_secs": (sample16["time_secs"].(int64) * 1e9) +
			sample16["time_nsecs"].(int64),
	}
	expectedStartTime := ((sample16["time_secs"].(int64) * 1e9) +
		sample16["time_nsecs"].(int64)) / 1e7

	var testCases = []struct {
		sample    expression.FieldValueMap
		pid, tgid int32
		startTime int64
		result    bool
	}{
		{sample16, 11111, 22222, 33333, false},
		{sample16, sample16["pid"].(int32), 22222, 33333, false},
		{sample16, sample16["pid"].(int32), sample16["tgid"].(int32), 33333, false},
		{sample16, sample16["pid"].(int32), sample16["tgid"].(int32), expectedStartTime, true},

		{sample8, 11111, 22222, 33333, false},
		{sample8, sample8["pid"].(int32), 22222, 33333, false},
		{sample8, sample8["pid"].(int32), sample8["tgid"].(int32), 33333, false},
		{sample8, sample8["pid"].(int32), sample8["tgid"].(int32), expectedStartTime, true},
	}

	for _, tc := range testCases {
		sensor.taskStructPID = StructField{}
		sensor.taskStructTGID = StructField{}
		sensor.taskStructRealStartTime = StructField{}

		result := sensor.compareTaskStructSample(tc.sample, tc.pid, tc.tgid,
			tc.startTime, fields)
		assert.Equal(t, tc.result, result)
		if result {
			assert.Equal(t, fields.PID, sensor.taskStructPID)
			assert.Equal(t, fields.TGID, sensor.taskStructTGID)
			assert.Equal(t, fields.RealStartTime, sensor.taskStructRealStartTime)
		} else {
			assert.Zero(t, sensor.taskStructPID)
			assert.Zero(t, sensor.taskStructTGID)
			assert.Zero(t, sensor.taskStructRealStartTime)
		}
	}
}

func TestNewSubscription(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := sensor.NewSubscription()
	assert.NotNil(t, s)
	assert.Equal(t, sensor, s.sensor)
	assert.Equal(t, uint64(1), s.subscriptionID)
}

func TestDispatchEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)

	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		dispatched = true
	}

	types := expression.FieldTypeMap{
		"field": expression.ValueTypeString,
	}
	ast := expression.Equal(
		expression.Identifier("field"),
		expression.Value("value"))
	expr, err := expression.ConvertExpression(ast, types)
	require.NoError(t, err)
	require.NotNil(t, expr)

	validFalseData := expression.FieldValueMap{
		"field": "aklsjd",
	}
	validTrueData := expression.FieldValueMap{
		"field": "value",
	}
	invalidData := expression.FieldValueMap{
		"field": uint64(982734),
	}

	eventid, _ := s.addTestEventSink(t, expr)
	sensor.eventMap.subscribe(s)

	var dummyEvent LostRecordTelemetryEvent
	dummyEvent.Init(sensor)
	dummyEvent.Lost = 239847293854

	// Attempt to dispatch an event to a non-existent eventid
	dispatched = false
	sensor.DispatchEvent(eventid+298374, dummyEvent, validTrueData)
	assert.False(t, dispatched)

	// Attempt to dispatch an event with invalid field data
	dispatched = false
	sensor.DispatchEvent(eventid, dummyEvent, invalidData)
	assert.False(t, dispatched)

	// Attempt to dispatch an event that evaluates to false
	dispatched = false
	sensor.DispatchEvent(eventid, dummyEvent, validFalseData)
	assert.False(t, dispatched)

	// Attempt a successful dispatch
	dispatched = false
	sensor.DispatchEvent(eventid, dummyEvent, validTrueData)
	assert.True(t, dispatched)
}

func TestDispatchEventToAllSubscription(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	const numSubscriptions = 8

	dispatchCount := 0
	for i := 0; i < numSubscriptions; i++ {
		s := newTestSubscription(t, sensor)
		s.dispatchFn = func(event TelemetryEvent) {
			dispatchCount++
		}
	}
}
