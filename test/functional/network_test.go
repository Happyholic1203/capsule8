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
	"encoding/binary"
	"fmt"
	"syscall"
	"testing"
	"unsafe"

	telemetryAPI "github.com/Happyholic1203/capsule8/api/v0"
	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/golang/glog"
)

const (
	// These need to be coordinated with the code in network/main.go
	testNetworkPort    = 8080
	testNetworkBacklog = 128
	testNetworkMsgLen  = 5
)

var (
	testNetworkPortN = hton16(testNetworkPort)
)

func hton16(port uint16) uint16 {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, port)
	return *(*uint16)(unsafe.Pointer(&b[0]))
}

type networkTest struct {
	testContainer *Container
	containerID   string
	serverSocket  uint64
	clientSocket  uint64
	seenEvents    map[telemetryAPI.NetworkEventType]bool
}

func (nt *networkTest) BuildContainer(t *testing.T) string {
	c := NewContainer(t, "network")
	err := c.Build()
	if err != nil {
		t.Error(err)
		return ""
	}

	glog.V(2).Infof("Built container %s\n", c.ImageID[0:12])
	nt.testContainer = c
	return nt.testContainer.ImageID
}

func portListItem(port uint16) string {
	return fmt.Sprintf("%d:%d", port, port)
}

func (nt *networkTest) RunContainer(t *testing.T) {
	err := nt.testContainer.Run()
	if err != nil {
		t.Error(err)
	}
	glog.V(2).Infof("Running container %s\n", nt.testContainer.ImageID[0:12])
}

func (nt *networkTest) CreateSubscription(t *testing.T) *telemetryAPI.Subscription {
	familyFilter := expression.Equal(
		expression.Identifier("sa_family"),
		expression.Value(uint16(syscall.AF_INET)))
	portFilter := expression.Equal(
		expression.Identifier("sin_port"),
		expression.Value(testNetworkPortN))
	resultFilter := expression.Equal(
		expression.Identifier("ret"),
		expression.Value(int64(0)))
	backlogFilter := expression.Equal(
		expression.Identifier("backlog"),
		expression.Value(uint64(testNetworkBacklog)))
	/*
		goodFDFilter := expression.GreaterThan(
			expression.Identifier("ret"),
			expression.Value(int32(-1)))
	*/

	msgLenFilter := expression.Equal(
		expression.Identifier("ret"),
		expression.Value(int64(testNetworkMsgLen)))

	networkEvents := []*telemetryAPI.NetworkEventFilter{
		&telemetryAPI.NetworkEventFilter{
			Type:             telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_CONNECT_ATTEMPT,
			FilterExpression: expression.LogicalAnd(familyFilter, portFilter),
		},
		&telemetryAPI.NetworkEventFilter{
			Type: telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_CONNECT_RESULT,
			//FilterExpression: resultFilter,
		},
		&telemetryAPI.NetworkEventFilter{
			Type:             telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_BIND_ATTEMPT,
			FilterExpression: expression.LogicalAnd(familyFilter, portFilter),
		},
		&telemetryAPI.NetworkEventFilter{
			Type:             telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_BIND_RESULT,
			FilterExpression: resultFilter,
		},
		&telemetryAPI.NetworkEventFilter{
			Type:             telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_LISTEN_ATTEMPT,
			FilterExpression: backlogFilter,
		},
		&telemetryAPI.NetworkEventFilter{
			Type:             telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_LISTEN_RESULT,
			FilterExpression: resultFilter,
		},
		&telemetryAPI.NetworkEventFilter{
			Type: telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_ACCEPT_ATTEMPT,
		},
		&telemetryAPI.NetworkEventFilter{
			Type: telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_ACCEPT_RESULT,
			//FilterExpression: goodFDFilter,
		},
		&telemetryAPI.NetworkEventFilter{
			Type: telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_SENDTO_ATTEMPT,
		},
		&telemetryAPI.NetworkEventFilter{
			Type:             telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_SENDTO_RESULT,
			FilterExpression: msgLenFilter,
		},
		&telemetryAPI.NetworkEventFilter{
			Type: telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_RECVFROM_ATTEMPT,
		},
		&telemetryAPI.NetworkEventFilter{
			Type:             telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_RECVFROM_RESULT,
			FilterExpression: msgLenFilter,
		},
	}

	// Subscribing to container created events are currently necessary
	// to get imageIDs in other events.
	containerEvents := []*telemetryAPI.ContainerEventFilter{
		&telemetryAPI.ContainerEventFilter{
			Type: telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_CREATED,
		},
		&telemetryAPI.ContainerEventFilter{
			Type: telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_EXITED,
		},
	}

	eventFilter := &telemetryAPI.EventFilter{
		NetworkEvents:   networkEvents,
		ContainerEvents: containerEvents,
	}

	return &telemetryAPI.Subscription{
		EventFilter: eventFilter,
	}
}

func (nt *networkTest) HandleTelemetryEvent(t *testing.T, te *telemetryAPI.ReceivedTelemetryEvent) bool {

	switch event := te.Event.Event.(type) {
	case *telemetryAPI.TelemetryEvent_Container:
		switch event.Container.Type {
		case telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_CREATED:
			return true

		case telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_EXITED:
			unseen := []telemetryAPI.NetworkEventType{}
			for i := telemetryAPI.NetworkEventType(1); i <= 12; i++ {
				if !nt.seenEvents[i] {
					unseen = append(unseen, i)
				}
			}
			if len(unseen) > 0 {
				t.Logf("Never saw network event(s) %+v\n", unseen)
			}
			return true

		default:
			t.Errorf("Unexpected Container event %+v\n", event)
			return false
		}

	case *telemetryAPI.TelemetryEvent_Network:
		glog.V(2).Infof("Got Network Event %+v\n", te.Event)
		if te.Event.ImageId == nt.testContainer.ImageID {
			switch event.Network.Type {
			case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_CONNECT_ATTEMPT:
				nt.clientSocket = event.Network.Sockfd

			case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_CONNECT_RESULT:
				// The golang runtime uses non-blocking sockets, so a successful connect will
				// return an EINPROGRESS. The container also attempts connecting to TCP6, so
				// we also get an EADDRNOTAVAIL (-99).
				if event.Network.Result != 0 && event.Network.Result != -int64(syscall.EADDRNOTAVAIL) {
					// Don't mark this event as successfully received
					return true
				}

			case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_BIND_ATTEMPT:
				if event.Network.Address.Family != telemetryAPI.NetworkAddressFamily_NETWORK_ADDRESS_FAMILY_INET {
					t.Errorf("Expected bind family %s, got %s",
						telemetryAPI.NetworkAddressFamily_NETWORK_ADDRESS_FAMILY_INET,
						event.Network.Address.Family)
					return false
				}

				addr, haveAddr := event.Network.Address.Address.(*telemetryAPI.NetworkAddress_Ipv4Address)

				if !haveAddr {
					t.Errorf("Unexpected bind address %+v", event.Network.Address.Address)
					return false
				} else if addr.Ipv4Address.Port != uint32(testNetworkPortN) {
					t.Errorf("Expected bind port %d, got %d",
						testNetworkPortN, addr.Ipv4Address.Port)
					return false
				}

				nt.serverSocket = event.Network.Sockfd

			case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_BIND_RESULT:
				if event.Network.Result != 0 {
					t.Errorf("Expected bind result 0, got %d",
						event.Network.Result)
					return false
				}

			case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_LISTEN_ATTEMPT:
				if event.Network.Backlog != testNetworkBacklog {
					t.Errorf("Expected listen backlog %d, got %d",
						testNetworkBacklog, event.Network.Backlog)
					return false
				}

			case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_LISTEN_RESULT:
				if event.Network.Result != 0 {
					t.Errorf("Expected listen result 0, got %d",
						event.Network.Result)
					return false
				}

			case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_ACCEPT_ATTEMPT:
				if nt.serverSocket != 0 && event.Network.Sockfd != nt.serverSocket {
					// This is not the accept() attempt we are looking for
					return true
				}

			case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_ACCEPT_RESULT:
				if event.Network.Result < 0 && event.Network.Result != -int64(syscall.EAGAIN) {
					t.Errorf("Expected accept result > -1, got %d",
						event.Network.Result)
					return false
				}

			case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_SENDTO_RESULT:
				if event.Network.Result != int64(testNetworkMsgLen) {
					t.Errorf("Expected sendto result %d, got %d",
						testNetworkMsgLen, event.Network.Result)
					return false
				}

			case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_RECVFROM_RESULT:
				if event.Network.Result != int64(testNetworkMsgLen) {
					t.Errorf("Expected recvfrom result %d, got %d",
						testNetworkMsgLen, event.Network.Result)
					return false
				}

			}

			nt.seenEvents[event.Network.Type] = true
		}

		return len(nt.seenEvents) < 12

	default:
		t.Errorf("Unexpected event type %T\n", event)
		return false
	}
}

// TestNetwork exercises the network events.
func TestNetwork(t *testing.T) {
	nt := &networkTest{seenEvents: make(map[telemetryAPI.NetworkEventType]bool)}

	tt := NewTelemetryTester(nt)
	tt.RunTest(t)
}
