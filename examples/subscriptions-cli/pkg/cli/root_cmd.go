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

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"

	telemetryAPI "github.com/Happyholic1203/capsule8/api/v0"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

type options struct{}

// NewRootCommand creates the root command of the example CLI and returns it to be executed
func NewRootCommand(out, errorOut io.Writer) *cobra.Command {

	opts := options{}

	var rootCommand = &cobra.Command{
		Use:   "c8cli <subscription_file>",
		Short: "Subscribe to capsule8 telemetry events",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			err := opts.Run(out, errorOut, args)
			if err != nil {
				errorOut.Write([]byte(err.Error() + "\n"))
				os.Exit(1)
			}
		},
	}
	return rootCommand
}

// Run executes the logic of the example CLI based on its receiver options
func (opts *options) Run(out, errorOut io.Writer, args []string) error {

	subscription, err := getSubscipritionsFromFile(os.Args[1])
	if err != nil {
		return fmt.Errorf("Error reading subscription: %s", err.Error())
	}

	// Create telemetry service client
	conn, err := grpc.Dial(globalConfig.SensorAddress,
		grpc.WithDialer(dialer),
		grpc.WithInsecure())

	if err != nil {
		return fmt.Errorf("Error creading grpc connection to sensor: %s", err)
	}

	client := telemetryAPI.NewTelemetryServiceClient(conn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Open event stream
	stream, err := client.GetEvents(ctx, &telemetryAPI.GetEventsRequest{
		Subscription: subscription,
	})

	if err != nil {
		return fmt.Errorf("Error opening event stream: %s", err)
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
			return fmt.Errorf("Error receiving event from stream: %s", err)
		}

		for _, e := range ev.Events {
			var blob []byte
			blob, err = json.Marshal(e)
			if err != nil {
				return fmt.Errorf("Error marshaling received event to JSON: %s", err)
			}
			fmt.Println(string(blob))
		}
	}
}
