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

	"github.com/Happyholic1203/capsule8/pkg/sys/perf"
	"github.com/golang/glog"
)

var pageFaultsByPid map[int32]uint

const (
	alarmThresholdInfo     = 1
	alarmThresholdLow      = 10
	alarmThresholdMed      = 100
	alarmThresholdHigh     = 1000
	alarmThresholdCritical = 10000
)

func main() {
	flag.Set("alsologtostderr", "true")
	flag.Parse()

	glog.Info("Starting Capsule8 Meltdown Detector")

	monitor, err := perf.NewEventMonitor()
	if err != nil {
		glog.Fatal(err)
	}

	groupid, err := monitor.RegisterEventGroup("default", nil)
	if err != nil {
		glog.Fatal(err)
	}

	// Look for segmentation faults trying to read kernel memory addresses
	filter := "address > 0xffff000000000000"
	_, err = monitor.RegisterTracepoint("exceptions/page_fault_user",
		onPageFaultUser, groupid, perf.WithFilter(filter))
	if err != nil {
		glog.Fatal(err)
	}

	pageFaultsByPid = make(map[int32]uint)

	glog.Info("Monitoring for meltdown exploitation attempts")
	monitor.Run()
}

func onPageFaultUser(
	_ uint64,
	sample *perf.Sample,
) {
	pid := int32(sample.TID)
	pageFaultsByPid[pid]++
	switch faults := pageFaultsByPid[pid]; faults {
	case alarmThresholdInfo:
		glog.Infof("pid %d kernel address page faults = %d",
			pid, faults)
	case alarmThresholdLow:
		glog.Warningf("pid %d kernel address page faults = %d",
			pid, faults)
	case alarmThresholdMed:
		glog.Warningf("pid %d kernel address page faults = %d",
			pid, faults)
	case alarmThresholdHigh:
		glog.Errorf("pid %d kernel address page faults = %d",
			pid, faults)
	case alarmThresholdCritical:
		glog.Errorf("pid %d kernel address page faults = %d",
			pid, faults)
	}
}
