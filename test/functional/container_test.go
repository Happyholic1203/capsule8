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
	"github.com/golang/glog"
)

type containerTest struct {
	testContainer *Container
	containerID   string
	seenEvts      map[telemetryAPI.ContainerEventType]bool
}

func newContainerTest() *containerTest {
	return &containerTest{seenEvts: make(map[telemetryAPI.ContainerEventType]bool)}
}

func (ct *containerTest) BuildContainer(t *testing.T) string {
	c := NewContainer(t, "container")
	err := c.Build()
	if err != nil {
		t.Error(err)
	} else {
		glog.V(2).Infof("Built container %s\n", c.ImageID[0:12])
		ct.testContainer = c
	}

	return ct.testContainer.ImageID
}

func (ct *containerTest) RunContainer(t *testing.T) {
	err := ct.testContainer.Run()
	if err != nil {
		t.Error(err)
	}
	glog.V(2).Infof("Running container %s\n", ct.testContainer.ImageID[0:12])
}

func (ct *containerTest) CreateSubscription(t *testing.T) *telemetryAPI.Subscription {
	containerEvents := []*telemetryAPI.ContainerEventFilter{
		&telemetryAPI.ContainerEventFilter{
			Type: telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_CREATED,
		},
		&telemetryAPI.ContainerEventFilter{
			Type: telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_RUNNING,
		},
		&telemetryAPI.ContainerEventFilter{
			Type: telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_EXITED,
		},
		&telemetryAPI.ContainerEventFilter{
			Type: telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_DESTROYED,
		},
	}

	eventFilter := &telemetryAPI.EventFilter{
		ContainerEvents: containerEvents,
	}

	return &telemetryAPI.Subscription{
		EventFilter: eventFilter,
	}
}

func (ct *containerTest) HandleTelemetryEvent(t *testing.T, te *telemetryAPI.ReceivedTelemetryEvent) bool {
	switch event := te.Event.Event.(type) {
	case *telemetryAPI.TelemetryEvent_Container:
		switch event.Container.Type {
		case telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_CREATED:
			if event.Container.ImageId == ct.testContainer.ImageID {
				if ct.containerID != "" {
					t.Errorf("Already seen container event %s", event.Container.Type)
				}
				ct.containerID = te.Event.ContainerId
				ct.seenEvts[event.Container.Type] = true

				glog.V(1).Infof("Found container %s",
					ct.containerID)
			}

		case telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_RUNNING,
			telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_EXITED,
			telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_DESTROYED:
			if ct.containerID != "" && te.Event.ContainerId == ct.containerID {
				if ct.seenEvts[event.Container.Type] {
					t.Errorf("Already saw container event type %v",
						event.Container.Type)
				}

				ct.seenEvts[event.Container.Type] = true

				glog.V(1).Infof("Got container event %s for %s",
					event.Container.Type, ct.containerID)
			}

		}

		return len(ct.seenEvts) < 4

	default:
		t.Errorf("Unexpected event type %T\n", event)
		return false
	}
}

//
// TestContainer checks that the sensor generates container events requested by
// the subscription.
//
func TestContainer(t *testing.T) {
	ct := newContainerTest()

	tt := NewTelemetryTester(ct)
	tt.RunTest(t)
}
