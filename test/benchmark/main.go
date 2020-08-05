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
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc"

	telemetryAPI "github.com/Happyholic1203/capsule8/api/v0"
	sensorConfig "github.com/Happyholic1203/capsule8/pkg/config"
	"github.com/Happyholic1203/capsule8/pkg/sensor"
	"github.com/Happyholic1203/capsule8/pkg/services"
	"github.com/golang/glog"

	"github.com/Happyholic1203/capsule8/pkg/version"
)

var config struct {
	endpoint string
	image    string
	verbose  bool
}

var (
	testSensor *sensor.Sensor
	manager    *services.ServiceManager
)

func init() {
	flag.StringVar(&config.endpoint, "endpoint",
		"unix:/var/run/capsule8/sensor.sock",
		"Capsule8 gRPC API endpoint")

	flag.StringVar(&config.image, "image", "",
		"container image wildcard pattern to monitor")

	flag.BoolVar(&config.verbose, "verbose", false,
		"verbose (print events received)")
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
	}

	containerEvents := []*telemetryAPI.ContainerEventFilter{
		&telemetryAPI.ContainerEventFilter{
			Type: telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_RUNNING,
		},
		&telemetryAPI.ContainerEventFilter{
			Type: telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_EXITED,
		},
	}

	eventFilter := &telemetryAPI.EventFilter{
		ProcessEvents:   processEvents,
		ContainerEvents: containerEvents,
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

var subscriptionEvents int64

var startSubEvents int64
var startMetrics map[string]sensor.MetricsCounters
var startRusage map[string]unix.Rusage

func onContainerRunning(cID string) {
	var rusage unix.Rusage

	if startMetrics == nil {
		startMetrics = make(map[string]sensor.MetricsCounters)
	}

	startMetrics[cID] = testSensor.Metrics

	startSubEvents = subscriptionEvents

	err := unix.Getrusage(unix.RUSAGE_SELF, &rusage)
	if err != nil {
		log.Fatal(err)
	}

	if startRusage == nil {
		startRusage = make(map[string]unix.Rusage)
	}
	startRusage[cID] = rusage
}

func max(x, y int64) int64 {
	if x > y {
		return x
	}
	return y
}

func onContainerExited(cID string) {
	var metricsStart, metricsEnd, metricsDelta sensor.MetricsCounters

	metricsEnd = testSensor.Metrics
	metricsStart = startMetrics[cID]

	metricsDelta.Events = metricsEnd.Events - metricsStart.Events
	metricsDelta.Subscriptions = uint64(max(int64(metricsEnd.Subscriptions),
		int64(metricsStart.Subscriptions)))

	var rusage, delta unix.Rusage
	err := unix.Getrusage(unix.RUSAGE_SELF, &rusage)
	if err != nil {
		log.Fatal(err)
	}

	start := startRusage[cID]

	startTotalUserUsec := (start.Utime.Sec * 1000000) + start.Utime.Usec
	startTotalSysUsec := (start.Stime.Sec * 1000000) + start.Stime.Usec

	stopTotalUserUsec := (rusage.Utime.Sec * 1000000) + rusage.Utime.Usec
	stopTotalSysUsec := (rusage.Stime.Sec * 1000000) + rusage.Stime.Usec

	deltaTotalUserUsec := stopTotalUserUsec - startTotalUserUsec
	deltaTotalSysUsec := stopTotalSysUsec - startTotalSysUsec

	delta.Utime.Sec = deltaTotalUserUsec / 1000000
	delta.Utime.Usec = deltaTotalUserUsec % 1000000
	delta.Stime.Sec = deltaTotalSysUsec / 1000000
	delta.Stime.Usec = deltaTotalSysUsec % 1000000

	delta.Maxrss = max(rusage.Maxrss, start.Maxrss)
	delta.Minflt = rusage.Minflt - start.Minflt
	delta.Majflt = rusage.Majflt - start.Majflt
	delta.Inblock = rusage.Inblock - start.Inblock
	delta.Oublock = rusage.Oublock - start.Oublock
	delta.Nvcsw = rusage.Nvcsw - start.Nvcsw
	delta.Nivcsw = rusage.Nivcsw - start.Nivcsw

	nEvents := subscriptionEvents - startSubEvents
	usecsPerEvent := deltaTotalUserUsec / nEvents
	ssecsPerEvent := deltaTotalSysUsec / nEvents

	fmt.Printf("%s Events:%d avg_user_us_per_event:%d avg_sys_us_per_event:%d %+v %+v\n",
		cID, nEvents, usecsPerEvent, ssecsPerEvent,
		metricsDelta, delta)
}

func main() {
	// Set "alsologtostderr" flag so that glog messages go stderr as well as /tmp.
	//flag.Set("alsologtostderr", "true")
	flag.Parse()

	// Enable profiling by default
	sensorConfig.Global.ProfilingListenAddr = ":6060"

	// Log version and build at "Starting ..." for debugging
	version.InitialBuildLog("benchmark")

	manager = services.NewServiceManager()

	if len(sensorConfig.Sensor.ListenAddr) > 0 {
		var err error
		testSensor, err = sensor.NewSensor()
		if err != nil {
			glog.Fatalf("Could not create sensor: %s", err)
		}

		if err = testSensor.Start(); err != nil {
			glog.Fatalf("Could not start sensor: %s", err)
		}
		defer testSensor.Stop()

		service := sensor.NewTelemetryService(testSensor,
			sensorConfig.Sensor.ListenAddr)
		manager.RegisterService(service)
	}

	go func() {
		manager.Run()
	}()

	time.Sleep(1 * time.Second)

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
		var response *telemetryAPI.GetEventsResponse
		response, err = stream.Recv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Recv: %s\n", err)
			os.Exit(1)
		}

		for _, event := range response.Events {
			e := event.Event

			if config.verbose {
				fmt.Printf("%+v\n", e)
			}

			switch x := e.Event.(type) {
			case *telemetryAPI.TelemetryEvent_Container:
				switch x.Container.GetType() {
				case telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_RUNNING:
					onContainerRunning(e.ContainerId)

				case telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_EXITED:
					onContainerExited(e.ContainerId)
				}
			}

			subscriptionEvents++
		}
	}
}
