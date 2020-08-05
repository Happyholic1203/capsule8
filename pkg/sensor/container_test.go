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
	"testing"

	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/sys/unix"
)

func TestContainerHandlers(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	sampleID := perf.SampleID{
		Time: uint64(sys.CurrentMonotonicRaw()),
	}
	info := ContainerInfo{
		ID:         "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ./",
		Name:       "capsule8-sensor-container",
		ImageID:    "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz./",
		ImageName:  "capsule8-sensor-image",
		Pid:        872364,
		ExitCode:   255,
		Runtime:    ContainerRuntimeDocker,
		State:      ContainerStateRunning,
		JSONConfig: "This is the JSON config that isn't actually JSON",
		OCIConfig:  "This is the OCI config that isn't real",
	}

	type testCase struct {
		eventid      uint64
		dispatch     func(perf.SampleID, *ContainerInfo)
		expectedType interface{}
	}
	testCases := []testCase{
		testCase{
			eventid:      sensor.ContainerCache.ContainerCreatedEventID,
			dispatch:     sensor.ContainerCache.dispatchContainerCreatedEvent,
			expectedType: ContainerCreatedTelemetryEvent{},
		},
		testCase{
			eventid:      sensor.ContainerCache.ContainerDestroyedEventID,
			dispatch:     sensor.ContainerCache.dispatchContainerDestroyedEvent,
			expectedType: ContainerDestroyedTelemetryEvent{},
		},
		testCase{
			eventid:      sensor.ContainerCache.ContainerExitedEventID,
			dispatch:     sensor.ContainerCache.dispatchContainerExitedEvent,
			expectedType: ContainerExitedTelemetryEvent{},
		},
		testCase{
			eventid:      sensor.ContainerCache.ContainerRunningEventID,
			dispatch:     sensor.ContainerCache.dispatchContainerRunningEvent,
			expectedType: ContainerRunningTelemetryEvent{},
		},
		testCase{
			eventid:      sensor.ContainerCache.ContainerUpdatedEventID,
			dispatch:     sensor.ContainerCache.dispatchContainerUpdatedEvent,
			expectedType: ContainerUpdatedTelemetryEvent{},
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

			cted := e.CommonTelemetryEventData()
			assert.Equal(t, info, cted.Container)
			dispatched = true
		}

		s.addEventSink(tc.eventid, nil)
		sensor.eventMap.subscribe(s)

		tc.dispatch(sampleID, &info)
		require.True(t, dispatched)
	}
}

func TestContainerCache(t *testing.T) {
	const id = "b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c"

	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	cache := sensor.ContainerCache

	// Lookup non-existant
	info := cache.LookupContainer(id, false)
	assert.Nil(t, info)

	// Lookup && create
	info = cache.LookupContainer(id, true)
	assert.NotNil(t, info)

	info2 := cache.LookupContainer(id, false)
	assert.Equal(t, info, info2)

	// Delete and verify
	sampleID := perf.SampleID{Time: uint64(sys.CurrentMonotonicRaw())}
	cache.DeleteContainer(id, ContainerRuntimeDocker, sampleID)

	info = cache.LookupContainer(id, false)
	assert.NotNil(t, info)

	cache.DeleteContainer(id, ContainerRuntimeUnknown, sampleID)

	info = cache.LookupContainer(id, false)
	assert.Nil(t, info)

	// Lookup && create again
	info = cache.LookupContainer(id, true)
	assert.NotNil(t, info)

	info2 = cache.LookupContainer(id, false)
	assert.Equal(t, info, info2)

	// Update
	changes := map[string]interface{}{
		"foo":   "this field does not exist",
		"State": ContainerStateExited,
	}
	sampleID.Time = uint64(sys.CurrentMonotonicRaw())
	info.Update(cache, ContainerRuntimeDocker, sampleID, changes)

	info = cache.LookupContainer(id, false)
	assert.Equal(t, ContainerRuntimeDocker, info.Runtime)
	assert.Equal(t, ContainerStateExited, info.State)

	changes = map[string]interface{}{
		"Name":     "capsule8-sensor",
		"Pid":      int(3874),
		"ExitCode": int(unix.SIGSEGV) | 0x80,
	}
	sampleID.Time = uint64(sys.CurrentMonotonicRaw())
	info.Update(cache, ContainerRuntimeDocker, sampleID, changes)

	assert.Equal(t, "capsule8-sensor", info.Name)
	assert.Equal(t, int(3874), info.Pid)
	assert.Equal(t, int(unix.SIGSEGV)|0x80, info.ExitCode)

	changes = map[string]interface{}{
		"State": ContainerStateRunning,
	}
	info.Update(cache, ContainerRuntimeUnknown, sampleID, changes)
	assert.Equal(t, ContainerStateExited, info.State)
}

func TestContainerCacheLostRecordHandler(t *testing.T) {
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

	expectedLostType := LostRecordTypeContainer
	expectedLostCount := uint64(238746)
	sensor.ContainerCache.lostRecordHandler(123, 456, perf.SampleID{},
		expectedLostCount)
	cancel()

	if assert.True(t, receivedLostEvent) {
		assert.Equal(t, expectedLostType, lostEventType)
		assert.Equal(t, expectedLostCount, lostEventCount)
	}

	assert.Equal(t, expectedLostCount, sensor.Metrics.KernelSamplesLost)
}

func verifyContainerEventRegistration(t *testing.T, s *Subscription, count int) {
	if count > 0 {
		assert.Len(t, s.eventSinks, count)
	} else {
		assert.Len(t, s.status, -count)
		assert.Len(t, s.eventSinks, 0)
	}
}

func TestContainerEventRegistration(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	names := []string{
		"RegisterContainerCreatedEventFilter",
		"RegisterContainerRunningEventFilter",
		"RegisterContainerExitedEventFilter",
		"RegisterContainerDestroyedEventFilter",
		"RegisterContainerUpdatedEventFilter",
	}
	for _, name := range names {
		s := newTestSubscription(t, sensor)
		v := reflect.ValueOf(s)
		m := v.MethodByName(name)

		var nilExpr *expression.Expression
		m.Call([]reflect.Value{reflect.ValueOf(nilExpr)})
		verifyContainerEventRegistration(t, s, 1)
	}
}

func TestContainerFilterLen(t *testing.T) {
	cf := ContainerFilter{}
	assert.Equal(t, 0, cf.Len())

	cf.AddContainerID("abc")
	assert.Equal(t, 1, cf.Len())

	cf.AddContainerName("abc")
	assert.Equal(t, 2, cf.Len())

	cf.AddImageID("abc")
	assert.Equal(t, 3, cf.Len())

	err := cf.AddImageName("*abc*")
	if assert.NoError(t, err) {
		assert.Equal(t, 4, cf.Len())
	}
	err = cf.AddImageName("*abc*")
	if assert.NoError(t, err) {
		assert.Equal(t, 4, cf.Len())
	}

	err = cf.AddImageName("*.[ch")
	assert.Error(t, err)
}

func TestContainerMatch(t *testing.T) {
	var cf *ContainerFilter
	info := ContainerInfo{}

	// A nil ContainerFilter should always match
	m := cf.Match(info)
	assert.True(t, m)

	// An empty ContainerInfo should never match
	cf = &ContainerFilter{}
	m = cf.Match(info)
	assert.False(t, m)
}

func TestFilterContainerId(t *testing.T) {
	cf := NewContainerFilter()
	cf.AddContainerID("alice")
	cf.AddContainerID("bob")

	pass := ContainerInfo{
		ID: "alice",
	}
	assert.True(t, cf.Match(pass))

	fail := ContainerInfo{
		ID: "bill",
	}
	assert.False(t, cf.Match(fail))
}

func TestFilterContainerImageId(t *testing.T) {
	cf := NewContainerFilter()
	cf.AddImageID("alice")
	cf.AddImageID("bob")

	pass := ContainerInfo{
		ID:      "pass",
		ImageID: "alice",
	}
	assert.True(t, cf.Match(pass))

	fail := ContainerInfo{
		ID:      "fail",
		ImageID: "bill",
	}
	assert.False(t, cf.Match(fail))
}

func TestFilterContainerImageNames(t *testing.T) {
	cf := NewContainerFilter()
	cf.AddImageName("alice")
	cf.AddImageName("bob")

	pass := ContainerInfo{
		ID:        "pass",
		ImageName: "alice",
	}
	assert.True(t, cf.Match(pass))

	fail := ContainerInfo{
		ID:        "fail",
		ImageName: "bill",
	}
	assert.False(t, cf.Match(fail))
}

func TestFilterContainerNames(t *testing.T) {
	cf := NewContainerFilter()
	cf.AddContainerName("alice")
	cf.AddContainerName("bob")

	pass := ContainerInfo{
		ID:   "pass",
		Name: "alice",
	}
	assert.True(t, cf.Match(pass))

	fail := ContainerInfo{
		ID:   "fail",
		Name: "bill",
	}
	assert.False(t, cf.Match(fail))
}
