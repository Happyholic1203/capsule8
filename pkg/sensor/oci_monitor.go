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

package sensor

import (
	//"encoding/json"
	//"io/ioutil"
	"os"
	//"path/filepath"
	//"strings"
	"sync"
	//"github.com/Happyholic1203/capsule8/pkg/sys/perf"
	//"github.com/golang/glog"
)

// The OCI Monitor watches for containers managed only by runc. Not much
// information in which the sensor is interested is available. No image id,
// image name, container name, etc. is defined. There's not a good way to get
// lifecycle information either. The monitor watches for changes to config.json
// in the container bundle. File close events don't always necessarily match
// file open events, so information updates won't necessarily be reliable.

// ----------------------------------------------------------------------------
// OCI configuration file format
// ----------------------------------------------------------------------------

type ociConfig struct {
	// XXX: Fill in as needed ...
	// XXX: ...
}

const (
	ociSysOpenKprobeSymbol    = "do_sys_open"
	ociSysOpenKprobeFetchargs = "filename=+0(%si):string flags=%dx:s32"
	ociSysOpenKprobeFilter    = "(flags & 1 || flags & 2) && filename ~ \"*/config.json\""

	// It would be nice if we could also pull out file->dentry->d_parent->d_name
	// here to get the parent filename, but testing yields no useful
	// information at this point. This is the best we can do ...
	ociImaFileFreeKprobeSymbol    = "ima_file_free"
	ociImaFileFreeKprobeFetchargs = "filename=+56(+24(%di)):string p=+56(+24(%di)):u64 " +
		"f_op=+40(%di):u64 f_flags=+64(%di):u32 old_f_flags=+56(%di):u32"
	// f_op will be NULL if f_inode is missing as in 2.6 kernels
	ociImaFileFreeKprobeFilter = "p != 0 && filename == \"config.json\" " +
		"&& ((f_op == 0 && (old_f_flags & 1 || old_f_flags & 2)) || " +
		"    (f_op != 0 && (f_flags & 1 || f_flags & 2)))"

	ociUnlinkKprobeSymbol    = "sys_unlinkat"
	ociUnlinkKprobeFetchargs = "pathname=+0(%si):string"
	ociUnlinkKprobeFilter    = "pathname ~ \"*/config.json\""
)

type ociDeferredAction func()

type ociMonitor struct {
	sensor           *Sensor
	containerDir     string
	containerDirFile *os.File

	configOpens map[int][]string

	startLock  sync.Mutex
	startQueue []ociDeferredAction
	started    bool
}

/*
func newOciMonitor(sensor *Sensor, containerDir string) *ociMonitor {
	d, err := os.Open(containerDir)
	if err != nil {
		glog.Infof("OCI monitoring of %s disabled: %s",
			containerDir, err)
		return nil
	}

	om := &ociMonitor{
		sensor:           sensor,
		containerDir:     containerDir,
		containerDirFile: d,
		configOpens:      make(map[int][]string),
	}

	// Register probes in an enabled state so that we get the events
	// right away. Otherwise there'll be race conditions as we scan
	// the filesystem for existing containers.

	_, err = sensor.RegisterKprobe(ociSysOpenKprobeSymbol, false,
		ociSysOpenKprobeFetchargs, om.decodeSysOpen,
		perf.WithFilter(ociSysOpenKprobeFilter),
		perf.WithEventEnabled())
	if err != nil {
		glog.Fatalf("Could not register OCI monitor %s kprobe: %s",
			ociSysOpenKprobeSymbol, err)
	}
	_, err = sensor.RegisterKprobe(ociImaFileFreeKprobeSymbol, false,
		ociImaFileFreeKprobeFetchargs, om.decodeImaFileFree,
		perf.WithFilter(ociImaFileFreeKprobeFilter),
		perf.WithEventEnabled())
	if err != nil {
		glog.Fatalf("Could not register OCI monitor %s kprobe: %s",
			ociImaFileFreeKprobeSymbol, err)
	}

	_, err = sensor.RegisterKprobe(ociUnlinkKprobeSymbol, false,
		ociUnlinkKprobeFetchargs, om.decodeUnlink,
		perf.WithFilter(ociUnlinkKprobeFilter),
		perf.WithEventEnabled())
	if err != nil {
		glog.Fatalf("Could not register OCI monitor %s kprobe: %s",
			ociUnlinkKprobeSymbol, err)
	}

	return om
}

func (om *ociMonitor) start() {
	if om.started {
		glog.Fatal("Illegal second call to ociMonitor.start()")
	}

	if om.containerDirFile != nil {
		defer func() {
			om.containerDirFile.Close()
			om.containerDirFile = nil
		}()

		// Scan the filesystem looking for existing containers
		names, err := om.containerDirFile.Readdirnames(0)
		if err != nil {
			glog.Fatalf("Could not read directory %s: %s", om.containerDir, err)
		}
		for _, name := range names {
			configFilename := filepath.Join(om.containerDir, name, "config.json")
			err = om.processConfigJSON(perf.SampleID{}, configFilename)
			if err == nil {
				glog.V(2).Infof("{OCI} Found existing container %s", name)
			}
		}
	}

	om.startLock.Lock()
	for len(om.startQueue) > 0 {
		queue := om.startQueue
		om.startQueue = nil
		om.startLock.Unlock()
		for _, f := range queue {
			f()
		}
		om.startLock.Lock()
	}
	om.started = true
	om.startLock.Unlock()
}

func (om *ociMonitor) processConfigJSON(
	sampleID perf.SampleID,
	configFilename string,
) error {
	configJSON, err := ioutil.ReadFile(configFilename)
	if err != nil {
		return err
	}
	JSONString := string(configJSON)

	paths := strings.Split(configFilename, "/")
	containerID := paths[len(paths)-2]

	containerCache := om.sensor.ContainerCache
	containerInfo := containerCache.LookupContainer(containerID, false)
	if containerInfo != nil && containerInfo.OCIConfig == JSONString {
		// No change; do nothing more
		return nil
	}

	var config ociConfig
	err = json.Unmarshal(configJSON, &config)
	if err != nil {
		glog.V(1).Infof("Could not unmarshal %s: %s", configFilename, err)
		return err
	}

	data := make(map[string]interface{})
	// Update the cache with the newly loaded information
	if containerInfo == nil {
		containerInfo = containerCache.LookupContainer(containerID, true)
	}
	data["OCIConfig"] = JSONString

	if containerInfo.State == ContainerStateUnknown {
		data["State"] = ContainerStateCreated
	}
	containerInfo.Update(containerCache, ContainerRuntimeUnknown, sampleID, data)

	return nil
}

func (om *ociMonitor) maybeDeferAction(f func()) {
	if !om.started {
		om.startLock.Lock()
		if !om.started {
			om.startQueue = append(om.startQueue, f)
			om.startLock.Unlock()
			return
		}
		om.startLock.Unlock()
	}

	f()
}

func (om *ociMonitor) decodeSysOpen(_ uint64, sample *perf.Sample) {
	pid := int(sample.TID)
	configFilename, _ := sample.GetString("filename")

	om.maybeDeferAction(func() {
		om.configOpens[pid] = append(om.configOpens[pid], configFilename)
	})

	return nil, nil
}

func (om *ociMonitor) decodeImaFileFree(_ uint64, sample *perf.Sample) {
	sampleID := sample.SampleID

	om.maybeDeferAction(func() {
		pid := int(sample.TID)
		opens := om.configOpens[pid]
		if len(opens) == 0 {
			return
		}
		filename := opens[len(opens)-1]
		if len(opens) == 1 {
			delete(om.configOpens, pid)
		} else {
			opens = opens[:len(opens)-1]
			om.configOpens[pid] = opens
		}

		if !strings.HasPrefix(filename, om.containerDir) {
			return
		}

		om.processConfigJSON(sampleID, filename)
	})

	return nil, nil
}

func (om *ociMonitor) decodeUnlink(_ uint64, sample *perf.Sample) {
	configFilename, err := sample.GetString("pathname")
	if err != nil || !strings.HasPrefix(configFilename, om.containerDir) {
		return nil, nil
	}
	sampleID := sample.SampleID

	om.maybeDeferAction(func() {
		parts := strings.Split(configFilename, "/")
		if len(parts) >= 2 {
			containerID := parts[len(parts)-2]
			om.sensor.ContainerCache.DeleteContainer(containerID,
				ContainerRuntimeUnknown, sampleID)
		}

	})

	return nil, nil
}

*/
