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

	"github.com/Happyholic1203/capsule8/pkg/sys"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlePerfCounterEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	counters := []perf.CounterEventValue{
		perf.CounterEventValue{
			EventType: perf.EventTypeHardware,
			Config:    239478,
			Value:     19823452,
		},
		perf.CounterEventValue{
			EventType: perf.EventTypeHardwareCache,
			Config:    984567,
			Value:     5678398457,
		},
		perf.CounterEventValue{
			EventType: perf.EventTypeSoftware,
			Config:    398457,
			Value:     7867568,
		},
	}

	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(PerformanceTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, uint64(293847), e.TotalTimeEnabled)
		assert.Equal(t, uint64(2340978), e.TotalTimeRunning)
		assert.Equal(t, counters, e.Counters)
		dispatched = true
	}

	eventid, _ := s.addTestEventSink(t, nil)
	s.handlePerfCounterEvent(eventid, sample, counters, 293847, 2340978)
	require.True(t, dispatched)
}

func verifyRegisterPerformanceEventFilter(t *testing.T, s *Subscription, count int) {
	if count > 0 {
		assert.Len(t, s.eventSinks, count)
	} else {
		assert.Len(t, s.status, -count)
		assert.Len(t, s.eventSinks, 0)
	}
}

func TestRegisterPerformanceEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	attr := perf.EventAttr{}
	s := newTestSubscription(t, sensor)
	s.RegisterPerformanceEventFilter(attr, nil)
	verifyRegisterPerformanceEventFilter(t, s, -1)

	counters := []perf.CounterEventGroupMember{
		perf.CounterEventGroupMember{
			EventType: perf.EventTypeHardware,
			Config:    983745,
		},
	}
	s = newTestSubscription(t, sensor)
	s.RegisterPerformanceEventFilter(attr, counters)
	verifyRegisterPerformanceEventFilter(t, s, 1)
}
