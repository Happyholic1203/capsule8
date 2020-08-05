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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleUprobe(t *testing.T) {
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
		"bytes":  []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
		"string": "string_value",
		"sint8":  int8(-8),
		"sint16": int16(-16),
		"sint32": int32(-32),
		"sint64": int64(-64),
		"uint8":  uint8(8),
		"uint16": uint16(16),
		"uint32": uint32(32),
		"uint64": uint64(64),
	}
	setSampleRawData(sample, data)

	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(UserFunctionCallTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, data, e.Arguments)
		dispatched = true
	}

	eventid, _ := s.addTestEventSink(t, nil)
	s.handleUprobe(eventid, sample)
	require.False(t, dispatched)

	sample.TID = 0
	s.handleUprobe(eventid, sample)
	require.True(t, dispatched)
}

func prepareForRegisterUserFunctionCallEventFilter(t *testing.T, s *Subscription) {
	format := `name: ^^NAME^^
id: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:__data_loc char[] s;	offset:16;	size:4;	signed:1;

print fmt: "s=\"%s\"", __get_str(s)`

	newUnitTestUprobe(t, s.sensor, 0, format)
}

func verifyRegisterUserFunctionCallEventFilter(t *testing.T, s *Subscription, count int) {
	if count > 0 {
		assert.Len(t, s.eventSinks, count)
	} else {
		assert.Len(t, s.status, -count)
		assert.Len(t, s.eventSinks, 0)
	}
}

func TestRegisterUserFunctionCallEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)
	s.RegisterUserFunctionCallEventFilter("/bin/bash", "0x83764287", false, nil, nil)
	verifyRegisterUserFunctionCallEventFilter(t, s, -1)

	e := expression.Equal(expression.Identifier("foo"), expression.Value("bar"))
	expr, err := expression.ConvertExpression(e, nil)
	require.NotNil(t, expr)
	require.NoError(t, err)

	executable := "/bin/bash"
	symbol := "readline"
	arguments := map[string]string{
		"s": "+0($retval):string",
	}

	s = newTestSubscription(t, sensor)
	prepareForRegisterUserFunctionCallEventFilter(t, s)
	s.RegisterUserFunctionCallEventFilter(executable, symbol, true, arguments, expr)
	verifyRegisterUserFunctionCallEventFilter(t, s, -1)

	s = newTestSubscription(t, sensor)
	prepareForRegisterUserFunctionCallEventFilter(t, s)
	s.RegisterUserFunctionCallEventFilter(executable, symbol, true, arguments, nil)
	verifyRegisterUserFunctionCallEventFilter(t, s, 1)
}
