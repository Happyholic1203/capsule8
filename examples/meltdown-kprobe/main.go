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

package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	telemetryAPI "github.com/Happyholic1203/capsule8/api/v0"
	"github.com/Happyholic1203/capsule8/pkg/expression"
)

var config struct {
	endpoint string
}

func init() {
	flag.StringVar(&config.endpoint, "endpoint",
		"unix:/var/run/capsule8/sensor.sock",
		"Capsule8 gRPC API endpoint")
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
	arguments := make(map[string]string)

	arguments["signal"] = "%di:u64"
	arguments["address"] = "+24(%si):u64"

	filterExpression := expression.LogicalAnd(
		expression.Equal(
			expression.Identifier("signal"),
			expression.Value(uint64(11))),
		expression.GreaterThan(
			expression.Identifier("address"),
			expression.Value(uint64(0xffff000000000000))))

	kernelCallEvents := []*telemetryAPI.KernelFunctionCallFilter{
		&telemetryAPI.KernelFunctionCallFilter{
			Type:             telemetryAPI.KernelFunctionCallEventType_KERNEL_FUNCTION_CALL_EVENT_TYPE_ENTER,
			Symbol:           "force_sig_info",
			Arguments:        arguments,
			FilterExpression: filterExpression,
		},
	}

	eventFilter := &telemetryAPI.EventFilter{
		KernelEvents: kernelCallEvents,
	}

	sub := &telemetryAPI.Subscription{
		EventFilter: eventFilter,
	}

	return sub
}

func main() {
	flag.Parse()

	// Create telemetry service client
	conn, err := grpc.Dial(config.endpoint,
		grpc.WithDialer(dialer),
		grpc.WithInsecure())

	c := telemetryAPI.NewTelemetryServiceClient(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grpc.Dial: %s\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := c.GetEvents(ctx, &telemetryAPI.GetEventsRequest{
		Subscription: createSubscription(),
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "GetEvents: %s\n", err)
		os.Exit(1)
	}

	// Exit cleanly on Control-C
	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt)

	go func() {
		<-signals
		cancel()
	}()

	for {
		var ev *telemetryAPI.GetEventsResponse
		ev, err = stream.Recv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Recv: %s\n", err)
			os.Exit(1)
		}

		for _, e := range ev.Events {
			fmt.Println(e)
		}
	}
}
