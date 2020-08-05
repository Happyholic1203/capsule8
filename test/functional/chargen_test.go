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

package functional

import (
	"testing"

	telemetryAPI "github.com/Happyholic1203/capsule8/api/v0"
)

const chargenLength = 40
const chargenEventCount = 32

type chargenTest struct {
	count int
}

func (ct *chargenTest) BuildContainer(t *testing.T) string {
	// No container is needed for testing, nothing to do.
	return ""
}

func (ct *chargenTest) RunContainer(t *testing.T) {
	// No container is needed for testing, nothing to do.
}

func (ct *chargenTest) CreateSubscription(t *testing.T) *telemetryAPI.Subscription {
	chargenEvents := []*telemetryAPI.ChargenEventFilter{
		&telemetryAPI.ChargenEventFilter{
			Length: chargenLength,
		},
	}

	eventFilter := &telemetryAPI.EventFilter{
		ChargenEvents: chargenEvents,
	}

	return &telemetryAPI.Subscription{
		EventFilter: eventFilter,
	}
}

func (ct *chargenTest) HandleTelemetryEvent(t *testing.T, te *telemetryAPI.ReceivedTelemetryEvent) bool {
	switch event := te.Event.Event.(type) {
	case *telemetryAPI.TelemetryEvent_Chargen:
		if len(event.Chargen.Characters) != chargenLength {
			t.Errorf("Event %#v has the wrong number of characters.\n", *event.Chargen)
			return false
		}

		ct.count++
		return ct.count < chargenEventCount

	default:
		t.Errorf("Unexpected event type %T\n", event)
		return false
	}
}

//
// TestChargen checks that with a subscription including a ChargenEvents filter,
// the sensor will generate appropriate Chargen events.
//
func TestChargen(t *testing.T) {

	ct := &chargenTest{}

	tt := NewTelemetryTester(ct)
	tt.RunTest(t)
}
