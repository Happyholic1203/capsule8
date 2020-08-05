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
	"testing"

	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"
	"github.com/Happyholic1203/capsule8/pkg/sys/proc/procfs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleSyscallTraceEnter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			TID:  uint32(sensorPID),
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"id":   int64(9237845),
		"arg0": uint64(0x11),
		"arg1": uint64(0x22),
		"arg2": uint64(0x33),
		"arg3": uint64(0x44),
		"arg4": uint64(0x55),
		"arg5": uint64(0x66),
	}
	setSampleRawData(sample, data)

	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(SyscallEnterTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, data["id"], e.ID)
		assert.Equal(t, data["arg0"], e.Arguments[0])
		assert.Equal(t, data["arg1"], e.Arguments[1])
		assert.Equal(t, data["arg2"], e.Arguments[2])
		assert.Equal(t, data["arg3"], e.Arguments[3])
		assert.Equal(t, data["arg4"], e.Arguments[4])
		assert.Equal(t, data["arg5"], e.Arguments[5])
		dispatched = true
	}

	eventid, _ := s.addTestEventSink(t, nil)
	s.handleSyscallTraceEnter(eventid, sample)
	require.False(t, dispatched)

	sample.TID = 0
	s.handleSyscallTraceEnter(eventid, sample)
	require.True(t, dispatched)
}

func TestHandleSysExit(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			TID:  uint32(sensorPID),
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"id":  int64(9237845),
		"ret": int64(3824567),
	}
	setSampleRawData(sample, data)

	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(SyscallExitTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, data["id"], e.ID)
		assert.Equal(t, data["ret"], e.Return)
		dispatched = true
	}

	eventid, _ := s.addTestEventSink(t, nil)
	s.handleSysExit(eventid, sample)
	require.False(t, dispatched)

	sample.TID = 0
	s.handleSysExit(eventid, sample)
	require.True(t, dispatched)
}

func TestInitSyscallNames(t *testing.T) {
	procFS, err := procfs.NewFileSystem("testdata")
	require.NoError(t, err)

	// raw_syscalls
	//
	monitor, err :=
		perf.NewEventMonitor(
			perf.WithProcFileSystem(procFS),
			perf.WithTracingDir("testdata/syscall_tests/raw_syscalls"),
			perf.WithEventSourceController(perf.NewStubEventSourceController()))
	require.NoError(t, err)

	sensor := &Sensor{}
	sensor.monitor.Store(monitor)
	s := Subscription{sensor: sensor}

	s.initSyscallNames()
	assert.Equal(t, "raw_syscalls/sys_enter", syscallEnterName)
	assert.Equal(t, "raw_syscalls/sys_exit", syscallExitName)

	// syscalls
	//
	monitor, err =
		perf.NewEventMonitor(
			perf.WithProcFileSystem(procFS),
			perf.WithTracingDir("testdata/syscall_tests/syscalls"),
			perf.WithEventSourceController(perf.NewStubEventSourceController()))
	require.NoError(t, err)

	sensor = &Sensor{}
	sensor.monitor.Store(monitor)
	s = Subscription{sensor: sensor}

	s.initSyscallNames()
	assert.Equal(t, "syscalls/sys_enter", syscallEnterName)
	assert.Equal(t, "syscalls/sys_exit", syscallExitName)
}

func TestRegisterGlobalDummySyscallEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)

	ok := s.registerGlobalDummySyscallEvent()
	require.True(t, ok)
	assert.Equal(t, int64(1), s.sensor.dummySyscallEventCount)
	assert.NotZero(t, s.sensor.dummySyscallEventID)
}

func TestRegisterLocalDummySyscallEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)

	ok := s.registerLocalDummySyscallEvent()
	require.True(t, ok)
}

func prepareForRegisterSyscallEnterEventFilter(t *testing.T, s *Subscription) {
	format := `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:s64 id;	offset:8;	size:8;	signed:1;
	field:u64 arg0;	offset:16;	size:8;	signed:0;
	field:u64 arg1;	offset:24;	size:8;	signed:0;
	field:u64 arg2;	offset:32;	size:8;	signed:0;
	field:u64 arg3;	offset:40;	size:8;	signed:0;
	field:u64 arg4;	offset:48;	size:8;	signed:0;
	field:u64 arg5;	offset:56;	size:8;	signed:0;

print fmt: "id=%d arg0=%d arg1=%d arg2=%d arg3=%d arg4=%d arg5=%d", REC->id, REC->arg0, REC->arg1, REC->arg2, REC->arg3, REC->arg4, REC->arg5`

	newUnitTestKprobe(t, s.sensor, 0, format)
}

func verifyRegisterSyscallEnterEventFilter(t *testing.T, s *Subscription, count int) {
	if count > 0 {
		assert.Len(t, s.eventSinks, count)
	} else {
		assert.Len(t, s.status, -count)
		assert.Len(t, s.eventSinks, 0)
	}
}

func TestRegisterSyscallEnterEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)
	prepareForRegisterSyscallEnterEventFilter(t, s)
	s.RegisterSyscallEnterEventFilter(nil)
	verifyRegisterSyscallEnterEventFilter(t, s, 1)
}

func verifyRegisterSyscallExitEventFilter(t *testing.T, s *Subscription, count int) {
	if count > 0 {
		assert.Len(t, s.eventSinks, count)
	} else {
		assert.Len(t, s.status, 1)
		assert.Len(t, s.eventSinks, 0)
	}
}

func TestRegisterSyscallExitEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)
	s.RegisterSyscallExitEventFilter(nil)
	verifyRegisterSyscallExitEventFilter(t, s, 1)
}
