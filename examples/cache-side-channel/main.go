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
	"os"
	"os/signal"
	"strings"

	"github.com/Happyholic1203/capsule8/pkg/sensor"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"

	"github.com/golang/glog"
)

const (
	// How many cache loads to sample on. After each sample period
	// of this many cache loads, the cache miss rate is calculated
	// and examined. This value tunes the trade-off between CPU
	// load and detection accuracy.
	llcLoadSampleSize = 10000

	// Alarm thresholds as cache miss rates (between 0 and 1).
	// These values tune the trade-off between false negatives and
	// false positives.
	alarmThresholdInfo    = 0.97
	alarmThresholdWarning = 0.98
	alarmThresholdError   = 0.99

	// perf_event_attr config value for LL cache loads
	perfConfigLLCLoads = perf.PERF_COUNT_HW_CACHE_LL |
		(perf.PERF_COUNT_HW_CACHE_OP_READ << 8) |
		(perf.PERF_COUNT_HW_CACHE_RESULT_ACCESS << 16)

	// perf_event_attr config value for LL cache misses
	perfConfigLLCLoadMisses = perf.PERF_COUNT_HW_CACHE_LL |
		(perf.PERF_COUNT_HW_CACHE_OP_READ << 8) |
		(perf.PERF_COUNT_HW_CACHE_RESULT_MISS << 16)
)

type eventCounters struct {
	LLCLoads      uint64
	LLCLoadMisses uint64
}

func newSensor() *sensor.Sensor {
	s, err := sensor.NewSensor()
	if err != nil {
		glog.Fatalf("Could not create sensor: %s", err)
	}
	err = s.Start()
	if err != nil {
		glog.Fatalf("Could not start sensor: %s", err)
	}
	return s
}

type counterTracker struct {
	sensor   *sensor.Sensor
	counters []eventCounters
}

func main() {
	flag.Set("logtostderr", "true")
	flag.Parse()

	glog.Infof("Starting Capsule8 cache side channel detector")
	sensor := newSensor()
	ncpu := sensor.ProcFS.NumCPU()
	tracker := counterTracker{
		sensor:   sensor,
		counters: make([]eventCounters, ncpu),
	}

	// We ask the kernel to sample every llcLoadSampleSize LLC
	// loads. During each sample, the LLC load misses are also
	// recorded, as well as CPU number, PID/TID, and sample time.
	attr := perf.EventAttr{
		SampleType:   perf.PERF_SAMPLE_CPU | perf.PERF_SAMPLE_RAW,
		SamplePeriod: llcLoadSampleSize,
	}
	counters := []perf.CounterEventGroupMember{
		perf.CounterEventGroupMember{
			EventType: perf.EventTypeHardwareCache,
			Config:    perfConfigLLCLoads,
		},
		perf.CounterEventGroupMember{
			EventType: perf.EventTypeHardwareCache,
			Config:    perfConfigLLCLoadMisses,
		},
	}
	sub := tracker.sensor.NewSubscription()
	sub.RegisterPerformanceEventFilter(attr, counters)

	glog.Info("Monitoring for cache side channels")
	ctx, cancel := context.WithCancel(context.Background())
	status, err := sub.Run(ctx, tracker.dispatchEvent)
	for _, s := range status {
		glog.Info(s)
	}
	if err != nil {
		glog.Fatalf("Could not register subscription: %v", err)
	}

	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt)
	<-signals
	close(signals)

	glog.Info("Shutting down gracefully")
	cancel()
	sensor.Stop()
}

func (t *counterTracker) dispatchEvent(e sensor.TelemetryEvent) {
	event := e.(sensor.PerformanceTelemetryEvent)

	cpu := event.CPU
	prevCounters := t.counters[cpu]
	t.counters[cpu] = eventCounters{}

	for _, v := range event.Counters {
		switch v.EventType {
		case perf.EventTypeHardwareCache:
			switch v.Config {
			case perfConfigLLCLoads:
				t.counters[cpu].LLCLoads += v.Value
			case perfConfigLLCLoadMisses:
				t.counters[cpu].LLCLoadMisses += v.Value
			}
		}
	}

	counterDeltas := eventCounters{
		LLCLoads:      t.counters[cpu].LLCLoads - prevCounters.LLCLoads,
		LLCLoadMisses: t.counters[cpu].LLCLoadMisses - prevCounters.LLCLoadMisses,
	}

	t.alarm(e, counterDeltas)
}

func (t *counterTracker) alarm(
	e sensor.TelemetryEvent,
	counters eventCounters,
) {
	LLCLoadMissRate := float32(counters.LLCLoadMisses) /
		float32(counters.LLCLoads)
	if LLCLoadMissRate <= alarmThresholdInfo {
		return
	}

	var fields []string
	event := e.(sensor.PerformanceTelemetryEvent)
	fields = append(fields, fmt.Sprintf("LLCLoadMissRate=%v", LLCLoadMissRate))
	fields = append(fields, fmt.Sprintf("pid=%v", event.TGID))
	fields = append(fields, fmt.Sprintf("tid=%v", event.PID))
	fields = append(fields, fmt.Sprintf("cpu=%v", event.CPU))
	if event.Container.ID != "" {
		fields = append(fields, fmt.Sprintf("container_name=%v", event.Container.Name))
		fields = append(fields, fmt.Sprintf("container_id=%v", event.Container.ID))
		fields = append(fields, fmt.Sprintf("container_image=%v", event.Container.ImageName))
		fields = append(fields, fmt.Sprintf("container_image_id=%v", event.Container.ImageID))
	}

	message := strings.Join(fields, " ")
	if LLCLoadMissRate > alarmThresholdError {
		glog.Errorf(message)
	} else if LLCLoadMissRate > alarmThresholdWarning {
		glog.Warningf(message)
	} else {
		glog.Infof(message)
	}
}
