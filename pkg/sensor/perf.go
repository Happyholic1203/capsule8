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
	"fmt"

	"github.com/Happyholic1203/capsule8/pkg/sys/perf"
)

// PerformanceTelemetryEvent is a telemetry event generated by the performance
// event source.
type PerformanceTelemetryEvent struct {
	TelemetryEventData

	TotalTimeEnabled uint64
	TotalTimeRunning uint64
	Counters         []perf.CounterEventValue
}

// CommonTelemetryEventData returns the telemtry event data common to all
// telemetry events for a chargen telemetry event.
func (e PerformanceTelemetryEvent) CommonTelemetryEventData() TelemetryEventData {
	return e.TelemetryEventData
}

func (s *Subscription) handlePerfCounterEvent(
	eventid uint64,
	sample *perf.Sample,
	counters []perf.CounterEventValue,
	totalTimeEnabled uint64,
	totalTimeRunning uint64,
) {
	var e PerformanceTelemetryEvent
	if e.InitWithSample(s.sensor, sample) {
		e.TotalTimeEnabled = totalTimeEnabled
		e.TotalTimeRunning = totalTimeRunning
		e.Counters = counters
		s.DispatchEvent(eventid, e, nil)
	}
}

// RegisterPerformanceEventFilter registers a performance event filter with a
// subscription.
func (s *Subscription) RegisterPerformanceEventFilter(
	attr perf.EventAttr,
	counters []perf.CounterEventGroupMember,
) {
	eventName := "Performance Counters"
	groupID, eventID, err := s.sensor.Monitor().RegisterCounterEventGroup(
		eventName, counters, s.handlePerfCounterEvent, s.lostRecordHandler,
		perf.WithEventAttr(&attr))
	if err != nil {
		s.logStatus(
			fmt.Sprintf("Could not register %s performance event: %v",
				eventName, err))
	} else {
		s.counterGroupIDs = append(s.counterGroupIDs, groupID)
		s.addEventSink(eventID, nil)
	}
}
