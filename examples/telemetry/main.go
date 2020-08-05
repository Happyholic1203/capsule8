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

// Sample Telemetry API client
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"

	telemetryAPI "github.com/Happyholic1203/capsule8/api/v0"
	"github.com/Happyholic1203/capsule8/pkg/expression"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes/wrappers"

	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc"
)

var config struct {
	server      string
	image       string
	json        bool
	prettyPrint bool
}

func init() {
	flag.StringVar(&config.server, "server",
		"unix:/var/run/capsule8/sensor.sock",
		"Capsule8 gRPC API server address")

	flag.StringVar(&config.image, "image", "",
		"Container image wildcard pattern to monitor")
	flag.BoolVar(&config.json, "json", false,
		"Output telemetry events as JSON")
	flag.BoolVar(&config.prettyPrint, "prettyprint", false,
		"Pretty print JSON telemetry events")
}

// Custom gRPC Dialer that understands "unix:/path/to/sock" as well as TCP addrs
func dialer(addr string, timeout time.Duration) (net.Conn, error) {
	var network, address string

	parts := strings.Split(addr, ":")
	if len(parts) > 1 && parts[0] == "unix" {
		network = "unix"
		address = parts[1]
	} else {
		network = "tcp"
		address = addr
	}

	return net.DialTimeout(network, address, timeout)
}

func createSubscription() *telemetryAPI.Subscription {
	processEvents := []*telemetryAPI.ProcessEventFilter{
		//
		// Get all process lifecycle events
		//
		&telemetryAPI.ProcessEventFilter{
			Type: telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_FORK,
		},
		&telemetryAPI.ProcessEventFilter{
			Type: telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_EXEC,
		},
		&telemetryAPI.ProcessEventFilter{
			Type: telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_EXIT,
		},
		&telemetryAPI.ProcessEventFilter{
			Type: telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_UPDATE,
		},
	}

	syscallEvents := []*telemetryAPI.SyscallEventFilter{
		// Get all open(2) syscalls
		&telemetryAPI.SyscallEventFilter{
			Type: telemetryAPI.SyscallEventType_SYSCALL_EVENT_TYPE_ENTER,

			Id: &wrappers.Int64Value{
				Value: 2, // SYS_OPEN
			},
		},

		// An example of negative filters:
		// Get all setuid(2) calls that are not root
		&telemetryAPI.SyscallEventFilter{
			Type: telemetryAPI.SyscallEventType_SYSCALL_EVENT_TYPE_ENTER,

			Id: &wrappers.Int64Value{
				Value: 105, // SYS_SETUID
			},

			FilterExpression: expression.NotEqual(
				expression.Identifier("arg0"),
				expression.Value(uint64(0))),
		},
	}

	fileEvents := []*telemetryAPI.FileEventFilter{
		//
		// Get all attempts to open files matching glob *foo*
		//
		&telemetryAPI.FileEventFilter{
			Type: telemetryAPI.FileEventType_FILE_EVENT_TYPE_OPEN,

			//
			// The glob accepts a wild card character
			// (*,?) and character classes ([).
			//
			FilenamePattern: &wrappers.StringValue{
				Value: "*foo*",
			},
		},
	}

	sinFamilyFilter := expression.Equal(
		expression.Identifier("sin_family"),
		expression.Value(uint16(2)))
	kernelCallEvents := []*telemetryAPI.KernelFunctionCallFilter{
		//
		// Install a kprobe on connect(2)
		//
		&telemetryAPI.KernelFunctionCallFilter{
			Type:   telemetryAPI.KernelFunctionCallEventType_KERNEL_FUNCTION_CALL_EVENT_TYPE_ENTER,
			Symbol: "sys_connect",
			Arguments: map[string]string{
				"sin_family": "+0(%si):u16",
				"sin_port":   "+2(%si):u16",
				"sin_addr":   "+4(%si):u32",
			},
			FilterExpression: sinFamilyFilter,
		},
	}

	userCallEvents := []*telemetryAPI.UserFunctionCallFilter{
		&telemetryAPI.UserFunctionCallFilter{
			Type:       telemetryAPI.UserFunctionCallEventType_USER_FUNCTION_CALL_EVENT_TYPE_EXIT,
			Executable: "/bin/bash",
			Symbol:     "readline",
			Arguments: map[string]string{
				"s": "+0($retval):string",
			},
		},
	}

	containerEvents := []*telemetryAPI.ContainerEventFilter{
		//
		// Get all container lifecycle events
		//
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

	// Ticker events are used for debugging and performance testing
	tickerEvents := []*telemetryAPI.TickerEventFilter{
		/*
			&telemetryAPI.TickerEventFilter{
				Interval: int64(1 * time.Second),
			},
		*/
	}

	chargenEvents := []*telemetryAPI.ChargenEventFilter{
		/*
			&telemetryAPI.ChargenEventFilter{
				Length: 16,
			},
		*/
	}

	eventFilter := &telemetryAPI.EventFilter{
		ProcessEvents:   processEvents,
		SyscallEvents:   syscallEvents,
		KernelEvents:    kernelCallEvents,
		UserEvents:      userCallEvents,
		FileEvents:      fileEvents,
		ContainerEvents: containerEvents,
		TickerEvents:    tickerEvents,
		ChargenEvents:   chargenEvents,
	}

	sub := &telemetryAPI.Subscription{
		EventFilter: eventFilter,
	}

	if config.image != "" {
		fmt.Fprintf(os.Stderr,
			"Watching for container images matching %s\n",
			config.image)

		containerFilter := &telemetryAPI.ContainerFilter{}

		containerFilter.ImageNames =
			append(containerFilter.ImageNames, config.image)

		sub.ContainerFilter = containerFilter
	}

	return sub
}

func main() {
	var marshaler *jsonpb.Marshaler

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context on control-C
	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt)

	go func() {
		<-signals
		cancel()
	}()

	// Create telemetry service client
	conn, err := grpc.DialContext(ctx, config.server,
		grpc.WithDialer(dialer),
		grpc.WithBlock(),
		grpc.WithTimeout(1*time.Second),
		grpc.WithInsecure())

	c := telemetryAPI.NewTelemetryServiceClient(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grpc.Dial: %s\n", err)
		os.Exit(1)
	}

	stream, err := c.GetEvents(ctx, &telemetryAPI.GetEventsRequest{
		Subscription: createSubscription(),
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "GetEvents: %s\n", err)
		os.Exit(1)
	}

	if config.json {
		marshaler = &jsonpb.Marshaler{EmitDefaults: true}

		if config.prettyPrint {
			marshaler.Indent = "\t"
		}
	}

	for {
		var ev *telemetryAPI.GetEventsResponse
		ev, err = stream.Recv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Recv: %s\n", err)
			os.Exit(1)
		}

		if len(ev.Statuses) > 1 ||
			(len(ev.Statuses) == 1 &&
				ev.Statuses[0].Code != int32(code.Code_OK)) {
			for _, s := range ev.Statuses {
				if config.json {
					var msg string
					msg, err = marshaler.MarshalToString(s)
					if err != nil {
						fmt.Fprintf(os.Stderr,
							"Unable to decode event: %v", err)
						continue
					}
					fmt.Println(msg)
				} else {
					fmt.Println(s)
				}
			}

		}

		for _, e := range ev.Events {
			if config.json {
				var msg string
				msg, err = marshaler.MarshalToString(e)
				if err != nil {
					fmt.Fprintf(os.Stderr,
						"Unable to decode event: %v", err)
					continue
				}
				fmt.Println(msg)
			} else {
				fmt.Println(e)
			}
		}
	}
}
