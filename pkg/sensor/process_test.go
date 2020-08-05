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

package sensor

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/sys/unix"
)

func TestNewCredentials(t *testing.T) {
	assert.Exactly(t, &rootCredentials, newCredentials(0, 0, 0, 0, 0, 0, 0, 0))

	expected := &Cred{1000, 5000, 2000, 6000, 3000, 7000, 4000, 8000}
	actual := newCredentials(1000, 2000, 3000, 4000, 5000, 6000, 7000, 8000)
	assert.Equal(t, expected, actual)
}

func TestCloneEvent(t *testing.T) {
	var e *cloneEvent

	// A nil cloneEvent should return true to indicate that it is expired.
	assert.True(t, e.isExpired(uint64(sys.CurrentMonotonicRaw())))

	// Because clone related events can come out of order, a cloneEvent is
	// considered expired if its timestamp is +/- the current time by a
	// threshold constant (cloneEventThreshold = 100ms)
	e = &cloneEvent{timestamp: uint64(sys.CurrentMonotonicRaw())}
	assert.False(t, e.isExpired(e.timestamp+cloneEventThreshold/2))
	assert.False(t, e.isExpired(e.timestamp-cloneEventThreshold/2))
	assert.True(t, e.isExpired(e.timestamp+cloneEventThreshold*2))
	assert.True(t, e.isExpired(e.timestamp-cloneEventThreshold*2))
}

func TestTask(t *testing.T) {
	task := newTask(sensorPID)
	task.TGID = task.PID
	assert.True(t, task.IsSensor())

	task = newTask(1467)
	task.TGID = task.PID
	assert.False(t, task.IsSensor())

	parentTask := newTask(1231)
	parentTask.TGID = parentTask.PID
	assert.Equal(t, parentTask, parentTask.Leader())

	task.parent = parentTask
	task.TGID = parentTask.PID
	assert.Equal(t, parentTask, task.Leader())

	// Handling out of order events ... if task.parent is nil, the return
	// should be task. In this case, TGID must also be nil, but we can't
	// test for that, because glog.Fatal cannot be caught
	task.parent = nil
	task.TGID = 0
	assert.Equal(t, task, task.Parent())

	now := sys.CurrentMonotonicRaw()
	task.funkyExecTime = 0
	assert.False(t, task.suppressExitEvent(now))

	task.funkyExecTime = now + int64(10*time.Microsecond)
	assert.True(t, task.suppressExitEvent(now))

	task.funkyExecTime = now - int64(10*time.Microsecond)
	assert.True(t, task.suppressExitEvent(now))
}

const testCacheSize = uint(1024)

func testTaskCacheImplementation(t *testing.T, cache taskCache) {
	tasks := make([]*Task, testCacheSize)
	for i := 0; i < int(testCacheSize); i++ {
		tasks[i] = cache.LookupTask(i + 1)
	}
	for i := 0; i < int(testCacheSize); i++ {
		task := cache.LookupTask(i + 1)
		assert.Exactly(t, tasks[i], task)
		task.ExitTime = sys.CurrentMonotonicRaw() - taskReuseThreshold
	}
	for i := 0; i < int(testCacheSize); i++ {
		task := cache.LookupTask(i + 1)
		assert.NotEqual(t, tasks[i], task)
	}
}

func TestArrayTaskCache(t *testing.T) {
	cache := newArrayTaskCache(testCacheSize)
	testTaskCacheImplementation(t, cache)
}

func TestMapTaskCache(t *testing.T) {
	cache := newMapTaskCache(testCacheSize)
	testTaskCacheImplementation(t, cache)
}

func TestProcessInfoCacheLostRecordHandler(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	var (
		receivedLostEvent bool
		lostEventType     LostRecordType
		lostEventCount    uint64
	)

	s := newTestSubscription(t, sensor)
	_, err := s.addEventSink(sensor.Monitor().ReserveEventID(), nil)
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

	expectedLostType := LostRecordTypeProcess
	expectedLostCount := uint64(398746)
	sensor.ProcessCache.lostRecordHandler(123, 456, perf.SampleID{},
		expectedLostCount)
	cancel()

	if assert.True(t, receivedLostEvent) {
		assert.Equal(t, expectedLostType, lostEventType)
		assert.Equal(t, expectedLostCount, lostEventCount)
	}

	assert.Equal(t, expectedLostCount, sensor.Metrics.KernelSamplesLost)
}

func TestLookupTaskContainerInfo(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	parentTask := sensor.ProcessCache.LookupTask(2835)
	require.NotNil(t, parentTask)
	parentTask.TGID = parentTask.PID

	task := sensor.ProcessCache.LookupTask(2836)
	require.NotNil(t, task)
	task.parent = parentTask
	task.TGID = parentTask.TGID

	info := sensor.ProcessCache.LookupTaskContainerInfo(task)
	assert.Nil(t, info)

	containerID := "98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf"
	ci := sensor.ContainerCache.LookupContainer(containerID, true)
	require.NotNil(t, ci)

	parentTask.ContainerID = containerID

	info = sensor.ProcessCache.LookupTaskContainerInfo(parentTask)
	assert.Equal(t, ci, info)

	info = sensor.ProcessCache.LookupTaskContainerInfo(task)
	assert.Equal(t, ci, info)
}

func TestProcessInfoCache(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	cache := NewProcessInfoCache(sensor)
	require.NotNil(t, cache)

	// Test enqueueing of pending actions
	var executedDeferredAction bool
	cache.maybeDeferAction(func() {
		executedDeferredAction = true
	})
	cache.Start()
	assert.True(t, executedDeferredAction)
}

func TestProcessHandlers(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	sampleID := perf.SampleID{
		Time: uint64(sys.CurrentMonotonicRaw()),
	}
	data := expression.FieldValueMap{
		"filename":          "/bin/bash",
		"exec_command_line": []string{"bash", "-l"},

		"code":             int32(234987),
		"exit_status":      uint32(495678),
		"exit_signal":      uint32(11),
		"exit_core_dumped": true,

		"fork_child_pid":   int32(9485),
		"fork_child_id":    "some string that is a child process id",
		"fork_clone_flags": uint64(9283749),
		"fork_stack_start": uint64(293847),

		"cwd": "/var/run/capsule8",
	}

	type testCase struct {
		eventid      uint64
		dispatch     func(perf.SampleID, expression.FieldValueMap)
		expectedType interface{}
		fieldChecks  map[string]string
	}
	testCases := []testCase{
		testCase{
			eventid:      sensor.ProcessCache.ProcessExecEventID,
			dispatch:     sensor.ProcessCache.dispatchProcessExecEvent,
			expectedType: ProcessExecTelemetryEvent{},
			fieldChecks: map[string]string{
				"filename":          "Filename",
				"exec_command_line": "CommandLine",
				"cwd":               "CWD",
			},
		},
		testCase{
			eventid:      sensor.ProcessCache.ProcessExitEventID,
			dispatch:     sensor.ProcessCache.dispatchProcessExitEvent,
			expectedType: ProcessExitTelemetryEvent{},
			fieldChecks: map[string]string{
				"code":             "ExitCode",
				"exit_status":      "ExitStatus",
				"exit_signal":      "ExitSignal",
				"exit_core_dumped": "ExitCoreDumped",
			},
		},
		testCase{
			eventid:      sensor.ProcessCache.ProcessForkEventID,
			dispatch:     sensor.ProcessCache.dispatchProcessForkEvent,
			expectedType: ProcessForkTelemetryEvent{},
			fieldChecks: map[string]string{
				"fork_child_pid":   "ChildPID",
				"fork_child_id":    "ChildProcessID",
				"fork_clone_flags": "CloneFlags",
				"fork_stack_start": "StackStart",
				"cwd":              "CWD",
			},
		},
		testCase{
			eventid:      sensor.ProcessCache.ProcessUpdateEventID,
			dispatch:     sensor.ProcessCache.dispatchProcessUpdateEvent,
			expectedType: ProcessUpdateTelemetryEvent{},
			fieldChecks: map[string]string{
				"cwd": "CWD",
			},
		},
	}

	for _, tc := range testCases {
		s := newTestSubscription(t, sensor)

		dispatched := false
		s.dispatchFn = func(event TelemetryEvent) {
			e, ok := event.(TelemetryEvent)
			require.True(t, ok)
			require.IsType(t, tc.expectedType, event)

			ok = testCommonTelemetryEventData(t, sensor, e)
			require.True(t, ok)

			value := reflect.ValueOf(event)
			for k, v := range tc.fieldChecks {
				assert.Equal(t, data[k], value.FieldByName(v).Interface())
			}
			dispatched = true
		}
		s.addEventSink(tc.eventid, nil)
		sensor.eventMap.subscribe(s)

		sampleID.TID = uint32(sensorPID)
		tc.dispatch(sampleID, data)
		require.False(t, dispatched)

		sampleID.TID = 0
		tc.dispatch(sampleID, data)
		require.True(t, dispatched)
	}
}

func TestHandleSysClone(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	var (
		forkEvent *ProcessForkTelemetryEvent
		lock      sync.Mutex
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newTestSubscription(t, sensor)
	s.RegisterProcessForkEventFilter(nil)
	status, err := s.Run(ctx, func(event TelemetryEvent) {
		if e, ok := event.(ProcessForkTelemetryEvent); ok {
			lock.Lock()
			forkEvent = &e
			lock.Unlock()
		}
	})
	assert.Len(t, status, 0)
	require.NoError(t, err)

	cache := sensor.ProcessCache
	parentTask := cache.cache.LookupTask(88888)
	parentTask.TGID = parentTask.PID
	parentTask.Command = "/bin/bash"
	parentTask.StartTime = int64(sys.CurrentMonotonicRaw())
	parentTask.CWD = sensor.runtimeDir
	parentTask.Creds = &Cred{500, 500, 500, 500, 500, 500, 500, 500}

	parentLeader := parentTask.Leader()

	childTask := cache.cache.LookupTask(88889)
	cloneFlags := uint64(CLONE_THREAD)
	childComm := "bash"
	sampleID := perf.SampleID{
		Time: uint64(sys.CurrentMonotonicRaw()),
		PID:  uint32(parentTask.PID),
		TID:  uint32(parentTask.PID),
		CPU:  1,
	}
	cache.handleSysClone(parentTask, parentLeader, childTask,
		cloneFlags, childComm, sampleID, 0, 0)

	time.Sleep(100 * time.Millisecond)
	lock.Lock()
	if assert.NotNil(t, forkEvent) {
		// Make sure the fork event contains the right information
		assert.Equal(t, int32(childTask.PID), forkEvent.ChildPID)
		assert.Equal(t, childTask.ProcessID, forkEvent.ChildProcessID)
		forkEvent = nil
	}
	lock.Unlock()
	// Make sure childTask is filled in with the right information
	assert.Equal(t, parentTask.TGID, childTask.TGID)
	assert.Equal(t, childComm, childTask.Command)
	assert.Equal(t, parentTask.Creds, childTask.Creds)
	assert.Equal(t, parentLeader, childTask.parent)

	// Make the child thread fork a new process
	aNewTask := cache.cache.LookupTask(90000)
	cloneFlags = uint64(0)
	sampleID = perf.SampleID{
		Time: uint64(sys.CurrentMonotonicRaw()),
		PID:  uint32(childTask.PID),
		TID:  uint32(childTask.PID),
		CPU:  1,
	}
	startTimeNsec := int64(27456756294835)
	startTimeTicks := startTimeNsec / 1e7
	cache.handleSysClone(childTask, parentLeader, aNewTask, cloneFlags,
		childComm, sampleID, startTimeNsec, 0)

	time.Sleep(100 * time.Millisecond)
	lock.Lock()
	if assert.NotNil(t, forkEvent) {
		// Make sure the fork event contains the right information
		assert.Equal(t, int32(aNewTask.PID), forkEvent.ChildPID)
		assert.Equal(t, aNewTask.ProcessID, forkEvent.ChildProcessID)
		forkEvent = nil
	}
	lock.Unlock()
	// Make sure aNewTask is filled in with the right information
	assert.Equal(t, aNewTask.PID, aNewTask.TGID)
	assert.Equal(t, startTimeTicks, aNewTask.StartTime)
	assert.Equal(t, childComm, aNewTask.Command)
	assert.Equal(t, childTask.Creds, aNewTask.Creds)
	assert.Equal(t, parentLeader, aNewTask.parent)
}

func TestReplaceTaskStructOffsets(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	sensor.taskStructPID = StructField{Offset: 1188, Size: 4}
	sensor.taskStructTGID = StructField{Offset: 1192, Size: 4}
	sensor.taskStructRealStartTime = StructField{Offset: 1460, Size: 16}

	pc := sensor.ProcessCache
	s := pc.replaceTaskStructOffsets("pid=+PID_OFFSET(%cx) tgid=+TGID_OFFSET(%cx) sec=+START_TIME_SEC_OFFSET(%cx) nsec=+START_TIME_NSEC_OFFSET(%cx)")
	assert.Equal(t, "pid=+1188(%cx) tgid=+1192(%cx) sec=+1460(%cx) nsec=+1468(%cx)", s)
}

func TestInstallForkMonitor(t *testing.T) {
	// This is empty for now, but I'm leaving it so that I have somewhere
	// to put this comment ... I could add tests here for the sake of
	// coverage, but there's no way to really test what's going on right
	// now. I've consciously opted to not add tests just to get coverage
	// for the sake of coverage.

	/*
		sensor := newUnitTestSensor(t)
		defer sensor.Stop()

		sensor.taskStructPID = 1188
		sensor.taskStructTGID = 1192
		sensor.taskStructRealStartTime = 1460
		err := sensor.ProcessCache.installForkMonitor()
		require.NoError(t, err)
	*/
}

func TestHandleDoExit(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	var (
		exitEvent *ProcessExitTelemetryEvent
		lock      sync.Mutex
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newTestSubscription(t, sensor)
	s.RegisterProcessExitEventFilter(nil)
	status, err := s.Run(ctx, func(event TelemetryEvent) {
		if e, ok := event.(ProcessExitTelemetryEvent); ok {
			lock.Lock()
			exitEvent = &e
			lock.Unlock()
		}
	})
	assert.Len(t, status, 0)
	require.NoError(t, err)

	type testCase struct {
		exitCode       int32
		exitStatus     uint32
		exitSignal     uint32
		exitCoreDumped bool
	}
	testCases := []testCase{
		testCase{int32(unix.SIGSEGV) | 0x80, 0, uint32(unix.SIGSEGV), true},
		testCase{0xf00, 0xf, 0, false},
	}

	for _, tc := range testCases {
		exitEvent = nil

		task := sensor.ProcessCache.cache.LookupTask(410)
		task.TGID = task.PID

		sample := &perf.Sample{
			SampleID: perf.SampleID{
				PID:  410,
				TID:  410,
				Time: uint64(sys.CurrentMonotonicRaw()),
			},
		}
		data := expression.FieldValueMap{
			"code": int64(tc.exitCode),
		}
		setSampleRawData(sample, data)
		sensor.ProcessCache.handleDoExit(0, sample)

		time.Sleep(100 * time.Millisecond)
		lock.Lock()
		if assert.NotNil(t, exitEvent) {
			assert.Equal(t, tc.exitCode, exitEvent.ExitCode)
			assert.Equal(t, tc.exitStatus, exitEvent.ExitStatus)
			assert.Equal(t, tc.exitSignal, exitEvent.ExitSignal)
			assert.Equal(t, tc.exitCoreDumped, exitEvent.ExitCoreDumped)
		}
		lock.Unlock()
	}

	// Test exit event suppression
	sample := &perf.Sample{
		SampleID: perf.SampleID{
			PID:  410,
			TID:  410,
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	exitEvent = nil
	task := sensor.ProcessCache.cache.LookupTask(410)
	task.TGID = task.PID
	task.funkyExecTime = int64(sample.Time) + int64(10*time.Microsecond)
	sensor.ProcessCache.handleDoExit(0, sample)
	time.Sleep(100 * time.Millisecond)
	lock.Lock()
	assert.Nil(t, exitEvent)           // Make sure event was suppressed
	assert.Zero(t, task.funkyExecTime) // Make sure exec time is reset
	lock.Unlock()
}

func TestHandleCommitCreds(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	expected := &Cred{1111, 2222, 3333, 4444, 5555, 6666, 7777, 8888}

	task := sensor.ProcessCache.cache.LookupTask(410)
	task.TGID = task.PID

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			PID:  410,
			TID:  410,
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"uid":   expected.UID,
		"gid":   expected.GID,
		"euid":  expected.EUID,
		"egid":  expected.EGID,
		"suid":  expected.SUID,
		"sgid":  expected.SGID,
		"fsuid": expected.FSUID,
		"fsgid": expected.FSGID,
	}
	setSampleRawData(sample, data)
	sensor.ProcessCache.handleCommitCreds(0, sample)

	assert.Equal(t, expected, task.Creds)
}

func TestHandleDoSetFsPwd(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	task := sensor.ProcessCache.cache.LookupTask(sensorPID)
	expected := task.CWD
	task.CWD = ""

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			PID:  uint32(sensorPID),
			TID:  uint32(sensorPID),
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	sensor.ProcessCache.handleDoSetFsPwd(0, sample)

	assert.Equal(t, expected, task.CWD)
}

func TestHandleExecve(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	var (
		execEvent *ProcessExecTelemetryEvent
		exitEvent *ProcessExitTelemetryEvent
		lock      sync.Mutex
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newTestSubscription(t, sensor)
	s.RegisterProcessExecEventFilter(nil)
	s.RegisterProcessExitEventFilter(nil)
	status, err := s.Run(ctx, func(event TelemetryEvent) {
		if e, ok := event.(ProcessExecTelemetryEvent); ok {
			lock.Lock()
			execEvent = &e
			lock.Unlock()
		}
		if e, ok := event.(ProcessExitTelemetryEvent); ok {
			lock.Lock()
			exitEvent = &e
			lock.Unlock()
		}
	})
	assert.Len(t, status, 0)
	require.NoError(t, err)

	task := sensor.ProcessCache.cache.LookupTask(410)
	task.TGID = task.PID

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			PID:  410,
			TID:  410,
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"filename": "/bin/ls",
		"argv0":    "ls",
		"argv1":    "-F",
		"argv2":    "/etc",
		"argv3":    "",
		"argv4":    "",
		"argv5":    "",
	}
	setSampleRawData(sample, data)
	sensor.ProcessCache.handleExecve(0, sample)

	time.Sleep(100 * time.Millisecond)
	lock.Lock()
	assert.Nil(t, exitEvent)
	if assert.NotNil(t, execEvent) {
		assert.Equal(t, data["filename"], execEvent.Filename)

		commandLine := []string{"ls", "-F", "/etc"}
		assert.Equal(t, commandLine, execEvent.CommandLine)

		task = sensor.ProcessCache.cache.LookupTask(410)
		assert.Equal(t, commandLine, task.CommandLine)
	}
	lock.Unlock()

	// Test non-tgid pid calling execve
	taskThread := sensor.ProcessCache.cache.LookupTask(412)
	taskThread.parent = task
	taskThread.TGID = task.TGID
	sample.TID = 412
	execEvent = nil
	exitEvent = nil
	sensor.ProcessCache.handleExecve(0, sample)

	time.Sleep(100 * time.Millisecond)
	lock.Lock()
	if assert.NotNil(t, exitEvent) {
		assert.Equal(t, 412, exitEvent.PID)
		assert.Equal(t, 410, exitEvent.TGID)
	}
	if assert.NotNil(t, execEvent) {
		assert.Equal(t, 410, execEvent.PID)
		assert.Equal(t, 410, execEvent.TGID)
	}
}

func TestHandleDoFork(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	var (
		forkEvent *ProcessForkTelemetryEvent
		lock      sync.Mutex
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newTestSubscription(t, sensor)
	s.RegisterProcessForkEventFilter(nil)
	status, err := s.Run(ctx, func(event TelemetryEvent) {
		if e, ok := event.(ProcessForkTelemetryEvent); ok {
			lock.Lock()
			forkEvent = &e
			lock.Unlock()
		}
	})
	assert.Len(t, status, 0)
	require.NoError(t, err)

	task := sensor.ProcessCache.cache.LookupTask(410)
	task.TGID = task.PID

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			PID:  410,
			TID:  410,
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"clone_flags": uint64(29384576245),
	}
	setSampleRawData(sample, data)

	for x := 0; x < 2; x++ {
		sensor.ProcessCache.handleDoFork(0, sample)

		task = sensor.ProcessCache.cache.LookupTask(410)
		if assert.NotNil(t, task.pendingClone) {
			assert.Equal(t, sample.Time, task.pendingClone.timestamp)
			assert.Equal(t, data["clone_flags"], task.pendingClone.cloneFlags)
		}
	}

	task = sensor.ProcessCache.cache.LookupTask(410)
	task.pendingClone.cloneFlags = 0
	task.pendingClone.childPid = 4120

	sensor.ProcessCache.handleDoFork(0, sample)

	time.Sleep(100 * time.Millisecond)

	task = sensor.ProcessCache.cache.LookupTask(410)
	assert.Nil(t, task.pendingClone)

	lock.Lock()
	if assert.NotNil(t, forkEvent) {
		assert.Equal(t, int32(4120), forkEvent.ChildPID)
		assert.NotZero(t, forkEvent.ChildProcessID)
	}
	lock.Unlock()
}

func TestHandleSchedProcessFork(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	var (
		forkEvent *ProcessForkTelemetryEvent
		lock      sync.Mutex
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newTestSubscription(t, sensor)
	s.RegisterProcessForkEventFilter(nil)
	status, err := s.Run(ctx, func(event TelemetryEvent) {
		if e, ok := event.(ProcessForkTelemetryEvent); ok {
			lock.Lock()
			forkEvent = &e
			lock.Unlock()
		}
	})
	assert.Len(t, status, 0)
	require.NoError(t, err)

	task := sensor.ProcessCache.cache.LookupTask(410)
	task.TGID = task.PID

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			PID:  410,
			TID:  410,
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"parent_pid": int32(410),
		"child_pid":  int32(4120),
		"child_comm": commAsBytes,
	}
	setSampleRawData(sample, data)

	sensor.ProcessCache.handleSchedProcessFork(0, sample)

	task = sensor.ProcessCache.cache.LookupTask(410)
	if assert.NotNil(t, task.pendingClone) {
		assert.Equal(t, sample.Time, task.pendingClone.timestamp)
		assert.Equal(t, int(data["child_pid"].(int32)), task.pendingClone.childPid)
	}

	sensor.ProcessCache.handleSchedProcessFork(0, sample)

	time.Sleep(100 * time.Millisecond)

	task = sensor.ProcessCache.cache.LookupTask(410)
	assert.Nil(t, task.pendingClone)

	lock.Lock()
	if assert.NotNil(t, forkEvent) {
		assert.Equal(t, int32(4120), forkEvent.ChildPID)
		assert.NotZero(t, forkEvent.ChildProcessID)
	}
	lock.Unlock()
}

func TestHandleWakeUpNewTask(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	var (
		forkEvent *ProcessForkTelemetryEvent
		lock      sync.Mutex
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newTestSubscription(t, sensor)
	s.RegisterProcessForkEventFilter(nil)
	status, err := s.Run(ctx, func(event TelemetryEvent) {
		if e, ok := event.(ProcessForkTelemetryEvent); ok {
			lock.Lock()
			forkEvent = &e
			lock.Unlock()
		}
	})
	assert.Len(t, status, 0)
	require.NoError(t, err)

	task := sensor.ProcessCache.cache.LookupTask(410)
	task.TGID = task.PID

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			PID:  410,
			TID:  410,
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"child_pid":       int32(4120),
		"start_time_sec":  int64(9832476),
		"start_time_nsec": int64(2938745),
	}
	setSampleRawData(sample, data)

	sensor.ProcessCache.handleWakeUpNewTask(0, sample)

	task = sensor.ProcessCache.cache.LookupTask(410)
	if assert.NotNil(t, task.pendingClone) {
		assert.Equal(t, sample.Time, task.pendingClone.timestamp)
		assert.Equal(t, int(data["child_pid"].(int32)), task.pendingClone.childPid)
	}

	sensor.ProcessCache.handleWakeUpNewTask(0, sample)

	time.Sleep(100 * time.Millisecond)

	task = sensor.ProcessCache.cache.LookupTask(410)
	assert.Nil(t, task.pendingClone)

	lock.Lock()
	if assert.NotNil(t, forkEvent) {
		assert.Equal(t, int32(4120), forkEvent.ChildPID)
		assert.NotZero(t, forkEvent.ChildProcessID)
	}
	lock.Unlock()
}

func TestHandleCgroupProcsWrite(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	parentTask := sensor.ProcessCache.cache.LookupTask(410)
	parentTask.TGID = parentTask.PID

	childTask := sensor.ProcessCache.cache.LookupTask(4120)
	childTask.TGID = parentTask.PID
	childTask.parent = parentTask

	invalidContainerID := "cgroup name that isn't a container name"
	validContainerID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"container_id": invalidContainerID,
	}
	setSampleRawData(sample, data)
	sensor.ProcessCache.handleCgroupProcsWrite(0, sample)

	type testCase struct {
		data expression.FieldValueMap
		pid  int
	}
	testCases := []testCase{
		testCase{
			data: expression.FieldValueMap{
				"buf":         "4120",
				"threadgroup": int32(1),
			},
			pid: 410,
		},
		testCase{
			data: expression.FieldValueMap{
				"buf":         "4120",
				"threadgroup": int32(0),
			},
			pid: 4120,
		},
		testCase{
			data: expression.FieldValueMap{
				"tgid": uint64(4120),
			},
			pid: 410,
		},
		testCase{
			data: expression.FieldValueMap{
				"pid": uint64(4120),
			},
			pid: 4120,
		},
	}
	for _, tc := range testCases {
		task, leader := sensor.ProcessCache.LookupTaskAndLeader(4120)
		task.ContainerID = ""
		leader.ContainerID = ""

		tc.data["container_id"] = validContainerID
		setSampleRawData(sample, tc.data)

		sensor.ProcessCache.handleCgroupProcsWrite(0, sample)

		task = sensor.ProcessCache.cache.LookupTask(tc.pid)
		assert.Equal(t, validContainerID, task.ContainerID)
	}

}

var commAsBytes = []int8{'w', 'h', 'a', 't', 'e', 'v', 'e', 'r', 0, 0, 0, 0, 0, 0, 0, 0}
var commAsBytes2 = []uint8{'w', 'h', 'a', 't', 'e', 'v', 'e', 'r', 0, 0, 0, 0, 0, 0, 0, 0}
var commAsBytes3 = []uint8{'w', 'h', 'a', 't', 'e', 'v', 'e', 'r'}

func TestCommToString(t *testing.T) {
	s := commToString(commAsBytes)
	assert.Equal(t, "whatever", s)

	s = commToString(commAsBytes2)
	assert.Equal(t, "whatever", s)

	s = commToString(commAsBytes3)
	assert.Equal(t, "whatever", s)
}

func verifyProcessEventRegistration(t *testing.T, s *Subscription, count int) {
	if count > 0 {
		assert.Len(t, s.eventSinks, count)
	} else {
		assert.Len(t, s.status, -count)
		assert.Len(t, s.eventSinks, 0)
	}
}

func TestProcessEventRegistration(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	names := []string{
		"RegisterProcessExecEventFilter",
		"RegisterProcessExitEventFilter",
		"RegisterProcessForkEventFilter",
		"RegisterProcessUpdateEventFilter",
	}
	for _, name := range names {
		s := newTestSubscription(t, sensor)
		v := reflect.ValueOf(s)
		m := v.MethodByName(name)

		var nilExpr *expression.Expression
		m.Call([]reflect.Value{reflect.ValueOf(nilExpr)})
		verifyProcessEventRegistration(t, s, 1)
	}
}
