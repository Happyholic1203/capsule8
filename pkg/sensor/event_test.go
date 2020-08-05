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

	"github.com/Happyholic1203/capsule8/pkg/sys/perf"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCommonTelemetryEventData(
	t *testing.T,
	sensor *Sensor,
	e TelemetryEvent,
) bool {
	data := e.CommonTelemetryEventData()

	ok1 := assert.Len(t, data.EventID, 64)
	ok2 := assert.Equal(t, sensor.ID, data.SensorID)
	ok3 := assert.Condition(t, func() bool { return data.MonotimeNanos > 0 },
		"MonotimeNanos is %d", data.MonotimeNanos)
	ok4 := assert.NotZero(t, data.SequenceNumber)

	return ok1 && ok2 && ok3 && ok4
}

type testTelemetryEvent struct {
	TelemetryEventData
}

func (e testTelemetryEvent) CommonTelemetryEventData() TelemetryEventData {
	return e.TelemetryEventData
}

func TestTelemetryEventDataInit(t *testing.T) {
	sensor := newUnitTestSensor(t)

	var e testTelemetryEvent
	e.Init(sensor)
	testCommonTelemetryEventData(t, sensor, e)
	assert.Equal(t, sensor.Metrics.Events, e.SequenceNumber)
}

func TestTelemetryEventDataInitWithSample(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			ID:       923584,
			PID:      12839,
			TID:      12839,
			Time:     928347529 + uint64(sensor.bootMonotimeNanos),
			CPU:      2,
			StreamID: 827634,
		},
		IP:     982734,
		Addr:   2389047,
		Period: 98276345,
	}

	task := sensor.ProcessCache.LookupTask(12839)
	task.TGID = 12839
	task.Creds = newCredentials(1000, 1001, 1002, 1003, 1004, 1005, 1006, 1007)

	for x := 0; x < 2; x++ {
		var e testTelemetryEvent
		ok := e.InitWithSample(sensor, sample)
		require.True(t, ok)
		testCommonTelemetryEventData(t, sensor, e)
		assert.Equal(t, task.ProcessID, e.ProcessID)
		assert.Equal(t, task.PID, e.PID)
		assert.Equal(t, task.TGID, e.TGID)
		assert.Equal(t, sample.CPU, e.CPU)
		if assert.True(t, e.HasCredentials) {
			assert.Equal(t, *task.Creds, e.Credentials)
		}
	}
}
