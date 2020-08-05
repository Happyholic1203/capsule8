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
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileEventSource(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()
	m := sensor.FileMonitor

	fes := fileEventSource{
		probes: []fileProbe{},
	}

	err := fes.register(m, "TestFileEventSource")
	require.NoError(t, err)
	assert.Equal(t, int32(1), fes.counter)
	assert.NotZero(t, fes.groupid)

	fes.unregister(m)
	assert.Zero(t, fes.counter)
	assert.Zero(t, fes.groupid)
}

func TestFileEventSourceLostRecordHandler(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	var (
		receivedLostEvent bool
		lostEventType     LostRecordType
		lostEventCount    uint64
	)

	fes := fileEventSource{
		name:     "unit test",
		sensor:   sensor,
		eventid:  sensor.Monitor().ReserveEventID(),
		lostType: LostRecordTypeFileCreate,
	}

	s := newTestSubscription(t, sensor)
	_, err := s.addEventSink(fes.eventid, nil)
	require.NoError(t, err)

	dispatchFn := func(event TelemetryEvent) {
		if e, ok := event.(LostRecordTelemetryEvent); ok {
			receivedLostEvent = true
			lostEventType = e.Type
			lostEventCount = e.Lost
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.Run(ctx, dispatchFn)

	expectedLostType := fes.lostType
	expectedLostCount := uint64(238746)
	fes.lostRecordHandler(123, 456, perf.SampleID{}, expectedLostCount)
	cancel()

	if assert.True(t, receivedLostEvent) {
		assert.Equal(t, expectedLostType, lostEventType)
		assert.Equal(t, expectedLostCount, lostEventCount)
	}

	assert.Equal(t, expectedLostCount, sensor.Metrics.KernelSamplesLost)
}

func TestNewFileMonitor(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()
	m := sensor.FileMonitor

	assert.NotNil(t, m.fileCreateEventSource.probes)
	assert.NotZero(t, m.fileCreateEventSource.eventid)
	assert.NotNil(t, m.fileCreateEventSource.dispatch)

	assert.NotNil(t, m.fileDeleteEventSource.probes)
	assert.NotZero(t, m.fileDeleteEventSource.eventid)
	assert.NotNil(t, m.fileDeleteEventSource.dispatch)

	assert.NotNil(t, m.fileLinkEventSource.probes)
	assert.NotZero(t, m.fileLinkEventSource.eventid)
	assert.NotNil(t, m.fileLinkEventSource.dispatch)

	assert.NotNil(t, m.fileSymlinkEventSource.probes)
	assert.NotZero(t, m.fileSymlinkEventSource.eventid)
	assert.NotNil(t, m.fileSymlinkEventSource.dispatch)

	assert.NotNil(t, m.fileModifyEventSource.probes)
	assert.NotZero(t, m.fileModifyEventSource.eventid)
	assert.NotNil(t, m.fileModifyEventSource.dispatch)

	assert.NotNil(t, m.fileRenameEventSource.probes)
	assert.NotZero(t, m.fileRenameEventSource.eventid)
	assert.NotNil(t, m.fileRenameEventSource.dispatch)

	assert.NotNil(t, m.fileOpenForModifyEventSource.probes)
	assert.NotZero(t, m.fileOpenForModifyEventSource.eventid)
	assert.NotNil(t, m.fileOpenForModifyEventSource.dispatch)

	assert.NotNil(t, m.fileCloseForModifyEventSource.probes)
	assert.NotZero(t, m.fileCloseForModifyEventSource.eventid)
	assert.NotNil(t, m.fileCloseForModifyEventSource.dispatch)

	assert.NotNil(t, m.fileAttributeChangeEventSource.probes)
	assert.NotZero(t, m.fileAttributeChangeEventSource.eventid)
	assert.NotNil(t, m.fileAttributeChangeEventSource.dispatch)
}

func TestFileMonitorBuildFetchargs(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	// Set the dentry name and parent offsets to our own values so we can
	// test more easily rather than have different values depending on
	// which platform this test is running on. These values are meaningless
	// and arbitrary
	sensor.dentryStructName.Offset = 64
	sensor.dentryStructParent.Offset = 32

	testCases := []struct {
		dentry   dentryFetch
		expected string
	}{
		{dentryFetch{base: "XXX", prefix: 'A'}, "A01=+0(+64(XXX)):string A02=+0(+64(+32(XXX))):string A03=+0(+64(+32(+32(XXX)))):string A04=+0(+64(+32(+32(+32(XXX))))):string A05=+0(+64(+32(+32(+32(+32(XXX)))))):string A06=+0(+64(+32(+32(+32(+32(+32(XXX))))))):string A07=+0(+64(+32(+32(+32(+32(+32(+32(XXX)))))))):string A08=+0(+64(+32(+32(+32(+32(+32(+32(+32(XXX))))))))):string A09=+0(+64(+32(+32(+32(+32(+32(+32(+32(+32(XXX)))))))))):string"},
		{dentryFetch{base: "abc", prefix: 'B'}, "B01=+0(+64(abc)):string B02=+0(+64(+32(abc))):string B03=+0(+64(+32(+32(abc)))):string B04=+0(+64(+32(+32(+32(abc))))):string B05=+0(+64(+32(+32(+32(+32(abc)))))):string B06=+0(+64(+32(+32(+32(+32(+32(abc))))))):string B07=+0(+64(+32(+32(+32(+32(+32(+32(abc)))))))):string B08=+0(+64(+32(+32(+32(+32(+32(+32(+32(abc))))))))):string B09=+0(+64(+32(+32(+32(+32(+32(+32(+32(+32(abc)))))))))):string"},
	}

	for _, tc := range testCases {
		s := tc.dentry.buildFetchargs(sensor)
		assert.Equal(t, tc.expected, s)
	}
}

func TestFileMonitorConstructFilename(t *testing.T) {
	dentryDepth := 8

	shortData := expression.FieldValueMap{
		"flag": int32(0x403),
		"A01":  "passwd",
		"A02":  "etc",
		"A03":  "/",
	}
	for i := 4; i < dentryDepth; i++ {
		shortData[fmt.Sprintf("A%02d", i)] = "/"
	}
	shortExpected := "/etc/passwd"
	shortDentry := dentryFetch{
		base:      "XXX",
		key:       "path",
		prefix:    'A',
		usedDepth: dentryDepth,
	}
	shortActual := shortDentry.constructFilename(shortData)
	assert.Equal(t, shortExpected, shortActual)

	longParts := []string{".."}
	longData := expression.FieldValueMap{
		"flag": int32(0x403),
	}
	for i := dentryDepth; i > 0; i-- {
		s := string([]byte{byte(i) + 64})
		longParts = append(longParts, s)
		longData[fmt.Sprintf("A%02d", i)] = s
	}
	longExpected := strings.Join(longParts, "/")
	longDentry := dentryFetch{
		base:      "XXX",
		key:       "path",
		prefix:    'A',
		usedDepth: dentryDepth,
	}
	longActual := longDentry.constructFilename(longData)
	assert.Equal(t, longExpected, longActual)
}

func TestDispatchFileCreateEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()
	m := sensor.FileMonitor

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			TID:  uint32(sensorPID),
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"filename": "/to/foo.bar",
		"mode":     int32(0664),
		"mount":    int32(23),
	}

	s := newTestSubscription(t, sensor)
	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(FileCreateTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, "/run/to/foo.bar", e.Filename)
		assert.Equal(t, data["mode"], e.Mode)
		dispatched = true
	}
	s.addEventSink(m.fileCreateEventSource.eventid, nil)
	sensor.eventMap.subscribe(s)

	m.dispatchFileCreateEvent(sample, data)
	if !assert.False(t, dispatched) {
		dispatched = false
	}

	sample.TID = 0
	m.dispatchFileCreateEvent(sample, data)
	assert.True(t, dispatched)
}

func TestDispatchFileDeleteEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()
	m := sensor.FileMonitor

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			TID:  uint32(sensorPID),
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"filename": "/to/foo.bar",
		"mount":    int32(23),
	}

	s := newTestSubscription(t, sensor)
	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(FileDeleteTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, "/run/to/foo.bar", e.Filename)
		dispatched = true
	}
	s.addEventSink(m.fileDeleteEventSource.eventid, nil)
	sensor.eventMap.subscribe(s)

	m.dispatchFileDeleteEvent(sample, data)
	if !assert.False(t, dispatched) {
		dispatched = false
	}

	sample.TID = 0
	m.dispatchFileDeleteEvent(sample, data)
	assert.True(t, dispatched)
}

func TestDispatchFileLinkEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()
	m := sensor.FileMonitor

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			TID:  uint32(sensorPID),
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"source_file": "/to/foo.bar",
		"target_file": "/to/other/foo.bar",
		"mount":       int32(23),
	}

	s := newTestSubscription(t, sensor)
	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(FileLinkTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, "/run/to/foo.bar", e.SourceFile)
		assert.Equal(t, "/run/to/other/foo.bar", e.TargetFile)
		assert.Equal(t, false, e.Symlink)
		dispatched = true
	}
	s.addEventSink(m.fileLinkEventSource.eventid, nil)
	sensor.eventMap.subscribe(s)

	m.dispatchFileLinkEvent(sample, data)
	if !assert.False(t, dispatched) {
		dispatched = false
	}

	sample.TID = 0
	m.dispatchFileLinkEvent(sample, data)
	assert.True(t, dispatched)
}

func TestDispatchFileSymlinkEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()
	m := sensor.FileMonitor

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			TID:  uint32(sensorPID),
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"source_file": "/to/foo.bar",
		"target_file": "/path/to/other/foo.bar",
		"mount":       int32(23),
	}

	s := newTestSubscription(t, sensor)
	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(FileLinkTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, "/run/to/foo.bar", e.SourceFile)
		assert.Equal(t, "/path/to/other/foo.bar", e.TargetFile)
		assert.Equal(t, true, e.Symlink)
		dispatched = true
	}
	s.addEventSink(m.fileSymlinkEventSource.eventid, nil)
	sensor.eventMap.subscribe(s)

	m.dispatchFileSymlinkEvent(sample, data)
	if !assert.False(t, dispatched) {
		dispatched = false
	}

	sample.TID = 0
	m.dispatchFileSymlinkEvent(sample, data)
	assert.True(t, dispatched)
}

func TestDispatchFileModifyEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()
	m := sensor.FileMonitor

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			TID:  uint32(sensorPID),
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"filename": "/to/foo.bar",
		"mount":    int32(23),
		"mask":     uint32(2),
		"fd":       uint64(0),
		"parent":   uint64(1),
		"fstype":   uint32(1718644084),
	}

	s := newTestSubscription(t, sensor)
	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(FileModifyTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, "/run/to/foo.bar", e.Filename)
		dispatched = true
	}
	s.addEventSink(m.fileModifyEventSource.eventid, nil)
	sensor.eventMap.subscribe(s)

	m.dispatchFileModifyEvent(sample, data)
	if !assert.False(t, dispatched) {
		dispatched = false
	}

	sample.TID = 0
	m.dispatchFileModifyEvent(sample, data)
	assert.True(t, dispatched)
}

func TestDispatchFileOpenForModifyEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()
	m := sensor.FileMonitor

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			TID:  uint32(sensorPID),
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"filename": "/to/foo.bar",
		"mount":    int32(23),
		"flag":     int32(0x400),
	}

	s := newTestSubscription(t, sensor)
	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(FileOpenForModifyTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, "/run/to/foo.bar", e.Filename)
		dispatched = true
	}
	s.addEventSink(m.fileOpenForModifyEventSource.eventid, nil)
	sensor.eventMap.subscribe(s)

	m.dispatchFileOpenForModifyEvent(sample, data)
	if !assert.False(t, dispatched) {
		dispatched = false
	}

	sample.TID = 0
	m.dispatchFileOpenForModifyEvent(sample, data)
	assert.True(t, dispatched)
}

func TestDispatchFileCloseForModifyEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()
	m := sensor.FileMonitor

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			TID:  uint32(sensorPID),
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"filename": "/to/foo.bar",
		"mount":    int32(23),
		"mask":     uint32(8),
		"fd":       uint64(0),
		"parent":   uint64(1),
	}

	s := newTestSubscription(t, sensor)
	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(FileCloseForModifyTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, "/run/to/foo.bar", e.Filename)
		dispatched = true
	}
	s.addEventSink(m.fileCloseForModifyEventSource.eventid, nil)
	sensor.eventMap.subscribe(s)

	m.dispatchFileCloseForModifyEvent(sample, data)
	if !assert.False(t, dispatched) {
		dispatched = false
	}

	sample.TID = 0
	m.dispatchFileCloseForModifyEvent(sample, data)
	assert.True(t, dispatched)
}

func TestDispatchFileRenameEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()
	m := sensor.FileMonitor

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			TID:  uint32(sensorPID),
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"oldname": "/to/old/foo.bar",
		"newname": "/to/new/foo.bar",
		"mount":   int32(23),
	}

	s := newTestSubscription(t, sensor)
	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(FileRenameTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, "/run/to/old/foo.bar", e.Oldname)
		assert.Equal(t, "/run/to/new/foo.bar", e.Newname)
		dispatched = true
	}
	s.addEventSink(m.fileRenameEventSource.eventid, nil)
	sensor.eventMap.subscribe(s)

	m.dispatchFileRenameEvent(sample, data)
	if !assert.False(t, dispatched) {
		dispatched = false
	}

	sample.TID = 0
	m.dispatchFileRenameEvent(sample, data)
	assert.True(t, dispatched)
}

func TestHandleDoSysOpen(t *testing.T) {
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
		"filename": "/path/to/foo.bar",
		"flags":    int32(29384756),
		"mode":     int32(0664),
	}
	setSampleRawData(sample, data)

	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(FileOpenTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, "/path/to/foo.bar", e.Filename)
		assert.Equal(t, data["flags"], e.Flags)
		assert.Equal(t, data["mode"], e.Mode)
		dispatched = true
	}

	eventid, _ := s.addTestEventSink(t, nil)
	s.handleDoSysOpen(eventid, sample)
	require.False(t, dispatched)

	sample.TID = 0
	s.handleDoSysOpen(eventid, sample)
	require.True(t, dispatched)
}

func TestDispatchFileAttributeChangeEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()
	m := sensor.FileMonitor

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			TID:  uint32(sensorPID),
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"filename": "/hello/world.txt",
		"mount":    int32(23),
	}

	s := newTestSubscription(t, sensor)
	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(FileAttributeChangeTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, "/run/hello/world.txt", e.Filename)
		dispatched = true
	}
	s.addEventSink(m.fileAttributeChangeEventSource.eventid, nil)
	sensor.eventMap.subscribe(s)

	m.dispatchFileAttributeChangeEvent(sample, data)
	if !assert.False(t, dispatched) {
		dispatched = false
	}

	sample.TID = 0
	m.dispatchFileAttributeChangeEvent(sample, data)
	assert.True(t, dispatched)
}

func prepareForRegisterFileCreateEventFilter(t *testing.T, s *Subscription, delta uint64) {
	format := `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;
	field:int common_lock_depth;	offset:8;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:16;	size:8;	signed:0;
	field:s32 mode;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] A01;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] A02;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] A03;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] A04;	offset:40;	size:4;	signed:1;
	field:__data_loc char[] A05;	offset:44;	size:4;	signed:1;
	field:__data_loc char[] A06;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A07;	offset:52;	size:4;	signed:1;
	field:__data_loc char[] A08;	offset:56;	size:4;	signed:1;

print fmt: "(%lx) mode=%lx A01=%lx A02=%lx A03=%lx A04=%lx A05=%lx A06=%lx A07=%lx A08=%lx", REC->__probe_ip, REC->mode, REC->A01, REC->A02, REC->A03, REC->A04, REC->A05, REC->A06, REC->A07, REC->A08`
	newUnitTestKprobe(t, s.sensor, delta, format)

	format = `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;
	field:int common_lock_depth;	offset:8;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:16;	size:8;	signed:0;
	field:s32 mode;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] A01;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] A02;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] A03;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] A04;	offset:40;	size:4;	signed:1;
	field:__data_loc char[] A05;	offset:44;	size:4;	signed:1;
	field:__data_loc char[] A06;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A07;	offset:52;	size:4;	signed:1;
	field:__data_loc char[] A08;	offset:56;	size:4;	signed:1;

print fmt: "(%lx) mode=%lx A01=%lx A02=%lx A03=%lx A04=%lx A05=%lx A06=%lx A07=%lx A08=%lx", REC->__probe_ip, REC->mode, REC->A01, REC->A02, REC->A03, REC->A04, REC->A05, REC->A06, REC->A07, REC->A08`
	newUnitTestKprobe(t, s.sensor, delta+1, format)

	format = `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;
	field:int common_lock_depth;	offset:8;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:16;	size:8;	signed:0;
	field:s32 mode;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] A01;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] A02;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] A03;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] A04;	offset:40;	size:4;	signed:1;
	field:__data_loc char[] A05;	offset:44;	size:4;	signed:1;
	field:__data_loc char[] A06;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A07;	offset:52;	size:4;	signed:1;
	field:__data_loc char[] A08;	offset:56;	size:4;	signed:1;

print fmt: "(%lx) mode=%lx A01=%lx A02=%lx A03=%lx A04=%lx A05=%lx A06=%lx A07=%lx A08=%lx", REC->__probe_ip, REC->mode, REC->A01, REC->A02, REC->A03, REC->A04, REC->A05, REC->A06, REC->A07, REC->A08`
	newUnitTestKprobe(t, s.sensor, delta+2, format)
}

func prepareForRegisterFileDeleteEventFilter(t *testing.T, s *Subscription, delta uint64) {
	format := `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;	signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:s32 mount;	offset:16;	size:4;	signed:1;
	field:__data_loc char[] A01;	offset:20;	size:4;	signed:1;
	field:__data_loc char[] A02;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] A03;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] A04;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] A05;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] A06;	offset:40;	size:4;	signed:1;
	field:__data_loc char[] A07;	offset:44;	size:4;	signed:1;
	field:__data_loc char[] A08;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A09;	offset:52;	size:4;	signed:1;

print fmt: "(%lx) mount=%d A01=\"%s\" A02=\"%s\" A03=\"%s\" A04=\"%s\" A05=\"%s\" A06=\"%s\" A07=\"%s\" A08=\"%s\" A09=\"%s\"", REC->__probe_ip, REC->mount, __get_str(A01), __get_str(A02), __get_str(A03), __get_str(A04), __get_str(A05), __get_str(A06), __get_str(A07), __get_str(A08), __get_str(A09)`
	newUnitTestKprobe(t, s.sensor, delta, format)

	format = `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;
	field:int common_lock_depth;	offset:8;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:16;	size:8;	signed:0;
	field:__data_loc char[] A01;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] A02;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] A03;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] A04;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] A05;	offset:40;	size:4;	signed:1;
	field:__data_loc char[] A06;	offset:44;	size:4;	signed:1;
	field:__data_loc char[] A07;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A08;	offset:52;	size:4;	signed:1;

print fmt: "(%lx) A01=%lx A02=%lx A03=%lx A04=%lx A05=%lx A06=%lx A07=%lx A08=%lx", REC->__probe_ip, REC->A01, REC->A02, REC->A03, REC->A04, REC->A05, REC->A06, REC->A07, REC->A08`
	newUnitTestKprobe(t, s.sensor, delta+1, format)
}

func prepareForRegisterFileLinkEventFilter(t *testing.T, s *Subscription, delta uint64) {
	format := `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;	signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:s32 mount;	offset:16;	size:4;	signed:1;
	field:__data_loc char[] A01;	offset:20;	size:4;	signed:1;
	field:__data_loc char[] A02;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] A03;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] A04;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] A05;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] A06;	offset:40;	size:4;	signed:1;
	field:__data_loc char[] A07;	offset:44;	size:4;	signed:1;
	field:__data_loc char[] A08;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A09;	offset:52;	size:4;	signed:1;
	field:__data_loc char[] B01;	offset:56;	size:4;	signed:1;
	field:__data_loc char[] B02;	offset:60;	size:4;	signed:1;
	field:__data_loc char[] B03;	offset:64;	size:4;	signed:1;
	field:__data_loc char[] B04;	offset:68;	size:4;	signed:1;
	field:__data_loc char[] B05;	offset:72;	size:4;	signed:1;
	field:__data_loc char[] B06;	offset:76;	size:4;	signed:1;
	field:__data_loc char[] B07;	offset:80;	size:4;	signed:1;
	field:__data_loc char[] B08;	offset:84;	size:4;	signed:1;
	field:__data_loc char[] B09;	offset:88;	size:4;	signed:1;

print fmt: "(%lx) mount=%d A01=\"%s\" A02=\"%s\" A03=\"%s\" A04=\"%s\" A05=\"%s\" A06=\"%s\" A07=\"%s\" A08=\"%s\" A09=\"%s\" B01=\"%s\" B02=\"%s\" B03=\"%s\" B04=\"%s\" B05=\"%s\" B06=\"%s\" B07=\"%s\" B08=\"%s\" B09=\"%s\"", REC->__probe_ip, REC->mount, __get_str(A01), __get_str(A02), __get_str(A03), __get_str(A04), __get_str(A05), __get_str(A06), __get_str(A07), __get_str(A08), __get_str(A09), __get_str(B01), __get_str(B02), __get_str(B03), __get_str(B04), __get_str(B05), __get_str(B06), __get_str(B07), __get_str(B08), __get_str(B09)`
	newUnitTestKprobe(t, s.sensor, delta, format)

	format = `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;	signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:__data_loc char[] source_file;	offset:16;	size:4;	signed:1;
	field:s32 mount;	offset:20;	size:4;	signed:1;
	field:__data_loc char[] A01;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] A02;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] A03;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] A04;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] A05;	offset:40;	size:4;	signed:1;
	field:__data_loc char[] A06;	offset:44;	size:4;	signed:1;
	field:__data_loc char[] A07;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A08;	offset:52;	size:4;	signed:1;
	field:__data_loc char[] A09;	offset:56;	size:4;	signed:1;

print fmt: "(%lx) source_file=\"%s\" mount=%d A01=\"%s\" A02=\"%s\" A03=\"%s\" A04=\"%s\" A05=\"%s\" A06=\"%s\" A07=\"%s\" A08=\"%s\" A09=\"%s\"", REC->__probe_ip, __get_str(source_file), REC->mount, __get_str(A01), __get_str(A02), __get_str(A03), __get_str(A04), __get_str(A05), __get_str(A06), __get_str(A07), __get_str(A08), __get_str(A09)`
	newUnitTestKprobe(t, s.sensor, delta+1, format)
}

func prepareForRegisterFileOpenForModifyEventFilter(t *testing.T, s *Subscription, delta uint64) {
	format := `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;	signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:s32 flag;	offset:16;	size:4;	signed:1;
	field:s32 mount;	offset:20;	size:4;	signed:1;
	field:__data_loc char[] A01;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] A02;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] A03;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] A04;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] A05;	offset:40;	size:4;	signed:1;
	field:__data_loc char[] A06;	offset:44;	size:4;	signed:1;
	field:__data_loc char[] A07;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A08;	offset:52;	size:4;	signed:1;
	field:__data_loc char[] A09;	offset:56;	size:4;	signed:1;

print fmt: "(%lx) mask=%d mount=%d A01=\"%s\" A02=\"%s\" A03=\"%s\" A04=\"%s\" A05=\"%s\" A06=\"%s\" A07=\"%s\" A08=\"%s\" A09=\"%s\"", REC->__probe_ip, REC->flag, REC->mount, __get_str(A01), __get_str(A02), __get_str(A03), __get_str(A04), __get_str(A05), __get_str(A06), __get_str(A07), __get_str(A08), __get_str(A09)`
	newUnitTestKprobe(t, s.sensor, delta, format)
}

func prepareForRegisterFileCloseForModifyEventFilter(t *testing.T, s *Subscription, delta uint64) {
	format := `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;	signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:s32 mask;	offset:16;	size:4;	signed:1;
	field:s32 mount;	offset:20;	size:4;	signed:1;
	field:__data_loc char[] A01;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] A02;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] A03;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] A04;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] A05;	offset:40;	size:4;	signed:1;
	field:__data_loc char[] A06;	offset:44;	size:4;	signed:1;
	field:__data_loc char[] A07;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A08;	offset:52;	size:4;	signed:1;
	field:__data_loc char[] A09;	offset:56;	size:4;	signed:1;

print fmt: "(%lx) mask=%d mount=%d A01=\"%s\" A02=\"%s\" A03=\"%s\" A04=\"%s\" A05=\"%s\" A06=\"%s\" A07=\"%s\" A08=\"%s\" A09=\"%s\"", REC->__probe_ip, REC->mask, REC->mount, __get_str(A01), __get_str(A02), __get_str(A03), __get_str(A04), __get_str(A05), __get_str(A06), __get_str(A07), __get_str(A08), __get_str(A09)`
	newUnitTestKprobe(t, s.sensor, delta, format)
}

func prepareForRegisterFileOpenEventFilter(t *testing.T, s *Subscription, delta uint64) {
	format := `name: ^^NAME^^
id: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:__data_loc char[] filename;	offset:16;	size:4;	signed:1;
	field:s32 flags;	offset:20;	size:4;	signed:1;
	field:s32 mode;	offset:24;	size:4;	signed:1;

print fmt: "filename=\"%s\" flags=%d mode=%d", __get_str(filename), REC->flags, REC->mode`

	newUnitTestKprobe(t, s.sensor, delta, format)
}

func prepareForRegisterFileRenameEventFilter(t *testing.T, s *Subscription, delta uint64) {
	format := `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;	signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:s32 mount;	offset:16;	size:4;	signed:1;
	field:__data_loc char[] A01;	offset:20;	size:4;	signed:1;
	field:__data_loc char[] A02;	offset:24;	size:4;	signed:1;
	field:__data_loc char[] A03;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] A04;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] A05;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] A06;	offset:40;	size:4;	signed:1;
	field:__data_loc char[] A07;	offset:44;	size:4;	signed:1;
	field:__data_loc char[] A08;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A09;	offset:52;	size:4;	signed:1;
	field:__data_loc char[] B01;	offset:56;	size:4;	signed:1;
	field:__data_loc char[] B02;	offset:60;	size:4;	signed:1;
	field:__data_loc char[] B03;	offset:64;	size:4;	signed:1;
	field:__data_loc char[] B04;	offset:68;	size:4;	signed:1;
	field:__data_loc char[] B05;	offset:72;	size:4;	signed:1;
	field:__data_loc char[] B06;	offset:76;	size:4;	signed:1;
	field:__data_loc char[] B07;	offset:80;	size:4;	signed:1;
	field:__data_loc char[] B08;	offset:84;	size:4;	signed:1;
	field:__data_loc char[] B09;	offset:88;	size:4;	signed:1;

print fmt: "(%lx) mount=%d A01=\"%s\" A02=\"%s\" A03=\"%s\" A04=\"%s\" A05=\"%s\" A06=\"%s\" A07=\"%s\" A08=\"%s\" A09=\"%s\" B01=\"%s\" B02=\"%s\" B03=\"%s\" B04=\"%s\" B05=\"%s\" B06=\"%s\" B07=\"%s\" B08=\"%s\" B09=\"%s\"", REC->__probe_ip, REC->mount, __get_str(A01), __get_str(A02), __get_str(A03), __get_str(A04), __get_str(A05), __get_str(A06), __get_str(A07), __get_str(A08), __get_str(A09), __get_str(B01), __get_str(B02), __get_str(B03), __get_str(B04), __get_str(B05), __get_str(B06), __get_str(B07), __get_str(B08), __get_str(B09)`
	newUnitTestKprobe(t, s.sensor, delta, format)
}

func prepareForRegisterFileModifyEventFilter(t *testing.T, s *Subscription, delta uint64) {
	format := `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;
	field:int common_lock_depth;	offset:8;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:16;	size:8;	signed:0;
	field:s32 mask;	offset:24;	size:4;	signed:1;
	field:u64 fd;	offset:28;	size:8;	signed:0;
	field:u64 parent;	offset:36;	size:8;	signed:0;
	field:u32 fstype;	offset:44;	size:4;	signed:0;
	field:s32 mount;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A01;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] A02;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] A03;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] A04;	offset:40;	size:4;	signed:1;
	field:__data_loc char[] A05;	offset:44;	size:4;	signed:1;
	field:__data_loc char[] A06;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A07;	offset:52;	size:4;	signed:1;
	field:__data_loc char[] A08;	offset:56;	size:4;	signed:1;

print fmt: "(%lx) mask=%lx A01=%lx A02=%lx A03=%lx A04=%lx A05=%lx A06=%lx A07=%lx A08=%lx", REC->__probe_ip, REC->mask, REC->A01, REC->A02, REC->A03, REC->A04, REC->A05, REC->A06, REC->A07, REC->A08`
	newUnitTestKprobe(t, s.sensor, delta, format)
}

func prepareForRegisterFileAttributeChangeEventFilter(t *testing.T, s *Subscription, delta uint64) {
	format := `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;
	field:int common_lock_depth;	offset:8;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:16;	size:8;	signed:0;
	field:u64 fd;	offset:28;	size:8;	signed:0;
	field:u64 parent;	offset:36;	size:8;	signed:0;
	field:u32 fstype;	offset:44;	size:4;	signed:0;
	field:s32 mount;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A01;	offset:28;	size:4;	signed:1;
	field:__data_loc char[] A02;	offset:32;	size:4;	signed:1;
	field:__data_loc char[] A03;	offset:36;	size:4;	signed:1;
	field:__data_loc char[] A04;	offset:40;	size:4;	signed:1;
	field:__data_loc char[] A05;	offset:44;	size:4;	signed:1;
	field:__data_loc char[] A06;	offset:48;	size:4;	signed:1;
	field:__data_loc char[] A07;	offset:52;	size:4;	signed:1;
	field:__data_loc char[] A08;	offset:56;	size:4;	signed:1;

print fmt: "(%lx) A01=%lx A02=%lx A03=%lx A04=%lx A05=%lx A06=%lx A07=%lx A08=%lx", REC->__probe_ip, REC->A01, REC->A02, REC->A03, REC->A04, REC->A05, REC->A06, REC->A07, REC->A08`
	newUnitTestKprobe(t, s.sensor, delta, format)
}

func verifyRegisterFileEventRegistration(t *testing.T, s *Subscription, count int) {
	if count > 0 {
		assert.Len(t, s.eventSinks, count)
	} else {
		assert.Len(t, s.status, -count)
		assert.Len(t, s.eventSinks, 0)
	}
}

func TestRegisterFileCreateEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)
	prepareForRegisterFileCreateEventFilter(t, s, 0)
	s.RegisterFileCreateEventFilter(nil)
	verifyRegisterFileEventRegistration(t, s, 1)
}

func TestRegisterFileDeleteEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)
	prepareForRegisterFileDeleteEventFilter(t, s, 0)
	s.RegisterFileDeleteEventFilter(nil)
	verifyRegisterFileEventRegistration(t, s, 1)
}

func TestRegisterFileLinkEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)
	prepareForRegisterFileLinkEventFilter(t, s, 0)
	s.RegisterFileLinkEventFilter(nil)
	verifyRegisterFileEventRegistration(t, s, 2)
}

func TestRegisterFileModifyEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)
	prepareForRegisterFileModifyEventFilter(t, s, 0)
	s.RegisterFileModifyEventFilter(nil)
	verifyRegisterFileEventRegistration(t, s, 1)
}

func TestRegisterFileOpenForModifyEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)
	prepareForRegisterFileOpenForModifyEventFilter(t, s, 0)
	s.RegisterFileOpenForModifyEventFilter(nil)
	verifyRegisterFileEventRegistration(t, s, 1)
}

func TestRegisterFileCloseForModifyEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)
	prepareForRegisterFileCloseForModifyEventFilter(t, s, 0)
	s.RegisterFileCloseForModifyEventFilter(nil)
	verifyRegisterFileEventRegistration(t, s, 1)
}

func TestRegisterFileOpenEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)
	prepareForRegisterFileOpenEventFilter(t, s, 0)
	s.RegisterFileOpenEventFilter(nil)
	verifyRegisterFileEventRegistration(t, s, 1)
}

func TestRegisterFileRenameEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)
	prepareForRegisterFileRenameEventFilter(t, s, 0)
	s.RegisterFileRenameEventFilter(nil)
	verifyRegisterFileEventRegistration(t, s, 1)
}

func TestRegisterFileAttributeChangeEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)
	prepareForRegisterFileAttributeChangeEventFilter(t, s, 0)
	s.RegisterFileAttributeChangeEventFilter(nil)
	verifyRegisterFileEventRegistration(t, s, 1)
}
