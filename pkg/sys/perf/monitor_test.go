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

package perf

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys/proc"
	"github.com/Happyholic1203/capsule8/pkg/sys/proc/procfs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/sys/unix"
)

type testProcFileSystem struct{}

func newTestProcFileSystem() proc.FileSystem {
	return &testProcFileSystem{}
}

func (fs *testProcFileSystem) BootID() string                  { return "bootid" }
func (fs *testProcFileSystem) MaxPID() uint                    { return 32768 }
func (fs *testProcFileSystem) SelfTGID() int                   { return 2432 }
func (fs *testProcFileSystem) NumCPU() int                     { return 2 }
func (fs *testProcFileSystem) Mounts() []proc.Mount            { return nil }
func (fs *testProcFileSystem) HostFileSystem() proc.FileSystem { return fs }
func (fs *testProcFileSystem) PerfEventDir() string            { return "" }
func (fs *testProcFileSystem) TracingDir() string              { return "testdata" }

func (fs *testProcFileSystem) SupportedFilesystems() []string {
	return []string{"debugfs", "cgroup", "ext4"}
}

func (fs *testProcFileSystem) KernelTextSymbolNames() (map[string]string, error) {
	return nil, unix.ENOSYS
}

func (fs *testProcFileSystem) ProcessContainerID(pid int) (string, error) {
	return "", unix.ESRCH
}

func (fs *testProcFileSystem) ProcessCommandLine(pid int) ([]string, error) {
	return nil, unix.ESRCH
}

func (fs *testProcFileSystem) ProcessMappings(pid int) ([]proc.MemoryMapping, error) {
	return nil, unix.ESRCH
}

func (fs *testProcFileSystem) TaskControlGroups(tigd, pid int) ([]proc.ControlGroup, error) {
	return nil, unix.ESRCH
}

func (fs *testProcFileSystem) TaskCWD(tgid, pid int) (string, error) {
	return "", unix.ESRCH
}

func (fs *testProcFileSystem) ProcessExecutable(pid int) (string, error) {
	return "", unix.ESRCH
}

func (fs *testProcFileSystem) TaskStartTime(tgid, pid int) (int64, error) {
	return 0, unix.ESRCH
}

func (fs *testProcFileSystem) TaskStatTime(tgid, pid int) (int64, error) {
	return 0, unix.ESRCH
}

func (fs *testProcFileSystem) TaskUniqueID(tgid, pid int, startTime int64) string {
	return fmt.Sprintf("%d-%d-%d", tgid, pid, startTime)
}

func (fs *testProcFileSystem) WalkTasks(walkFunc proc.TaskWalkFunc) error {
	return nil
}

func (fs *testProcFileSystem) ReadTaskStatus(tgid, pid int, i interface{}) error {
	return unix.ESRCH
}

var testFilterEventTypes = []EventType{
	EventTypeTracepoint, EventTypeKprobe, EventTypeUprobe,
}
var testNoFilterEventTypes = []EventType{
	EventTypeBreakpoint, EventTypeHardware, EventTypeHardwareCache,
	EventTypeRaw, EventTypeSoftware,
}

func TestEventMonitorOptions(t *testing.T) {
	expOptions := newEventMonitorOptions()
	expOptions.eventSourceController = NewStubEventSourceController()
	expOptions.flags = 827634
	expOptions.defaultEventAttr = &EventAttr{}
	expOptions.perfEventDir = "*** perf_event_dir ***"
	expOptions.tracingDir = "*** tracing_dir ***"
	expOptions.ringBufferNumPages = 88
	expOptions.cgroups = []string{"docker", "kubernetes", "capsule8"}
	expOptions.pids = []int{123, 456, 789}

	var err error
	expOptions.procfs, err = procfs.NewFileSystem("../proc/procfs/testdata/proc")
	assert.NoError(t, err)

	options := []EventMonitorOption{
		WithFlags(expOptions.flags),
		WithDefaultEventAttr(expOptions.defaultEventAttr),
		WithEventSourceController(expOptions.eventSourceController),
		WithProcFileSystem(expOptions.procfs),
		WithPerfEventDir(expOptions.perfEventDir),
		WithTracingDir(expOptions.tracingDir),
		WithRingBufferNumPages(expOptions.ringBufferNumPages),
		WithCgroups(expOptions.cgroups),
		WithCgroup("extra"),
		WithPids(expOptions.pids),
		WithPid(-1),
	}
	expOptions.cgroups = append(expOptions.cgroups, "extra")
	expOptions.pids = append(expOptions.pids, -1)

	gotOptions := eventMonitorOptions{}
	gotOptions.processOptions(options...)
	assert.Equal(t, expOptions, gotOptions)
}

func TestRegisterEventOptions(t *testing.T) {
	expOptions := newRegisterEventOptions()
	expOptions.disabled = true
	expOptions.eventAttr = &EventAttr{}
	expOptions.filter = "*** filter string ***"
	expOptions.name = "nnAAmmEE"

	options := []RegisterEventOption{
		WithEventDisabled(),
		WithEventAttr(expOptions.eventAttr),
		WithFilter(expOptions.filter),
		WithTracingEventName(expOptions.name),
	}

	gotOptions := registerEventOptions{}
	gotOptions.processOptions(options...)
	assert.Equal(t, expOptions, gotOptions)

	expOptions.disabled = false
	gotOptions.processOptions(WithEventEnabled())
	assert.Equal(t, expOptions, gotOptions)
}

func TestEventTypeMappings(t *testing.T) {
	fromPerfTypes := map[uint32]EventType{
		PERF_TYPE_BREAKPOINT: EventTypeBreakpoint,
		PERF_TYPE_HARDWARE:   EventTypeHardware,
		PERF_TYPE_HW_CACHE:   EventTypeHardwareCache,
		PERF_TYPE_RAW:        EventTypeRaw,
		PERF_TYPE_SOFTWARE:   EventTypeSoftware,
		PERF_TYPE_TRACEPOINT: EventTypeTracepoint,
	}
	for pt, expET := range fromPerfTypes {
		gotET := eventTypeFromPerfType(pt)
		assert.Equalf(t, expET, gotET, "mismatch PERF_TYPE_ %d", pt)
	}

	fromEventTypes := map[EventType]uint32{
		EventTypeTracepoint:    PERF_TYPE_TRACEPOINT,
		EventTypeKprobe:        PERF_TYPE_TRACEPOINT,
		EventTypeUprobe:        PERF_TYPE_TRACEPOINT,
		EventTypeHardware:      PERF_TYPE_HARDWARE,
		EventTypeSoftware:      PERF_TYPE_SOFTWARE,
		EventTypeHardwareCache: PERF_TYPE_HW_CACHE,
		EventTypeRaw:           PERF_TYPE_RAW,
		EventTypeBreakpoint:    PERF_TYPE_BREAKPOINT,
		// EventTypeDynamicPMU does not map
		// EventTypeExternal does not map
	}
	for et, expPT := range fromEventTypes {
		gotPT := perfTypeFromEventType(et)
		assert.Equalf(t, expPT, gotPT, "mismatch %s", EventTypeNames[et])
	}
}

func TestCounterEventSampleHandler(t *testing.T) {
	type testCase struct {
		eventID     uint64
		timeEnabled uint64
		timeRunning uint64
		counters    []CounterEventValue
	}
	var exp, got testCase

	h := counterEventSampleHandler{}
	h.handlerFn = func(eventid uint64, sample *Sample, counters []CounterEventValue, timeEnabled, timeRunning uint64) {
		got = testCase{
			eventID:     eventid,
			timeEnabled: timeEnabled,
			timeRunning: timeRunning,
			counters:    counters,
		}
	}

	exp = testCase{
		eventID:     203948,
		timeEnabled: 29384756,
		timeRunning: 12039487,
		counters: []CounterEventValue{
			CounterEventValue{
				EventType: EventTypeHardware,
				Config:    PERF_COUNT_HW_BRANCH_INSTRUCTIONS,
				Value:     234568,
			},
			CounterEventValue{
				EventType: EventTypeHardwareCache,
				Config:    PERF_COUNT_HW_CACHE_MISSES,
				Value:     345789329,
			},
		},
	}

	attrMap := newEventAttrMap()
	V := CounterGroup{
		TimeEnabled: exp.timeEnabled,
		TimeRunning: exp.timeRunning,
	}
	for i, c := range exp.counters {
		attrMap[uint64(i)] = &EventAttr{
			Type:   perfTypeFromEventType(c.EventType),
			Config: c.Config,
		}
		v := CounterValue{
			ID:    uint64(i),
			Value: c.Value,
		}
		V.Values = append(V.Values, v)
	}

	sample := Sample{
		Type: PERF_RECORD_SAMPLE,
		V:    V,
	}

	expectedLostCount := uint64(982734)
	lostSample := Sample{
		Type: PERF_RECORD_LOST,
		Lost: expectedLostCount,
	}

	monitor := &EventMonitor{}
	monitor.eventAttrMap.updateInPlace(attrMap)

	var gotLostCount uint64
	e := &registeredEvent{
		id: exp.eventID,
		group: &eventMonitorGroup{
			lostRecordFn: func(_ uint64, _ int32, _ SampleID, count uint64) {
				gotLostCount += count
			},
		},
	}
	h.handleSample(e, &lostSample, monitor)
	h.handleSample(e, &sample, monitor)
	assert.Equal(t, exp, got)

	assert.Equal(t, expectedLostCount, gotLostCount)
}

func TestTraceEventSampleHandler(t *testing.T) {
	monitor := &EventMonitor{
		tracingDir: "testdata",
	}

	var gotEventID uint64
	fn := func(eventid uint64, sample *Sample) {
		gotEventID = eventid
	}
	id, format, err := getTraceEventFormat(monitor.tracingDir, "valid/valid2")
	assert.NoError(t, err)
	monitor.traceFormats.insert(id, format)

	rawData := []byte{
		0x4e, 0x00, // common_type
		0x88,                   // common_flags
		0x23,                   // common_preempt_count
		0x11, 0x22, 0x33, 0x44, // common_pid
		0x44, 0x33, 0x22, 0x11, // pid
	}

	sample := Sample{
		Type:    PERF_RECORD_SAMPLE,
		RawData: rawData,
	}

	expectedLostCount := uint64(982734)
	lostSample := Sample{
		Type: PERF_RECORD_LOST,
		Lost: expectedLostCount,
	}

	var gotLostCount uint64
	e := &registeredEvent{
		id: 290873,
		group: &eventMonitorGroup{
			lostRecordFn: func(_ uint64, _ int32, _ SampleID, count uint64) {
				gotLostCount += count
			},
		},
	}

	h := traceEventSampleHandler{handlerFn: fn}
	h.handleSample(e, &lostSample, monitor)
	h.handleSample(e, &sample, monitor)
	assert.Equal(t, uint64(290873), gotEventID)

	assert.Equal(t, expectedLostCount, gotLostCount)
}

func TestRegisteredEventFilterSample(t *testing.T) {
	event := registeredEvent{}

	// If there is no filter set, the sample should not be filtered.
	r := event.filterSample(nil)
	assert.False(t, r)

	types := expression.FieldTypeMap{
		"foo": expression.ValueTypeSignedInt32,
	}
	var err error
	event.filter, err = expression.ParseString("foo == 12",
		expression.ParseModeKernelFilter, types)
	require.NoError(t, err)
	assert.NotNil(t, event.filter)

	// If there's an error in evaluation, the sample should be filtered
	// Pass type mismatch in values to cause an evaluation error
	data := expression.FieldValueMap{"foo": "string"}
	r = event.filterSample(data)
	assert.True(t, r)

	// Normal evaluation
	data = expression.FieldValueMap{"foo": int32(12)}
	r = event.filterSample(data)
	assert.False(t, r)

	data = expression.FieldValueMap{"foo": int32(8)}
	r = event.filterSample(data)
	assert.True(t, r)
}

func (event *registeredEvent) validateKernelFilter(t *testing.T, e *expression.Expression) {
	require.True(t, event.kernelFilter)
	assert.Nil(t, event.filter)
	for _, source := range event.sources {
		stub := source.(*StubEventSource)
		assert.Equal(t, e.KernelFilterString(), stub.Filter)
	}
}

func (event *registeredEvent) validateExpressionFilter(t *testing.T, e *expression.Expression) {
	require.False(t, event.kernelFilter)
	assert.Equal(t, e, event.filter)
	for _, source := range event.sources {
		stub := source.(*StubEventSource)
		assert.Equal(t, "", stub.Filter)
	}
}

func TestRegisteredEventSetFilter(t *testing.T) {
	for _, eventType := range testNoFilterEventTypes {
		event := registeredEvent{eventType: eventType}
		err := event.setFilter(nil)
		assert.Error(t, err, EventTypeNames[eventType])
	}

	event := registeredEvent{
		id:   827346,
		name: "TestRegisteredEventSetFilter",
		sources: []EventSource{
			newStubEventSource(EventAttr{}),
			newStubEventSource(EventAttr{}),
		},
		fields: expression.FieldTypeMap{
			"s":   expression.ValueTypeString,
			"s32": expression.ValueTypeSignedInt32,
			"u64": expression.ValueTypeUnsignedInt64,
		},
		eventType: EventTypeTracepoint,
	}

	validExpr, err := expression.ParseString("s32 == 12",
		expression.ParseModeKernelFilter, event.fields)
	require.NoError(t, err)
	require.NotNil(t, validExpr)
	err = validExpr.ValidateKernelFilter()
	require.NoError(t, err)

	invalidExpr, err := expression.ParseString("s == \"abc \\\" def\"",
		expression.ParseModeKernelFilter, event.fields)
	require.NoError(t, err)
	require.NotNil(t, invalidExpr)
	err = invalidExpr.ValidateKernelFilter()
	require.Error(t, err)

	// Set a filter that'll get set in the kernel
	err = event.setFilter(validExpr)
	require.NoError(t, err)
	event.validateKernelFilter(t, validExpr)

	// Clear the filter
	err = event.setFilter(nil)
	require.NoError(t, err)
	event.validateExpressionFilter(t, nil)

	// Set a filter that won't get set in the kernel
	err = event.setFilter(invalidExpr)
	require.NoError(t, err)
	event.validateExpressionFilter(t, invalidExpr)

	// Clear the filter
	err = event.setFilter(nil)
	require.NoError(t, err)
	event.validateExpressionFilter(t, nil)

	// Set a kernel filter again, then replace it with an expression filter
	err = event.setFilter(validExpr)
	require.NoError(t, err)
	event.validateKernelFilter(t, validExpr)

	err = event.setFilter(invalidExpr)
	require.NoError(t, err)
	event.validateExpressionFilter(t, invalidExpr)

	err = event.setFilter(validExpr)
	require.NoError(t, err)
	event.validateKernelFilter(t, validExpr)
}

func TestPerfGroupLeader(t *testing.T) {
	pgl := perfGroupLeader{
		source: newStubEventSourceLeader(EventAttr{}, -1, 0),
		state:  perfGroupLeaderStateActive,
	}
	assert.True(t, pgl.active())

	pgl.state = perfGroupLeaderStateClosing
	assert.False(t, pgl.active())

	pgl.cleanup()
	assert.Equal(t, pgl.state, perfGroupLeaderStateClosed)
	assert.False(t, pgl.active())
}

func TestEventMonitorGroup(t *testing.T) {
	firstEventID := uint64(2934678)
	attr := EventAttr{
		Type:     PERF_TYPE_SOFTWARE,
		Config:   927834,
		Disabled: true}

	for x := 0; x < 2; x++ {
		monitor := &EventMonitor{
			nextEventID: firstEventID,
		}
		monitor.isRunning.Store(x == 1)

		var name string
		if monitor.isRunning.Load().(bool) {
			name = "unit test (running)"
		} else {
			name = "unit test"
		}

		leaders := []*StubEventSourceLeader{
			newStubEventSourceLeader(attr, -1, 0),
			newStubEventSourceLeader(attr, -1, 1),
		}
		group := &eventMonitorGroup{
			name: name,
			leaders: []*perfGroupLeader{
				&perfGroupLeader{
					source: leaders[0],
					state:  perfGroupLeaderStateActive,
				},
				&perfGroupLeader{
					source: leaders[1],
					state:  perfGroupLeaderStateActive,
				},
			},
			events:  make(map[uint64]*registeredEvent),
			monitor: monitor,
		}

		// Test: group.perfEventOpen
		sources, err := group.perfEventOpen(name, attr, 0)
		assert.NoError(t, err)
		assert.NotNil(t, sources)
		assert.Len(t, sources, len(leaders))

		fields := expression.FieldTypeMap{
			"foo": expression.ValueTypeSignedInt32,
		}
		id := monitor.newRegisteredEvent(name, sources, fields, nil,
			EventTypeTracepoint, nil, &attr, group, false, 88)
		assert.Equal(t, firstEventID, id)
		assert.Len(t, monitor.eventAttrMap.getMap(), len(leaders))
		assert.Len(t, monitor.eventIDMap.getMap(), len(leaders))
		assert.Len(t, group.events, 1)
		assert.Len(t, monitor.events.getMap(), 1)

		expFilter := "foo == 8"
		err = monitor.SetFilter(id, expFilter)
		assert.NoError(t, err)
		for _, source := range sources {
			assert.Equal(t, expFilter, source.(*StubEventSource).Filter)
		}

		// Test: group.enable
		group.enable()
		assert.True(t, sources[0].(*StubEventSource).Enabled)
		assert.True(t, sources[1].(*StubEventSource).Enabled)
		assert.Equal(t, 1, sources[0].(*StubEventSource).EnableCount)
		assert.Equal(t, 1, sources[1].(*StubEventSource).EnableCount)

		// Test: group.disable
		group.disable()
		assert.False(t, sources[0].(*StubEventSource).Enabled)
		assert.False(t, sources[0].(*StubEventSource).Enabled)
		assert.Equal(t, 1, sources[0].(*StubEventSource).DisableCount)
		assert.Equal(t, 1, sources[1].(*StubEventSource).DisableCount)

		// Test: group.cleanup
		group.cleanup()
		assert.Len(t, monitor.eventAttrMap.getMap(), 0)
		assert.Len(t, monitor.eventIDMap.getMap(), 0)
		assert.Len(t, group.events, 0)
		assert.Len(t, monitor.events.getMap(), 0)
	}
}

func createTempTracingDir() (string, error) {
	tracingDir, err := ioutil.TempDir("", "capsule8_")
	if err != nil {
		return "", err
	}

	kprobeEvents := filepath.Join(tracingDir, "kprobe_events")
	if err = ioutil.WriteFile(kprobeEvents, []byte{}, 0666); err != nil {
		return tracingDir, err
	}

	uprobeEvents := filepath.Join(tracingDir, "uprobe_events")
	if err = ioutil.WriteFile(uprobeEvents, []byte{}, 0666); err != nil {
		return tracingDir, err
	}

	return tracingDir, nil
}

func TestDoesTracepointExist(t *testing.T) {
	monitor := EventMonitor{
		tracingDir: "./testdata",
	}
	assert.True(t, monitor.DoesTracepointExist("valid/valid"))
	assert.False(t, monitor.DoesTracepointExist("junk"))
	assert.False(t, monitor.DoesTracepointExist("foo/does_not_exist"))
}

func TestTraceCommands(t *testing.T) {
	tracingDir, err := createTempTracingDir()
	if tracingDir != "" {
		defer os.RemoveAll(tracingDir)
	}
	assert.NoError(t, err)

	kprobeEvents := filepath.Join(tracingDir, "kprobe_events")
	uprobeEvents := filepath.Join(tracingDir, "uprobe_events")

	monitor := &EventMonitor{
		tracingDir: tracingDir,
	}

	// writeTraceCommand opens append only, so the file needs to be created
	// first
	rawTestFilename := filepath.Join(tracingDir, "raw_test")
	err = ioutil.WriteFile(rawTestFilename, []byte{}, 0666)
	assert.NoError(t, err)

	exp := "oodles of fun!"
	err = monitor.writeTraceCommand("raw_test", exp)
	assert.NoError(t, err)
	got, err := ioutil.ReadFile(rawTestFilename)
	assert.NoError(t, err)
	assert.Equal(t, exp, string(got))

	kprobeTestCases := []struct {
		name     string
		address  string
		onReturn bool
		output   string
		expected string
	}{
		{"group/kprobe_name_1", "address", false, "arg1=%ax:u64     arg2=%bx:s32   ",
			"p:group/kprobe_name_1 address arg1=%ax:u64 arg2=%bx:s32",
		},
		{"group/kretprobe_name_2", "asdfasdf", true, "r=%retval:u64",
			"r:group/kretprobe_name_2 asdfasdf r=%retval:u64",
		},
	}
	for _, tc := range kprobeTestCases {
		err = os.Truncate(kprobeEvents, 0)
		assert.NoError(t, err)

		err = monitor.addKprobe(tc.name, tc.address, tc.onReturn, tc.output)
		assert.NoError(t, err)
		got, err = ioutil.ReadFile(kprobeEvents)
		assert.NoError(t, err)
		assert.Equal(t, tc.expected, string(got))
	}

	err = os.Truncate(kprobeEvents, 0)
	assert.NoError(t, err)

	err = monitor.removeKprobe("foo/bar")
	assert.NoError(t, err)
	got, err = ioutil.ReadFile(kprobeEvents)
	assert.NoError(t, err)
	assert.Equal(t, "-:foo/bar", string(got))

	uprobeTestCases := []struct {
		name     string
		bin      string
		address  string
		onReturn bool
		output   string
		expected string
	}{
		{"group/uprobe_name_1", "/bin/sh", "address", false,
			"arg1=%ax:u64     arg2=%bx:s32   ",
			"p:group/uprobe_name_1 /bin/sh:address arg1=%ax:u64 arg2=%bx:s32",
		},
		{"group/uretprobe_name_2", "/bin/ls", "asdfasdf", true,
			"r=%retval:u64",
			"r:group/uretprobe_name_2 /bin/ls:asdfasdf r=%retval:u64",
		},
	}
	for _, tc := range uprobeTestCases {
		err = os.Truncate(uprobeEvents, 0)
		assert.NoError(t, err)

		err = monitor.addUprobe(tc.name, tc.bin, tc.address, tc.onReturn, tc.output)
		assert.NoError(t, err)
		got, err = ioutil.ReadFile(uprobeEvents)
		assert.NoError(t, err)
		assert.Equal(t, tc.expected, string(got))
	}

	err = os.Truncate(uprobeEvents, 0)
	assert.NoError(t, err)

	err = monitor.removeUprobe("foo/bar")
	assert.NoError(t, err)
	got, err = ioutil.ReadFile(uprobeEvents)
	assert.NoError(t, err)
	assert.Equal(t, "-:foo/bar", string(got))
}

func TestEventGroupRegistration(t *testing.T) {
	monitor, err := NewEventMonitor(
		WithEventSourceController(NewStubEventSourceController()),
		WithProcFileSystem(newTestProcFileSystem()),
		WithTracingDir("testdata"),
		WithPid(234),
		WithPid(234))
	require.NoError(t, err)
	assert.Len(t, monitor.groups, 0)

	defer func() {
		monitor.isRunning.Store(false)
		monitor.Close()
	}()

	err = monitor.EnableGroup(867)
	assert.Error(t, err)

	err = monitor.DisableGroup(867)
	assert.Error(t, err)

	err = monitor.UnregisterEventGroup(867)
	assert.Error(t, err)

	for x := 0; x < 2; x++ {
		monitor.isRunning.Store(x == 1)

		var id int32
		id, err = monitor.RegisterEventGroup("", nil)
		assert.NoError(t, err)
		assert.Equal(t, int32(x), id)
		assert.Len(t, monitor.groups, 1)

		err = monitor.EnableGroup(id)
		assert.NoError(t, err)

		err = monitor.DisableGroup(id)
		assert.NoError(t, err)

		err = monitor.UnregisterEventGroup(id)
		assert.NoError(t, err)
		assert.Len(t, monitor.groups, 0)
	}
}

func TestEventManipulation(t *testing.T) {
	tracingDir, err := createTempTracingDir()
	if tracingDir != "" {
		defer os.RemoveAll(tracingDir)
	}
	assert.NoError(t, err)

	monitor, err := NewEventMonitor(
		WithEventSourceController(NewStubEventSourceController()),
		WithProcFileSystem(newTestProcFileSystem()),
		WithTracingDir(tracingDir))
	require.NoError(t, err)
	defer monitor.Close()

	groupID, err := monitor.RegisterEventGroup("default", nil)
	require.NoError(t, err)

	fields := expression.FieldTypeMap{
		"s":   expression.ValueTypeString,
		"s32": expression.ValueTypeSignedInt32,
		"u64": expression.ValueTypeUnsignedInt64,
	}

	// For all unfilterable event types, ensure this errors:
	for _, eventType := range testNoFilterEventTypes {
		opts := registerEventOptions{
			filter: "s32 == 12",
		}
		_, err = monitor.newRegisteredPerfEvent(
			EventTypeNames[eventType], 888,
			fields, opts, eventType, nil, 88, groupID)
		assert.Error(t, err, EventTypeNames[eventType])
	}

	eventTypes := []EventType{
		EventTypeBreakpoint, EventTypeHardware,
		EventTypeHardwareCache, EventTypeKprobe, EventTypeRaw,
		EventTypeSoftware, EventTypeTracepoint, EventTypeUprobe,
	}
	ids := make([]uint64, len(eventTypes))
	for x, eventType := range eventTypes {
		opts := registerEventOptions{
			disabled: true,
		}
		for _, et := range testFilterEventTypes {
			if eventType == et {
				opts.filter = "s32 == 12"
				break
			}
		}
		var eventid uint64
		eventid, err = monitor.newRegisteredPerfEvent(
			EventTypeNames[eventType], uint64(x),
			fields, opts, eventType, nil, 88, groupID)
		assert.NoError(t, err)
		assert.Len(t, monitor.events.getMap(), x+1)
		e, ok := monitor.events.lookup(eventid)
		assert.True(t, ok)
		assert.Equal(t, eventid, e.id)
		assert.Equal(t, eventType, e.eventType)
		assert.Equal(t, uint16(88), e.formatID)
		ids[x] = eventid
	}

	for x, eventid := range ids {
		eventType, ok := monitor.RegisteredEventType(eventid)
		assert.True(t, ok)
		assert.Equal(t, eventTypes[x], eventType)

		gotFields := monitor.RegisteredEventFields(eventid)
		assert.NotNil(t, gotFields)
		assert.Equal(t, fields, gotFields)

		monitor.Enable(eventid)
		e, ok := monitor.events.lookup(eventid)
		assert.True(t, ok)
		for _, source := range e.sources {
			assert.True(t, source.(*StubEventSource).Enabled)
			assert.Equal(t, 1, source.(*StubEventSource).EnableCount)
		}

		monitor.Disable(eventid)
		e, ok = monitor.events.lookup(eventid)
		assert.True(t, ok)
		for _, source := range e.sources {
			assert.False(t, source.(*StubEventSource).Enabled)
			assert.Equal(t, 1, source.(*StubEventSource).DisableCount)
		}
	}

	monitor.EnableAll()
	for _, eventid := range ids {
		e, ok := monitor.events.lookup(eventid)
		assert.True(t, ok)
		for _, source := range e.sources {
			assert.True(t, source.(*StubEventSource).Enabled)
			assert.Equal(t, 2, source.(*StubEventSource).EnableCount)
		}
	}

	monitor.DisableAll()
	for _, eventid := range ids {
		e, ok := monitor.events.lookup(eventid)
		assert.True(t, ok)
		for _, source := range e.sources {
			assert.False(t, source.(*StubEventSource).Enabled)
			assert.Equal(t, 2, source.(*StubEventSource).DisableCount)
		}
	}

	for _, eventid := range ids {
		monitor.UnregisterEvent(eventid)
	}
	assert.Len(t, monitor.events.getMap(), 0)

	opts := registerEventOptions{
		eventAttr: &EventAttr{},
	}
	_, err = monitor.newRegisteredPerfEvent("non-existent group",
		12345, nil, opts, EventTypeTracepoint, nil, 88, 937854)
	assert.Error(t, err)

	eventType, ok := monitor.RegisteredEventType(234987)
	assert.False(t, ok)
	assert.Equal(t, EventTypeInvalid, eventType)

	fields = monitor.RegisteredEventFields(23434978)
	assert.Nil(t, fields)

	err = monitor.UnregisterEvent(298374)
	assert.Error(t, err)
}

func TestNewRegisteredTraceEvent(t *testing.T) {
	tracingDir, err := createTempTracingDir()
	if tracingDir != "" {
		defer os.RemoveAll(tracingDir)
	}
	assert.NoError(t, err)

	eventDir := filepath.Join(tracingDir, "events", "task", "task_newtask")
	err = os.MkdirAll(eventDir, 0777)
	assert.NoError(t, err)

	formatContent := `name: task_newtask
ID: 109
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:pid_t pid;	offset:8;	size:4;	signed:1;
	field:char comm[16];	offset:12;	size:16;	signed:1;
	field:unsigned long clone_flags;	offset:32;	size:8;	signed:0;
	field:short oom_score_adj;	offset:40;	size:2;	signed:1;

print fmt: "pid=%d comm=%s clone_flags=%lx oom_score_adj=%hd", REC->pid, REC->comm, REC->clone_flags, REC->oom_score_adj`
	formatFile := filepath.Join(eventDir, "format")
	err = ioutil.WriteFile(formatFile, []byte(formatContent), 0666)
	assert.NoError(t, err)

	names := []string{"1", "2", "foo"}
	for _, name := range names {
		probeName := fmt.Sprintf("capsule8/sensor_%d_%s", unix.Getpid(), name)
		eventDir = filepath.Join(tracingDir, "events", probeName)
		err = os.MkdirAll(eventDir, 0777)
		assert.NoError(t, err)

		formatFile = filepath.Join(eventDir, "format")
		err = ioutil.WriteFile(formatFile, []byte(formatContent), 0666)
		assert.NoError(t, err)
	}

	monitor, err := NewEventMonitor(
		WithEventSourceController(NewStubEventSourceController()),
		WithProcFileSystem(newTestProcFileSystem()),
		WithTracingDir(tracingDir))
	require.NoError(t, err)
	defer monitor.Close()

	groupID, err := monitor.RegisterEventGroup("default", nil)
	require.NoError(t, err)

	eventid, err := monitor.newRegisteredTraceEvent("task/task_newtask",
		nil, registerEventOptions{}, EventTypeTracepoint, groupID)
	assert.NoError(t, err)
	assert.Len(t, monitor.events.getMap(), 1)
	e, found := monitor.events.lookup(eventid)
	assert.True(t, found)
	assert.Equal(t, eventid, e.id)
	assert.Equal(t, EventTypeTracepoint, e.eventType)

	err = monitor.UnregisterEvent(eventid)
	assert.NoError(t, err)
	assert.Len(t, monitor.events.getMap(), 0)

	eventid, err = monitor.RegisterTracepoint("task/task_newtask", nil, groupID)
	assert.NoError(t, err)
	assert.Len(t, monitor.events.getMap(), 1)
	e, found = monitor.events.lookup(eventid)
	assert.True(t, found)
	assert.Equal(t, eventid, e.id)
	assert.Equal(t, EventTypeTracepoint, e.eventType)

	err = monitor.UnregisterEvent(eventid)
	assert.NoError(t, err)
	assert.Len(t, monitor.events.getMap(), 0)

	eventid, err = monitor.RegisterKprobe("address", false, "output", nil, groupID)
	assert.NoError(t, err)
	assert.Len(t, monitor.events.getMap(), 1)
	e, found = monitor.events.lookup(eventid)
	assert.True(t, found)
	assert.Equal(t, eventid, e.id)
	assert.Equal(t, EventTypeKprobe, e.eventType)

	err = monitor.UnregisterEvent(eventid)
	assert.NoError(t, err)
	assert.Len(t, monitor.events.getMap(), 0)

	eventid, err = monitor.RegisterKprobe("address", false, "output", nil, groupID,
		WithTracingEventName("foo"))
	assert.NoError(t, err)
	assert.Len(t, monitor.events.getMap(), 1)
	e, found = monitor.events.lookup(eventid)
	assert.True(t, found)
	assert.Equal(t, eventid, e.id)
	assert.Equal(t, EventTypeKprobe, e.eventType)

	err = monitor.UnregisterEvent(eventid)
	assert.NoError(t, err)
	assert.Len(t, monitor.events.getMap(), 0)

	eventid, err = monitor.RegisterUprobe("testdata/uprobe_test",
		"some_function", false, "string=+0(%di):string", nil, groupID)
	assert.NoError(t, err)
	assert.Len(t, monitor.events.getMap(), 1)
	e, found = monitor.events.lookup(eventid)
	assert.True(t, found)
	assert.Equal(t, eventid, e.id)
	assert.Equal(t, EventTypeUprobe, e.eventType)

	err = monitor.UnregisterEvent(eventid)
	assert.NoError(t, err)
	assert.Len(t, monitor.events.getMap(), 0)

	eventid, err = monitor.RegisterUprobe("testdata/uprobe_test",
		"some_function", false, "string=+0(%di):string", nil, groupID,
		WithTracingEventName("foo"))
	assert.NoError(t, err)
	assert.Len(t, monitor.events.getMap(), 1)
	e, found = monitor.events.lookup(eventid)
	assert.True(t, found)
	assert.Equal(t, eventid, e.id)
	assert.Equal(t, EventTypeUprobe, e.eventType)

	err = monitor.UnregisterEvent(eventid)
	assert.NoError(t, err)
	assert.Len(t, monitor.events.getMap(), 0)
}

func TestRegisterCounterEventGroup(t *testing.T) {
	monitor, err := NewEventMonitor(
		WithEventSourceController(NewStubEventSourceController()),
		WithProcFileSystem(newTestProcFileSystem()),
		WithTracingDir("testdata"))
	assert.NoError(t, err)
	defer monitor.Close()

	_, _, err = monitor.RegisterCounterEventGroup("name", nil, nil, nil)
	assert.Error(t, err)

	badMember := CounterEventGroupMember{EventType: EventType(23094572345)}
	_, _, err = monitor.RegisterCounterEventGroup("name",
		[]CounterEventGroupMember{badMember}, nil, nil)
	assert.Error(t, err)

	var counters []CounterEventGroupMember
	_, _, err = monitor.RegisterCounterEventGroup("name", counters, nil, nil)
	assert.Error(t, err)

	counters = append(counters,
		CounterEventGroupMember{EventType: EventTypeHardware})
	counters = append(counters,
		CounterEventGroupMember{EventType: EventTypeHardwareCache})
	counters = append(counters,
		CounterEventGroupMember{EventType: EventTypeSoftware})

	_, _, err = monitor.RegisterCounterEventGroup("name", counters, nil, nil,
		WithFilter("bad filter"))
	assert.Error(t, err)

	groupid, eventid, err := monitor.RegisterCounterEventGroup("name",
		counters, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, int32(0), groupid)
	assert.Equal(t, uint64(1), eventid)
	assert.Len(t, monitor.events.getMap(), 3)
	assert.Len(t, monitor.groups, 1)
}

func TestMonitorRunStop(t *testing.T) {
	monitor, err := NewEventMonitor(
		WithEventSourceController(NewStubEventSourceController()),
		WithProcFileSystem(newTestProcFileSystem()),
		WithTracingDir("testdata"))
	assert.NoError(t, err)
	defer monitor.Close()

	go monitor.Run()
	time.Sleep(200 * time.Millisecond)

	err = monitor.Run()
	assert.Error(t, err)
}

func TestSampleMerger(t *testing.T) {
	samples := [][]Sample{
		[]Sample{
			Sample{SampleID: SampleID{Time: 10}},
			Sample{SampleID: SampleID{Time: 20}},
			Sample{SampleID: SampleID{Time: 30}},
		},
		[]Sample{
			Sample{SampleID: SampleID{Time: 15}},
			Sample{SampleID: SampleID{Time: 25}},
		},
		[]Sample{
			Sample{SampleID: SampleID{Time: 40}},
			Sample{SampleID: SampleID{Time: 50}},
		},
		[]Sample{
			Sample{SampleID: SampleID{Time: 12}},
			Sample{SampleID: SampleID{Time: 42}},
		},
	}
	monitor := &EventMonitor{}
	merger := monitor.newSampleMerger(samples)

	expected := []uint64{10, 12, 15, 20, 25, 30, 40, 42, 50}
	got := make([]uint64, 0, len(expected))
	for {
		if sample, done := merger.next(); !done {
			got = append(got, sample.Time)
		} else {
			break
		}
	}

	assert.Equal(t, expected, got)
}

func TestIndicesFreeList(t *testing.T) {
	monitor := &EventMonitor{
		defaultCacheSize: 4,
	}

	// Initially the free list is empty, should get new allocation. The
	// code is written to return different capacities depending on
	// defaultCacheSize, but there's really no right or wrong here. This
	// test is simply for coverage and to ensure that if a change is made,
	// it is made consciously, because this test will have to be updated.
	i := monitor.acquireIndices(2, 2)
	assert.Equal(t, 2, len(i))
	assert.Equal(t, monitor.defaultCacheSize, cap(i))

	j := monitor.acquireIndices(6, 6)
	assert.Equal(t, 6, len(j))
	assert.Equal(t, 16, cap(j))

	// Release in reverse order so that we have the bigger one in index
	// position 0 and the smaller in position 1
	monitor.releaseIndices(j)
	monitor.releaseIndices(i)

	// This acquisition should return the same as j above
	x := monitor.acquireIndices(5, 6)
	assert.Equal(t, uintptr(unsafe.Pointer(&j[0])), uintptr(unsafe.Pointer(&x[0])))
	assert.Equal(t, 5, len(x))
	assert.Equal(t, 16, cap(x))

	// This acquisition should return the same as i above
	y := monitor.acquireIndices(3, 4)
	assert.Equal(t, uintptr(unsafe.Pointer(&i[0])), uintptr(unsafe.Pointer(&y[0])))
	assert.Equal(t, 3, len(y))
	assert.Equal(t, monitor.defaultCacheSize, cap(y))
}

func TestCountSamples(t *testing.T) {
	esb := eventSourceBuffer{
		buffer: []byte{
			0x01, 0x00, 0x00, 0x00, 0x99, 0x99, 0x08, 0x00,
			0x02, 0x00, 0x00, 0x00, 0x99, 0x99, 0x0A, 0x00, 0x88, 0x88,
			0x03, 0x00, 0x00, 0x00, 0x99, 0x99,
		},
		size: 24,
	}
	n := esb.countSamples()
	assert.Equal(t, 2, n)

	/* invalid! n == 0 */
	esb = eventSourceBuffer{
		buffer: []byte{
			0x01, 0x00, 0x00, 0x00, 0x99, 0x99, 0x00, 0x00,
			0x02, 0x00, 0x00, 0x00, 0x99, 0x99,
		},
		size: 14,
	}
	n = esb.countSamples()
	assert.Equal(t, 0, n)

	/* invalid! n == 1 */
	esb = eventSourceBuffer{
		buffer: []byte{
			0x01, 0x00, 0x00, 0x00, 0x99, 0x99, 0x08, 0x00,
			0x02, 0x00, 0x00, 0x00, 0x99, 0x99, 0x11, 0x22,
			0x03, 0x00, 0x00, 0x00, 0x99, 0x99, 0x08, 0x00,
		},
		size: 24,
	}
	n = esb.countSamples()
	assert.Equal(t, 1, n)
}

func TestBufferLists(t *testing.T) {
	monitor := &EventMonitor{
		defaultCacheSize:   4,
		ringBufferNumPages: 2,
	}

	// Initially the free list is empty, should get new allocation. The
	// code is written to return different capacities depending on
	// defaultCacheSize, but there's really no right or wrong here. This
	// test is simply for coverage and to ensure that if a change is made,
	// it is made consciously, because this test will have to be updated.
	monitor.setupNewBufferList(2)
	assert.Nil(t, monitor.currentBuffer)
	i := monitor.bufferList
	assert.Zero(t, len(i))
	assert.Equal(t, monitor.defaultCacheSize, cap(i))

	monitor.setupNewBufferList(6)
	assert.Nil(t, monitor.currentBuffer)
	j := monitor.bufferList
	assert.Zero(t, len(j))
	assert.Equal(t, 16, cap(j))

	pageSize := os.Getpagesize()
	halfBuffer := monitor.ringBufferNumPages / 2 * pageSize
	fullBuffer := monitor.ringBufferNumPages * pageSize

	monitor.bufferFreeList = make([][]byte, 0, monitor.defaultCacheSize)
	monitor.bufferFreeList = append(monitor.bufferFreeList,
		make([]byte, fullBuffer))

	var buffers [][]byte

	data, offset := monitor.acquireBuffer(halfBuffer)
	if assert.NotNil(t, data) {
		buffers = append(buffers, data)
		assert.Len(t, monitor.bufferList, 1)
		assert.Equal(t, fullBuffer, cap(data))
		assert.Equal(t, halfBuffer, len(data))
		assert.Equal(t, 0, offset)
		if assert.NotNil(t, monitor.currentBuffer) {
			assert.Equal(t,
				uintptr(unsafe.Pointer(&monitor.currentBuffer[0])),
				uintptr(unsafe.Pointer(&data[0])))
		}
	}

	data, offset = monitor.acquireBuffer(halfBuffer)
	if assert.NotNil(t, data) {
		buffers = append(buffers, data)
		assert.Equal(t,
			uintptr(unsafe.Pointer(&buffers[1][0])),
			uintptr(unsafe.Pointer(&buffers[0][0])))

		assert.Len(t, monitor.bufferList, 2)
		assert.Equal(t, cap(data), len(data))
		assert.Equal(t, halfBuffer, offset)
		if assert.NotNil(t, monitor.currentBuffer) {
			assert.Equal(t,
				uintptr(unsafe.Pointer(&monitor.currentBuffer[0])),
				uintptr(unsafe.Pointer(&data[0])))
		}
	}

	data, offset = monitor.acquireBuffer(fullBuffer)
	if assert.NotNil(t, data) {
		buffers = append(buffers, data)
		assert.NotEqual(t,
			uintptr(unsafe.Pointer(&buffers[2][0])),
			uintptr(unsafe.Pointer(&buffers[1][0])))
		assert.Len(t, monitor.bufferList, 3)
		assert.Equal(t, cap(data), len(data))
		assert.Equal(t, 0, offset)
		if assert.NotNil(t, monitor.currentBuffer) {
			assert.Equal(t,
				uintptr(unsafe.Pointer(&monitor.currentBuffer[0])),
				uintptr(unsafe.Pointer(&data[0])))
		}
	}

	// Release in reverse order so that we have the bigger one in index
	// position 0 and the smaller in position 1
	monitor.releaseBufferList(monitor.bufferList)
	monitor.releaseBufferList(i)

	// Verify that the buffer freelist is what we expect it to be, which is
	// 2. The two half buffers should be released as one buffer and the
	// full buffer should be released as well.
	assert.Len(t, monitor.bufferFreeList, 2)

	// This acquisition should return the same as j above
	monitor.setupNewBufferList(6)
	assert.Nil(t, monitor.currentBuffer)
	x := monitor.bufferList
	assert.Zero(t, len(x))
	assert.Equal(t, 16, cap(x))
	j = append(j, eventSourceBuffer{}) // append so we can verify
	x = append(x, eventSourceBuffer{}) // append so we can verify
	assert.Equal(t, uintptr(unsafe.Pointer(&j[0])), uintptr(unsafe.Pointer(&x[0])))

	// This acquisition should return the same as i above
	monitor.setupNewBufferList(4)
	assert.Nil(t, monitor.currentBuffer)
	y := monitor.bufferList
	assert.Zero(t, len(y))
	assert.Equal(t, monitor.defaultCacheSize, cap(y))
	i = append(i, eventSourceBuffer{}) // append so we can verify
	y = append(y, eventSourceBuffer{}) // append so we can verify
	assert.Equal(t, uintptr(unsafe.Pointer(&i[0])), uintptr(unsafe.Pointer(&y[0])))
}

func TestBufferListQueueing(t *testing.T) {
	e, err := newDefaultTimedEvent()
	require.NoError(t, err)

	monitor := &EventMonitor{
		handlerEvent: e,
	}
	monitor.handlingSamples.Store(false)

	list := monitor.dequeueBufferList(0)
	assert.Nil(t, list)

	monitor.bufferList = append(monitor.bufferList, eventSourceBuffer{})
	monitor.enqueueBufferList()

	list = monitor.dequeueBufferList(0)
	assert.NotNil(t, list)

	list = monitor.dequeueBufferList(0)
	assert.Nil(t, list)

	monitor.bufferList = append(monitor.bufferList, eventSourceBuffer{})
	monitor.enqueueBufferList()

	monitor.bufferList = append(monitor.bufferList, eventSourceBuffer{})
	monitor.enqueueBufferList()

	list = monitor.dequeueBufferList(0)
	assert.NotNil(t, list)
	list = monitor.dequeueBufferList(0)
	assert.NotNil(t, list)
	list = monitor.dequeueBufferList(0)
	assert.Nil(t, list)
}

func TestSampleBatchFreeList(t *testing.T) {
	monitor := &EventMonitor{
		defaultCacheSize: 4,
	}

	// Initially the free list is empty, should get new allocation. The
	// code is written to return different capacities depending on
	// defaultCacheSize, but there's really no right or wrong here. This
	// test is simply for coverage and to ensure that if a change is made,
	// it is made consciously, because this test will have to be updated.
	i := monitor.acquireSampleBatch(2, 2)
	assert.Equal(t, 2, len(i))
	assert.Equal(t, monitor.defaultCacheSize, cap(i))

	j := monitor.acquireSampleBatch(6, 6)
	assert.Equal(t, 6, len(j))
	assert.Equal(t, 16, cap(j))

	// Release in reverse order so that we have the bigger one in index
	// position 0 and the smaller in position 1
	monitor.releaseSampleBatch(j)
	assert.Len(t, monitor.sampleBatchFreeList, 1)
	monitor.releaseSampleBatch(i)
	assert.Len(t, monitor.sampleBatchFreeList, 2)

	// This acquisition should return the same as j above
	x := monitor.acquireSampleBatch(5, 6)
	assert.Len(t, monitor.sampleBatchFreeList, 1)
	assert.Equal(t, uintptr(unsafe.Pointer(&j[0])), uintptr(unsafe.Pointer(&x[0])))
	assert.Equal(t, 5, len(x))
	assert.Equal(t, 16, cap(x))

	// This acquisition should return the same as i above
	y := monitor.acquireSampleBatch(3, 4)
	assert.Len(t, monitor.sampleBatchFreeList, 0)
	assert.Equal(t, uintptr(unsafe.Pointer(&i[0])), uintptr(unsafe.Pointer(&y[0])))
	assert.Equal(t, 3, len(y))
	assert.Equal(t, monitor.defaultCacheSize, cap(y))
}

func TestSampleLists(t *testing.T) {
	monitor := &EventMonitor{}

	i := monitor.acquireSampleList(4, 7)
	assert.Equal(t, 8, cap(i))
	assert.Len(t, i, 4)
	monitor.releaseSampleList(i)

	j := monitor.acquireSampleList(2, 6)
	assert.Equal(t, 8, cap(j))
	assert.Len(t, j, 2)
	assert.Equal(t,
		uintptr(unsafe.Pointer(&i[0])),
		uintptr(unsafe.Pointer(&j[0])))
	monitor.releaseSampleList(j)
}

func TestReadSamples(t *testing.T) {
	b := eventSourceBuffer{
		buffer: []byte{
			0x09, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, // eventHeader
			0x44, 0x33, 0x22, 0x11, 0x00, 0x00, 0x00, 0x00, // SampleID
			0x11, 0x22, 0x11, 0x22, 0x11, 0x22, 0x11, 0x22, // Time
			0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // CPU
		},
		size: 0x20,
	}

	formatMap := map[uint64]*EventAttr{
		0x11223344: &EventAttr{
			SampleIDAll: true,
			SampleType:  PERF_SAMPLE_IDENTIFIER | PERF_SAMPLE_TIME | PERF_SAMPLE_CPU,
		},
	}

	HaveClockID = false
	TimeOffsets = []int64{0x1122112211221122, 0x2211221122112211}
	TimeBase = 0x1122112211221122

	monitor := &EventMonitor{}
	samples := monitor.readSamples(b, formatMap)
	require.NotNil(t, samples)
	require.Len(t, samples, 1)

	sample := samples[0]
	assert.Equal(t, uint32(1), sample.CPU)
	assert.Equal(t, uint64(0x1122112211221122), sample.Time)
}

func TestHandleSamples(t *testing.T) {
	monitor, err := NewEventMonitor(
		WithEventSourceController(NewStubEventSourceController()),
		WithProcFileSystem(newTestProcFileSystem()),
		WithTracingDir("testdata"))
	require.NoError(t, err)
	defer monitor.Close()

	groupID, err := monitor.RegisterEventGroup("default", nil)
	require.NoError(t, err)

	var gotTimes []uint64
	eventid, err := monitor.RegisterTracepoint("valid/valid2",
		func(_ uint64, sample *Sample) {
			gotTimes = append(gotTimes, sample.Time)
		}, groupID)
	assert.NoError(t, err)
	event, ok := monitor.events.lookup(eventid)
	assert.True(t, ok)
	assert.Len(t, event.sources, 2)

	dyingStreamID := uint64(298347928374)
	dyingEventID := uint64(327465827346)
	m := uint64Map{
		dyingStreamID: dyingEventID,
	}
	monitor.eventIDMap.updateInPlace(m)

	// NumCPU is 2, so for the first source start with time 200. For the
	// second, use a time before and after so that pendingSamples code
	// paths are covered.

	rawData := []byte{
		0x4e, 0x00, // common_type
		0x00,                   // common_flags
		0x00,                   // common_preempt_count
		0x11, 0x22, 0x33, 0x44, // common_pid
		0x12, 0x34, 0x56, 0x78, // pid
	}

	samples := [][]Sample{
		[]Sample{
			Sample{
				Type:     PERF_RECORD_SAMPLE,
				SampleID: SampleID{Time: 200, StreamID: event.sources[0].SourceID()},
				RawData:  rawData,
			},
			// This sample is intended to be invalid (invalid StreamID)
			Sample{
				Type:     PERF_RECORD_SAMPLE,
				SampleID: SampleID{Time: 250, StreamID: 0},
			},
		},
		[]Sample{
			Sample{
				Type:     PERF_RECORD_SAMPLE,
				SampleID: SampleID{Time: 100, StreamID: event.sources[1].SourceID()},
				RawData:  rawData,
			},
			Sample{
				Type:     PERF_RECORD_SAMPLE,
				SampleID: SampleID{Time: 300, StreamID: event.sources[1].SourceID()},
				RawData:  rawData,
			},
			// This sample is intended to be partially invalid (invalid eventid)
			Sample{
				Type:     PERF_RECORD_SAMPLE,
				SampleID: SampleID{Time: 500, StreamID: dyingStreamID},
				RawData:  rawData,
			},
		},
	}

	// Use flushPendingSamples here instead of handleSamples directly.
	// Kill two birds with one stone since flushPendingSamples is basically
	// just a wrapper around handleSamples.
	monitor.pendingSamples = samples
	monitor.flushPendingSamples()

	expectedTimes := []uint64{100, 200, 300}
	assert.Equal(t, expectedTimes, gotTimes)

	// Prevent EventMonitor.Close from panicking due to our faulty addition
	// of a partial streamid/eventid pair
	monitor.eventIDMap.removeInPlace([]uint64{dyingStreamID})
}

func TestDyingGroups(t *testing.T) {
	c := NewStubEventSourceController()
	monitor := &EventMonitor{
		eventSourceController: c,
	}

	var leaders []*perfGroupLeader
	for i := 0; i < 4; i++ {
		source, err := c.NewEventSourceLeader(EventAttr{}, -1, i, 0)
		require.NoError(t, err)
		leader := &perfGroupLeader{
			source: source,
			state:  perfGroupLeaderStateClosing,
		}
		leaders = append(leaders, leader)
	}
	monitor.groupLeaders.update(leaders)

	assert.Len(t, monitor.groupLeaders.getMap(), 4)
	monitor.checkDyingGroups(true)
	assert.Len(t, monitor.groupLeaders.getMap(), 4)

	monitor.dyingGroups = append(monitor.dyingGroups,
		&eventMonitorGroup{
			leaders: leaders,
		})
	monitor.checkDyingGroups(false)
	assert.Len(t, monitor.groupLeaders.getMap(), 0)
}
