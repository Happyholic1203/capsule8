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

type signalTest struct {
	testContainer   *Container
	err             error
	containerID     string
	containerExited bool
	processID       string
	processExited   bool
}

func (ct *signalTest) BuildContainer(t *testing.T) string {
	c := NewContainer(t, "signal")
	err := c.Build()
	if err != nil {
		t.Error(err)
		return ""
	}

	ct.testContainer = c
	return ct.testContainer.ImageID
}

func (ct *signalTest) RunContainer(t *testing.T) {
	err := ct.testContainer.Start()
	if err != nil {
		t.Error(err)
		return
	}

	// We assume that the container will return an error, so ignore that one
	ct.testContainer.Wait()
}

func (ct *signalTest) CreateSubscription(t *testing.T) *telemetryAPI.Subscription {
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
	}

	processEvents := []*telemetryAPI.ProcessEventFilter{
		&telemetryAPI.ProcessEventFilter{
			Type: telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_FORK,
		},

		&telemetryAPI.ProcessEventFilter{
			Type: telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_EXEC,
		},

		&telemetryAPI.ProcessEventFilter{
			Type: telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_EXIT,
		},
	}

	eventFilter := &telemetryAPI.EventFilter{
		ContainerEvents: containerEvents,
		ProcessEvents:   processEvents,
	}

	sub := &telemetryAPI.Subscription{
		EventFilter: eventFilter,
	}

	return sub
}

func (ct *signalTest) HandleTelemetryEvent(t *testing.T, telemetryEvent *telemetryAPI.ReceivedTelemetryEvent) bool {
	switch event := telemetryEvent.Event.Event.(type) {
	case *telemetryAPI.TelemetryEvent_Container:
		if event.Container.Type == telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_CREATED {
			if event.Container.ImageId == ct.testContainer.ImageID {
				if len(ct.containerID) > 0 {
					t.Error("Already saw container created")
					return false
				}

				ct.containerID = telemetryEvent.Event.ContainerId
				glog.V(1).Infof("containerID = %s", ct.containerID)
			}
		} else if event.Container.Type == telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_EXITED &&
			len(ct.containerID) > 0 &&
			telemetryEvent.Event.ContainerId == ct.containerID {

			if event.Container.ExitCode != 138 {
				t.Errorf("Expected ExitCode %d, got %d",
					138, event.Container.ExitCode)
				return false
			}

			ct.containerExited = true
			glog.V(1).Infof("containerExited = true")
		}

	case *telemetryAPI.TelemetryEvent_Process:
		if event.Process.Type == telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_EXEC {
			if event.Process.ExecFilename == "/main" &&
				telemetryEvent.Event.ContainerId == ct.containerID {
				if len(ct.processID) > 0 {
					t.Error("Already saw process exec")
					return false
				}

				ct.processID = telemetryEvent.Event.ProcessId
				glog.V(1).Infof("processID = %s", ct.processID)
			}
		} else if len(ct.processID) > 0 &&
			telemetryEvent.Event.ProcessId == ct.processID &&
			event.Process.Type == telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_EXIT {

			if event.Process.ExitSignal != 10 {
				t.Errorf("Expected ExitSignal == %d, got %d",
					10, event.Process.ExitSignal)
				return false
			}

			if event.Process.ExitCoreDumped != false {
				t.Errorf("Expected ExitCoreDumped %v, got %v",
					false, event.Process.ExitCoreDumped)
				return false
			}

			if event.Process.ExitStatus != 0 {
				t.Errorf("Expected ExitStatus %d, got %d",
					0, event.Process.ExitStatus)
				return false
			}

			ct.processExited = true
			glog.V(1).Infof("processExited = true")
		}
	}

	return !(ct.containerExited && ct.processExited)
}

func TestSignal(t *testing.T) {
	// Skip this test for now. Something in the CircleCI environment
	// appears to have changed in such a way as to mask signals. This
	// test is consistently failing in that environment, but functions
	// as it should everywhere else.
	t.Skip()

	st := &signalTest{}
	tt := NewTelemetryTester(st)
	tt.RunTest(t)
}
