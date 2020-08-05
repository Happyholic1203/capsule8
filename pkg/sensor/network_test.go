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
	"reflect"
	"testing"

	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/sys/unix"
)

func TestNetworkHandlers(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"fd":             uint64(258675234),
		"ret":            int64(8927364),
		"backlog":        uint64(24576),
		"sin_addr":       uint32(0x7f000001),
		"sin_port":       uint16(0x1f90),
		"sin6_addr_high": uint64(0x1122334455667788),
		"sin6_addr_low":  uint64(0x9900aabbccddeeff),
		"sin6_port":      uint16(0x8080),
		"sun_path":       "/path/to/local.socket",
	}
	families := []uint16{unix.AF_INET, unix.AF_INET6, unix.AF_LOCAL}

	type testCase struct {
		handler      perf.TraceEventHandlerFn
		expectedType interface{}
		fieldChecks  map[string]string
	}
	testCases := []testCase{
		testCase{
			handler:      s.handleSysEnterAccept,
			expectedType: NetworkAcceptAttemptTelemetryEvent{},
			fieldChecks: map[string]string{
				"fd": "FD",
			},
		},
		testCase{
			handler:      s.handleSysExitAccept,
			expectedType: NetworkAcceptResultTelemetryEvent{},
			fieldChecks: map[string]string{
				"ret": "Return",
			},
		},
		// 3x bind to catch all three address families
		testCase{
			handler:      s.handleSysBind,
			expectedType: NetworkBindAttemptTelemetryEvent{},
			fieldChecks: map[string]string{
				"fd":        "FD",
				"sa_family": "Family",
			},
		},
		testCase{
			handler:      s.handleSysBind,
			expectedType: NetworkBindAttemptTelemetryEvent{},
			fieldChecks: map[string]string{
				"fd":        "FD",
				"sa_family": "Family",
			},
		},
		testCase{
			handler:      s.handleSysBind,
			expectedType: NetworkBindAttemptTelemetryEvent{},
			fieldChecks: map[string]string{
				"fd":        "FD",
				"sa_family": "Family",
			},
		},
		testCase{
			handler:      s.handleSysExitBind,
			expectedType: NetworkBindResultTelemetryEvent{},
			fieldChecks: map[string]string{
				"ret": "Return",
			},
		},
		// 3x connect to catch all three address families
		testCase{
			handler:      s.handleSysConnect,
			expectedType: NetworkConnectAttemptTelemetryEvent{},
			fieldChecks: map[string]string{
				"fd":        "FD",
				"sa_family": "Family",
			},
		},
		testCase{
			handler:      s.handleSysConnect,
			expectedType: NetworkConnectAttemptTelemetryEvent{},
			fieldChecks: map[string]string{
				"fd":        "FD",
				"sa_family": "Family",
			},
		},
		testCase{
			handler:      s.handleSysConnect,
			expectedType: NetworkConnectAttemptTelemetryEvent{},
			fieldChecks: map[string]string{
				"fd":        "FD",
				"sa_family": "Family",
			},
		},
		testCase{
			handler:      s.handleSysExitConnect,
			expectedType: NetworkConnectResultTelemetryEvent{},
			fieldChecks: map[string]string{
				"ret": "Return",
			},
		},
		testCase{
			handler:      s.handleSysEnterListen,
			expectedType: NetworkListenAttemptTelemetryEvent{},
			fieldChecks: map[string]string{
				"fd":      "FD",
				"backlog": "Backlog",
			},
		},
		testCase{
			handler:      s.handleSysExitListen,
			expectedType: NetworkListenResultTelemetryEvent{},
			fieldChecks: map[string]string{
				"ret": "Return",
			},
		},
		testCase{
			handler:      s.handleSysEnterRecvfrom,
			expectedType: NetworkRecvfromAttemptTelemetryEvent{},
			fieldChecks: map[string]string{
				"fd": "FD",
			},
		},
		testCase{
			handler:      s.handleSysExitRecvfrom,
			expectedType: NetworkRecvfromResultTelemetryEvent{},
			fieldChecks: map[string]string{
				"ret": "Return",
			},
		},

		// 3x sendto to catch all three address families
		testCase{
			handler:      s.handleSysSendto,
			expectedType: NetworkSendtoAttemptTelemetryEvent{},
			fieldChecks: map[string]string{
				"fd":        "FD",
				"sa_family": "Family",
			},
		},
		testCase{
			handler:      s.handleSysSendto,
			expectedType: NetworkSendtoAttemptTelemetryEvent{},
			fieldChecks: map[string]string{
				"fd":        "FD",
				"sa_family": "Family",
			},
		},
		testCase{
			handler:      s.handleSysSendto,
			expectedType: NetworkSendtoAttemptTelemetryEvent{},
			fieldChecks: map[string]string{
				"fd":        "FD",
				"sa_family": "Family",
			},
		},
		testCase{
			handler:      s.handleSysExitSendto,
			expectedType: NetworkSendtoResultTelemetryEvent{},
			fieldChecks: map[string]string{
				"ret": "Return",
			},
		},
	}

	for x, tc := range testCases {
		dispatched := false
		s.dispatchFn = func(event TelemetryEvent) {
			e, ok := event.(TelemetryEvent)
			require.True(t, ok)
			require.IsType(t, tc.expectedType, e)

			ok = testCommonTelemetryEventData(t, sensor, e)
			require.True(t, ok)

			value := reflect.ValueOf(e)
			for k, v := range tc.fieldChecks {
				assert.Equal(t, data[k], value.FieldByName(v).Interface())
			}
			if _, ok = tc.fieldChecks["sa_family"]; ok {
				switch data["sa_family"] {
				case unix.AF_INET:
					assert.Equal(t, data["sin_addr"], value.FieldByName("IPv4Address").Interface())
					assert.Equal(t, data["sin_port"], value.FieldByName("IPv4Port").Interface())
				case unix.AF_INET6:
					assert.Equal(t, data["sin6_addr_high"], value.FieldByName("IPv6AddressHigh").Interface())
					assert.Equal(t, data["sin6_addr_low"], value.FieldByName("IPv6AddressLow").Interface())
					assert.Equal(t, data["sin6_port"], value.FieldByName("IPv6Port").Interface())
				case unix.AF_LOCAL:
					assert.Equal(t, data["sun_path"], value.FieldByName("UnixPath").Interface())
				}
			}
			dispatched = true
		}

		data["sa_family"] = families[x%len(families)]
		setSampleRawData(sample, data)
		eventid, _ := s.addTestEventSink(t, nil)

		sample.TID = uint32(sensorPID)
		tc.handler(eventid, sample)
		require.False(t, dispatched)

		sample.TID = 0
		tc.handler(eventid, sample)
		require.True(t, dispatched)
	}
}

const networkKprobeFormat = `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:int fd;	offset:8;	size:4;	signed:1;
	field:u16 sa_family;	offset:12;	size:2;	signed:0;
	field:u16 sin_port;	offset:14;	size:2;	signed:0;
	field:u32 sin_addr;	offset:16;	size:4;	signed:0;
	field:__data_loc char[] sun_path;	offset:20;	size:4;	signed:1;
	field:u16 sin6_port;	offset:22;	size:2;	signed:0;
	field:u64 sin6_addr_high;	offset:24;	size:8;	signed:0;
	field:u64 sin6_addr_low;	offset:32;	size:8;	signed:0;

print fmt: "fd=%d sa_family=%d sin_port=%d sin_addr=%d sun_path=\"%s\" sin6_port=%d sin6_addr_high=%d sin6_addr_low=%d", REC->fd, REC->sa_family, REC->sin_port, REC->sin_addr, __get_str(sun_path), REC->sin6_port, REC->sin6_addr_high, REC->sin6_addr_low`

func prepareForRegisterNetworkBindAttemptEventFilter(t *testing.T, s *Subscription, delta uint64) {
	newUnitTestKprobe(t, s.sensor, delta, networkKprobeFormat)
}

func prepareForRegisterNetworkConnectAttemptEventFilter(t *testing.T, s *Subscription, delta uint64) {
	newUnitTestKprobe(t, s.sensor, delta, networkKprobeFormat)
}

func prepareForRegisterNetworkSendtoAttemptEventFilter(t *testing.T, s *Subscription, delta uint64) {
	newUnitTestKprobe(t, s.sensor, delta, networkKprobeFormat)
	newUnitTestKprobe(t, s.sensor, delta+1, networkKprobeFormat)
}

func verifyNetworkEventRegistration(t *testing.T, s *Subscription, name string, count int) {
	if count > 0 {
		assert.Len(t, s.eventSinks, count, name)
	} else {
		assert.Len(t, s.status, -count, name)
		assert.Len(t, s.eventSinks, 0, name)
	}
}

func TestNetworkEventRegistration(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	type testCase struct {
		name    string
		prepare func(*testing.T, *Subscription, uint64)
		count   int
	}
	testCases := []testCase{
		testCase{"RegisterNetworkAcceptAttemptEventFilter", nil, 2},
		testCase{"RegisterNetworkAcceptResultEventFilter", nil, 2},
		testCase{"RegisterNetworkBindAttemptEventFilter", prepareForRegisterNetworkBindAttemptEventFilter, 1},
		testCase{"RegisterNetworkBindResultEventFilter", nil, 1},
		testCase{"RegisterNetworkConnectAttemptEventFilter", prepareForRegisterNetworkConnectAttemptEventFilter, 1},
		testCase{"RegisterNetworkConnectResultEventFilter", nil, 1},
		testCase{"RegisterNetworkListenAttemptEventFilter", nil, 1},
		testCase{"RegisterNetworkListenResultEventFilter", nil, 1},
		testCase{"RegisterNetworkRecvfromAttemptEventFilter", nil, 2},
		testCase{"RegisterNetworkRecvfromResultEventFilter", nil, 2},
		testCase{"RegisterNetworkSendtoAttemptEventFilter", prepareForRegisterNetworkSendtoAttemptEventFilter, 2},
		testCase{"RegisterNetworkSendtoResultEventFilter", nil, 2},
	}
	for _, tc := range testCases {
		s := newTestSubscription(t, sensor)
		v := reflect.ValueOf(s)
		m := v.MethodByName(tc.name)

		if tc.prepare != nil {
			tc.prepare(t, s, 0)
		}
		var nilExpr *expression.Expression
		m.Call([]reflect.Value{reflect.ValueOf(nilExpr)})
		verifyNetworkEventRegistration(t, s, tc.name, tc.count)
	}
}
