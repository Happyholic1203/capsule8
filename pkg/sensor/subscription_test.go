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
	"fmt"
	"testing"

	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSubscription(t *testing.T, sensor *Sensor) *Subscription {
	s := sensor.NewSubscription()
	require.NotNil(t, s)

	return s
}

func (s *Subscription) addTestEventSink(
	t *testing.T,
	filterExpression *expression.Expression,
) (uint64, *eventSink) {
	eventid := s.sensor.Monitor().ReserveEventID()
	es, err := s.addEventSink(eventid, filterExpression)
	require.NoError(t, err)
	require.NotNil(t, es)

	return eventid, es
}

func dumpSubscriptionStatus(s *Subscription) {
	for _, msg := range s.status {
		fmt.Printf("Status: %s\n", msg)
	}
}

func TestSubscriptionLostRecordHandler(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	var gotLostCount uint64
	expectedLostCount := uint64(298374239847)

	expectedTime := int64(29837452)
	sampleID := perf.SampleID{
		Time: uint64(expectedTime + sensor.bootMonotimeNanos),
		CPU:  23,
	}

	s := newTestSubscription(t, sensor)
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(LostRecordTelemetryEvent)
		if assert.True(t, ok) {
			assert.Equal(t, expectedTime, e.MonotimeNanos)
			assert.Equal(t, uint32(23), e.CPU)
			gotLostCount += e.Lost
		}
	}

	eventid, _ := s.addTestEventSink(t, nil)
	sensor.eventMap.subscribe(s)

	s.lostRecordHandler(eventid, 12345, sampleID, expectedLostCount)

	assert.Equal(t, expectedLostCount, gotLostCount)
	assert.Equal(t, expectedLostCount, sensor.Metrics.KernelSamplesLost)
}

func TestSafeEventSinkMap(t *testing.T) {
	sesm := newSafeEventSinkMap()
	require.NotNil(t, sesm)

	m := sesm.getMap()
	assert.Nil(t, m)

	s1 := &Subscription{
		subscriptionID: 1,
		eventGroupID:   888,
		eventSinks: map[uint64]*eventSink{
			1: &eventSink{eventID: 1},
			2: &eventSink{eventID: 2},
			3: &eventSink{eventID: 3},
		},
	}

	sesm.subscribe(s1)
	m = sesm.getMap()
	require.NotNil(t, m)
	assert.Len(t, m, 3)
	for eventid, eventSinks := range m {
		assert.Lenf(t, eventSinks, 1, "eventid %d", eventid)
	}

	s2 := &Subscription{
		subscriptionID: 2,
		eventGroupID:   8888,
		eventSinks: map[uint64]*eventSink{
			1: &eventSink{eventID: 1},
			2: &eventSink{eventID: 2},
			3: &eventSink{eventID: 3},
		},
	}
	sesm.subscribe(s2)
	m = sesm.getMap()
	require.NotNil(t, m)
	assert.Len(t, m, 3)
	for eventid, eventSinks := range m {
		assert.Lenf(t, eventSinks, 2, "eventid %d", eventid)
	}

	expect := map[uint64]bool{
		1: true, 2: true, 3: true,
	}
	got := make(map[uint64]bool)
	sesm.unsubscribe(s1, func(eventid uint64) {
		got[eventid] = true
	})
	// got should be 0 here, because the callback is only called when the
	// eventid is completely removed. s2's subscription still holds all of
	// the same ones.
	assert.Len(t, got, 0)
	m = sesm.getMap()
	require.NotNil(t, m)
	assert.Len(t, m, 3)
	for eventid, eventSinks := range m {
		assert.Lenf(t, eventSinks, 1, "eventid %d", eventid)
	}

	got = make(map[uint64]bool)
	unreg := make(map[uint64]bool)
	for _, es := range s2.eventSinks {
		es.unregister = func(e *eventSink) {
			unreg[e.eventID] = true
		}
	}
	sesm.unsubscribe(s2, func(eventid uint64) {
		got[eventid] = true
	})
	// got should now have all three eventids
	assert.Equal(t, expect, got)
	// unreg should also have all three eventids
	assert.Equal(t, expect, unreg)
	m = sesm.getMap()
	require.NotNil(t, m)
	assert.Len(t, m, 0)
}

func TestSafeSubscriptionMap(t *testing.T) {
	ssm := newSafeSubscriptionMap()
	require.NotNil(t, ssm)

	m := ssm.getMap()
	require.Nil(t, m)

	s1 := &Subscription{subscriptionID: 1}
	s2 := &Subscription{subscriptionID: 2}

	ssm.insert(s1)
	m = ssm.getMap()
	require.NotNil(t, m)
	assert.Len(t, m, 1)

	ssm.insert(s2)
	m = ssm.getMap()
	require.NotNil(t, m)
	assert.Len(t, m, 2)

	ssm.remove(s2)
	m = ssm.getMap()
	require.NotNil(t, m)
	assert.Len(t, m, 1)

	ssm.remove(s1)
	m = ssm.getMap()
	require.NotNil(t, m)
	assert.Len(t, m, 0)
}
