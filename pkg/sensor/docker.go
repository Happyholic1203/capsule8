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
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Happyholic1203/capsule8/pkg/sys/perf"

	"github.com/golang/glog"
)

// ----------------------------------------------------------------------------
// Docker container configuration file format V2
// ----------------------------------------------------------------------------

type dockerConfigState struct {
	Running           bool      `json:"Running"`
	Paused            bool      `json:"Paused"`
	Restarting        bool      `json:"Restarting"`
	OOMKilled         bool      `json:"OOMKilled"`
	RemovalInProgress bool      `json:"RemovalInProgress"`
	Dead              bool      `json:"Dead"`
	Pid               int       `json:"Pid"`
	StartedAt         time.Time `json:"StartedAt"`
	FinishedAt        time.Time `json:"FinishedAt"`
	Health            string    `json:"Health"`
	ExitCode          int       `json:"ExitCode"`
}

type dockerConfigConfig struct {
	// XXX: Fill in as needed ...
	Image string `json:"Image"`
	// XXX: ...
}

type dockerConfigV2 struct {
	// XXX: Fill in as needed ...
	ID     string             `json:"ID"`
	Name   string             `json:"Name"`
	Image  string             `json:"Image"`
	State  dockerConfigState  `json:"State"`
	Config dockerConfigConfig `json:"Config"`
	// XXX: ...
}

const (
	dockerRenameKprobeSymbol    = "sys_renameat"
	dockerRenameKprobeFetchargs = "newname=+0(%cx):string"
	dockerRenameKprobeFilter    = "newname ~ \"*/config.v2.json\""

	dockerUnlinkKprobeSymbol    = "sys_unlinkat"
	dockerUnlinkKprobeFetchargs = "pathname=+0(%si):string"
	dockerUnlinkKprobeFilter    = "pathname ~ \"*/config.v2.json\""
)

type dockerDeferredAction func()

// dockerMonitor monitors the system for Docker container events
type dockerMonitor struct {
	sensor           *Sensor
	containerDir     string
	containerDirFile *os.File

	startLock  sync.Mutex
	startQueue []dockerDeferredAction
	started    bool
}

// newDockerMonitor creates a new Docker monitor that monitors the specified
// container directory using the specified EventMonitor. When changes occur,
// the specified cache is updated.
func newDockerMonitor(sensor *Sensor, containerDir string) *dockerMonitor {
	d, err := os.Open(containerDir)
	if err != nil {
		glog.Infof("Docker monitoring of %s disabled: %s",
			containerDir, err)
		return nil
	}

	dm := &dockerMonitor{
		sensor:           sensor,
		containerDir:     containerDir,
		containerDirFile: d,
	}

	// Register these probes in an enabled state so that we get the events
	// right away. Otherwise there'll be race conditions as we scan the
	// filesystem for existing containers

	_, err = sensor.RegisterKprobe(dockerRenameKprobeSymbol, false,
		dockerRenameKprobeFetchargs, dm.handleRename,
		sensor.ContainerCache.EventGroupID,
		perf.WithFilter(dockerRenameKprobeFilter),
		perf.WithTracingEventName("docker1"),
		perf.WithEventEnabled())
	if err != nil {
		glog.Fatalf("Could not register Docker monitor %s kprobe: %s",
			dockerRenameKprobeSymbol, err)
	}

	_, err = sensor.RegisterKprobe(dockerUnlinkKprobeSymbol, false,
		dockerUnlinkKprobeFetchargs, dm.handleUnlink,
		sensor.ContainerCache.EventGroupID,
		perf.WithFilter(dockerUnlinkKprobeFilter),
		perf.WithTracingEventName("docker2"),
		perf.WithEventEnabled())
	if err != nil {
		glog.Fatalf("Could not register Docker monitor %s kprobe: %s",
			dockerUnlinkKprobeSymbol, err)
	}

	return dm
}

func (dm *dockerMonitor) start() {
	if dm.started {
		glog.Fatal("Illegal second call to dockerMonitor.start()")
	}

	if dm.containerDirFile != nil {
		defer func() {
			dm.containerDirFile.Close()
			dm.containerDirFile = nil
		}()

		// Scan the filesystem looking for existing containers
		names, err := dm.containerDirFile.Readdirnames(0)
		if err != nil {
			glog.Fatalf("Could not read directory %s: %s", dm.containerDir, err)
		}
		for _, name := range names {
			configFilename := filepath.Join(dm.containerDir, name, "config.v2.json")
			var configFile *os.File
			configFile, err = os.Open(configFilename)
			if err == nil {
				err = dm.processDockerConfig(perf.SampleID{}, configFilename, configFile)
				configFile.Close()
				if err == nil {
					glog.V(2).Infof("{DOCKER} Found existing container %s", name)
				}
			}
		}
	}

	dm.startLock.Lock()
	for len(dm.startQueue) > 0 {
		queue := dm.startQueue
		dm.startQueue = nil
		dm.startLock.Unlock()
		for _, f := range queue {
			f()
		}
		dm.startLock.Lock()
	}
	dm.started = true
	dm.startLock.Unlock()
}

func (dm *dockerMonitor) processDockerConfig(
	sampleID perf.SampleID,
	configFilename string,
	configFile io.Reader,
) error {
	configJSON, err := ioutil.ReadAll(configFile)
	if err != nil {
		return err
	}
	JSONString := string(configJSON)

	paths := strings.Split(configFilename, "/")
	containerID := paths[len(paths)-2]

	containerCache := dm.sensor.ContainerCache
	containerInfo := containerCache.LookupContainer(containerID, false)
	if containerInfo != nil && containerInfo.JSONConfig == JSONString {
		// No change; do nothing more
		return nil
	}

	var config dockerConfigV2
	err = json.Unmarshal(configJSON, &config)
	if err != nil {
		glog.V(1).Infof("Could not unmarshal %s: %s", configFilename, err)
		return err
	}

	data := make(map[string]interface{})
	// Update the cache with the newly loaded information
	if config.ID != containerID || containerInfo == nil {
		containerID = config.ID
		containerInfo = containerCache.LookupContainer(containerID, true)
	}
	data["JSONConfig"] = JSONString
	data["Name"] = config.Name
	data["ImageID"] = strings.TrimPrefix(config.Image, "sha256:")
	data["ImageName"] = config.Config.Image
	data["Pid"] = config.State.Pid
	data["ExitCode"] = config.State.ExitCode

	var newState ContainerState
	if !config.State.Running && config.State.StartedAt.IsZero() {
		newState = ContainerStateCreated
	} else if config.State.Restarting {
		newState = ContainerStateRestarting
	} else if config.State.Running && !config.State.StartedAt.IsZero() {
		newState = ContainerStateRunning
	} else if config.State.RemovalInProgress {
		newState = ContainerStateRemoving
	} else if config.State.Paused {
		newState = ContainerStatePaused
	} else if !config.State.Running && !config.State.FinishedAt.IsZero() {
		newState = ContainerStateExited
	}

	data["State"] = newState
	containerInfo.Update(containerCache, ContainerRuntimeDocker, sampleID, data)

	return nil
}

func (dm *dockerMonitor) maybeDeferAction(f func()) {
	if !dm.started {
		dm.startLock.Lock()
		if !dm.started {
			dm.startQueue = append(dm.startQueue, f)
			dm.startLock.Unlock()
			return
		}
		dm.startLock.Unlock()
	}

	f()
}

func (dm *dockerMonitor) handleRename(eventid uint64, sample *perf.Sample) {
	configFilename, err := sample.GetString("newname")
	if err != nil ||
		!strings.HasPrefix(configFilename, dm.containerDir) ||
		!strings.HasSuffix(configFilename, "/config.v2.json") {
		return
	}

	configFile, err := os.Open(configFilename)
	if err == nil {
		// references to sample cannot outlive this function, because
		// the sample may be reused in EventMonitor.
		sampleID := sample.SampleID
		dm.maybeDeferAction(func() {
			dm.processDockerConfig(sampleID,
				configFilename, configFile)
			configFile.Close()
		})
	}
}

func (dm *dockerMonitor) handleUnlink(eventid uint64, sample *perf.Sample) {
	configFilename, err := sample.GetString("pathname")
	if err != nil || !strings.HasPrefix(configFilename, dm.containerDir) {
		return
	}

	// references to sample cannot outlive this function, because
	// the sample may be reused in EventMonitor.
	sampleID := sample.SampleID
	dm.maybeDeferAction(func() {
		parts := strings.Split(configFilename, "/")
		if len(parts) >= 2 {
			containerID := parts[len(parts)-2]
			dm.sensor.ContainerCache.DeleteContainer(containerID,
				ContainerRuntimeDocker, sampleID)
		}
	})
}
