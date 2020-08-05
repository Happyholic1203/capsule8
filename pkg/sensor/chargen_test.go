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
	"testing"
	"time"

	"github.com/Happyholic1203/capsule8/pkg/expression"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatchChargenEvent(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	s := newTestSubscription(t, sensor)

	data := expression.FieldValueMap{
		"index":      uint64(928734),
		"characters": "character string",
	}

	dispatched := false
	s.dispatchFn = func(event TelemetryEvent) {
		e, ok := event.(ChargenTelemetryEvent)
		require.True(t, ok)

		ok = testCommonTelemetryEventData(t, sensor, e)
		require.True(t, ok)
		assert.Equal(t, data["index"], e.Index)
		assert.Equal(t, data["characters"], e.Characters)
		dispatched = true
	}

	eventid, _ := s.addTestEventSink(t, nil)
	s.dispatchChargenEvent(eventid, data)
	require.True(t, dispatched)
}

func TestGenerateCharacters(t *testing.T) {
	type testCase struct {
		start, length uint64
		result        string
	}

	testCases := []testCase{
		{0, 5, " !\"#$"},
		{33, 10, "ABCDEFGHIJ"},
		{65, 4, "abcd"},
		{90, 7, "z{|}~ !"},
	}

	for _, tc := range testCases {
		s := generateCharacters(tc.start, tc.length)
		assert.Equal(t, tc.result, s)
	}
}

func verifyRegisterChargenEventFilter(t *testing.T, s *Subscription, count int) {
	if count > 0 {
		assert.Len(t, s.eventSinks, count)
	} else {
		assert.Len(t, s.status, -count)
		assert.Len(t, s.eventSinks, 0)
	}
}

func TestRegisterChargenEventFilter(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	// Invalid length should fail
	s := newTestSubscription(t, sensor)
	s.RegisterChargenEventFilter(0, nil)
	verifyRegisterChargenEventFilter(t, s, -1)

	s = newTestSubscription(t, sensor)
	s.RegisterChargenEventFilter(1<<16+1, nil)
	verifyRegisterChargenEventFilter(t, s, -1)

	// This should succeed
	s = newTestSubscription(t, sensor)
	s.RegisterChargenEventFilter(32, nil)
	verifyRegisterChargenEventFilter(t, s, 1)

	ctx, cancel := context.WithCancel(context.Background())
	s.Run(ctx, nil)
	time.Sleep(200 * time.Millisecond)
	cancel()
}
