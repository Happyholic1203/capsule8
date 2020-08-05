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
	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/golang/glog"
)

type kernelCallTest struct {
	testContainer *Container
	seenEnter     bool
	seenExit      bool
}

const kernelCallDataFilename = "hello.txt"

func (kt *kernelCallTest) BuildContainer(t *testing.T) string {
	c := NewContainer(t, "kernelcall")
	err := c.Build()
	if err != nil {
		t.Error(err)
		return ""
	}

	glog.V(2).Infof("Built container %s\n", c.ImageID[0:12])
	kt.testContainer = c
	return kt.testContainer.ImageID
}

func (kt *kernelCallTest) RunContainer(t *testing.T) {
	err := kt.testContainer.Run()
	if err != nil {
		t.Error(err)
	}
	glog.V(2).Infof("Running container %s\n", kt.testContainer.ImageID[0:12])
}

func (kt *kernelCallTest) CreateSubscription(t *testing.T) *telemetryAPI.Subscription {
	filenameFilter := expression.Like(
		expression.Identifier("filename"),
		expression.Value(kernelCallDataFilename))
	kernelEvents := []*telemetryAPI.KernelFunctionCallFilter{
		&telemetryAPI.KernelFunctionCallFilter{
			Type:   telemetryAPI.KernelFunctionCallEventType_KERNEL_FUNCTION_CALL_EVENT_TYPE_ENTER,
			Symbol: "do_sys_open",
			Arguments: map[string]string{
				"filename": "+0(%si):string",
				"mode":     "+0(%cx):u16",
			},
			FilterExpression: filenameFilter,
		},
		&telemetryAPI.KernelFunctionCallFilter{
			Type:   telemetryAPI.KernelFunctionCallEventType_KERNEL_FUNCTION_CALL_EVENT_TYPE_EXIT,
			Symbol: "do_sys_open",
			Arguments: map[string]string{
				"ret": "$retval",
			},
		},
	}

	// Subscribing to container created events are currently necessary
	// to get imageIDs in other events.
	containerEvents := []*telemetryAPI.ContainerEventFilter{
		&telemetryAPI.ContainerEventFilter{
			Type: telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_CREATED,
		},
	}

	eventFilter := &telemetryAPI.EventFilter{
		KernelEvents:    kernelEvents,
		ContainerEvents: containerEvents,
	}

	return &telemetryAPI.Subscription{
		EventFilter: eventFilter,
	}
}

func (kt *kernelCallTest) HandleTelemetryEvent(t *testing.T, te *telemetryAPI.ReceivedTelemetryEvent) bool {
	switch event := te.Event.Event.(type) {
	case *telemetryAPI.TelemetryEvent_Container:
		return true

	case *telemetryAPI.TelemetryEvent_KernelCall:
		glog.V(2).Infof("Got Event %+v\n", te.Event)
		if te.Event.ImageId == kt.testContainer.ImageID {

			if filename, ok := event.KernelCall.Arguments["filename"]; ok {
				if filename.FieldType != telemetryAPI.KernelFunctionCallEvent_STRING {
					t.Errorf("Expected argument type %s, got %s\n",
						telemetryAPI.KernelFunctionCallEvent_STRING, filename.FieldType)
				} else if filename.GetStringValue() != kernelCallDataFilename {
					t.Errorf("Expected argument value %q, got %q\n",
						kernelCallDataFilename, filename.GetStringValue())
				}

				kt.seenEnter = true

			} else if ret, ok2 := event.KernelCall.Arguments["ret"]; ok2 {
				if ret.FieldType != telemetryAPI.KernelFunctionCallEvent_UINT64 {
					t.Errorf("Expected return type %s, got %s\n",
						telemetryAPI.KernelFunctionCallEvent_UINT64, ret.FieldType)
				}

				kt.seenExit = true

			} else {
				t.Errorf("Unexpected Kernel event %+v", *event.KernelCall)

			}

		} // if te.Event.ImageId == kt.testContainer.ImageID

		return !kt.seenEnter || !kt.seenExit

	default:
		t.Errorf("Unexpected event type %T\n", event)
		return false
	}
}

// TestKernelCall exercises the kernel call events, including filtering.
func TestKernelCall(t *testing.T) {
	kt := &kernelCallTest{}

	tt := NewTelemetryTester(kt)
	tt.RunTest(t)
}
