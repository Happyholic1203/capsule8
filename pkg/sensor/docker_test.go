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

package sensor

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/sys/unix"
)

func TestDockerMonitor(t *testing.T) {
	sensor := newUnitTestSensor(t)
	defer sensor.Stop()

	// With a non-existant containerDir, newDockerMonitor should return nil
	containerDir := filepath.Join(sensor.runtimeDir, "doesnotexist", "docker")
	dm := newDockerMonitor(sensor, containerDir)
	assert.Nil(t, dm)

	// Create a functioning monitor
	newUnitTestKprobe(t, sensor, 0, `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:__data_loc char[] newname;	offset:16;	size:4;	signed:1;

print fmt: "(%lx) newname=\"%s\"", REC->__probe_ip, __get_str(newname)`)
	newUnitTestKprobe(t, sensor, 0, `name: ^^NAME^^
ID: ^^ID^^
format:
	field:unsigned short common_type;	offset:0;	size:2;	signed:0;
	field:unsigned char common_flags;	offset:2;	size:1;	signed:0;
	field:unsigned char common_preempt_count;	offset:3;	size:1;signed:0;
	field:int common_pid;	offset:4;	size:4;	signed:1;

	field:unsigned long __probe_ip;	offset:8;	size:8;	signed:0;
	field:__data_loc char[] pathname;	offset:16;	size:4;	signed:1;

print fmt: "(%lx) pathname=\"%s\"", REC->__probe_ip, __get_str(pathname)`)
	dm = newDockerMonitor(sensor, sensor.dockerContainerDir)
	require.NotNil(t, dm)

	// Test enqueueing of pending actions
	var executedDeferredAction bool
	dm.maybeDeferAction(func() {
		executedDeferredAction = true
	})
	dm.start()
	assert.True(t, executedDeferredAction)

	// Test handleRename
	containerID := "badb01badb01badb01badb01badb01badb01badb01badb01badb01badb01badb"
	configFilename := filepath.Join(sensor.dockerContainerDir, containerID, "config.v2.json")
	configData := `{"StreamConfig":{},"State":{"Running":false,"Paused":false,"Restarting":false,"OOMKilled":false,"RemovalInProgress":false,"Dead":false,"Pid":75542,"ExitCode":0,"Error":"","StartedAt":"0001-01-01T00:00:00Z","FinishedAt":"0001-01-01T00:00:00Z","Health":null},"ID":"badb01badb01badb01badb01badb01badb01badb01badb01badb01badb01badb","Created":"2018-07-29T13:03:15.475112279Z","Managed":false,"Path":"docker-entrypoint.sh","Args":["bash"],"Config":{"Hostname":"badb01badb01","Domainname":"","User":"","AttachStdin":true,"AttachStdout":true,"AttachStderr":true,"Tty":true,"OpenStdin":true,"StdinOnce":true,"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","_BASH_GPG_KEY=7C0135FB088AAF6C66C650B9BB5869F064EA74AB","_BASH_VERSION=4.4","_BASH_PATCH_LEVEL=18","_BASH_LATEST_PATCH=19"],"Cmd":["bash"],"ArgsEscaped":true,"Image":"bash","Volumes":null,"WorkingDir":"","Entrypoint":["docker-entrypoint.sh"],"OnBuild":null,"Labels":{}},"Image":"sha256:59507b30b48ad1faa1fa804b635b1fe0d17c60315722d622d1ed89ca1481192b","NetworkSettings":{"Bridge":"","SandboxID":"87ca3fc53893f74df55a4088f0bcafd30184b7509235cf481b8d3623408ca186","HairpinMode":false,"LinkLocalIPv6Address":"","LinkLocalIPv6PrefixLen":0,"Networks":{"bridge":{"IPAMConfig":null,"Links":null,"Aliases":null,"NetworkID":"988689c32445827be3ddec799bc154d164275e445c14cbd7dd78136e1627bcc5","EndpointID":"b1c0eb4ac7dc1729f49150072ce0b2dbb3d9009b951ef2456b9a52ea52ff767e","Gateway":"172.17.0.1","IPAddress":"172.17.0.2","IPPrefixLen":16,"IPv6Gateway":"","GlobalIPv6Address":"","GlobalIPv6PrefixLen":0,"MacAddress":"02:42:ac:11:00:02","DriverOpts":null,"IPAMOperational":false}},"Service":null,"Ports":{},"SandboxKey":"/var/run/docker/netns/87ca3fc53893","SecondaryIPAddresses":null,"SecondaryIPv6Addresses":null,"IsAnonymousEndpoint":true,"HasSwarmEndpoint":false},"LogPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf-json.log","Name":"/dreamy_volhard","Driver":"overlay2","OS":"linux","MountLabel":"","ProcessLabel":"","RestartCount":0,"HasBeenStartedBefore":true,"HasBeenManuallyStopped":false,"MountPoints":{},"SecretReferences":null,"ConfigReferences":null,"AppArmorProfile":"docker-default","HostnamePath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/hostname","HostsPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/hosts","ShmPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/mounts/shm","ResolvConfPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/resolv.conf","SeccompProfile":"","NoNewPrivileges":false}`
	writeFile(t, configFilename, ([]byte)(configData))

	sample := &perf.Sample{
		SampleID: perf.SampleID{
			Time: uint64(sys.CurrentMonotonicRaw()),
		},
	}
	data := expression.FieldValueMap{
		"newname": configFilename,
	}
	setSampleRawData(sample, data)
	dm.handleRename(0, sample)

	info := sensor.ContainerCache.LookupContainer(containerID, false)
	if assert.NotNil(t, info) {
		assert.Equal(t, ContainerStateCreated, info.State)
	}

	changes := map[ContainerState]string{
		ContainerStateRestarting: `{"StreamConfig":{},"State":{"Running":false,"Paused":false,"Restarting":true,"OOMKilled":false,"RemovalInProgress":false,"Dead":false,"Pid":75542,"ExitCode":0,"Error":"","StartedAt":"2018-07-29T10:28:00Z","FinishedAt":"0001-01-01T00:00:00Z","Health":null},"ID":"badb01badb01badb01badb01badb01badb01badb01badb01badb01badb01badb","Created":"2018-07-29T13:03:15.475112279Z","Managed":false,"Path":"docker-entrypoint.sh","Args":["bash"],"Config":{"Hostname":"badb01badb01","Domainname":"","User":"","AttachStdin":true,"AttachStdout":true,"AttachStderr":true,"Tty":true,"OpenStdin":true,"StdinOnce":true,"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","_BASH_GPG_KEY=7C0135FB088AAF6C66C650B9BB5869F064EA74AB","_BASH_VERSION=4.4","_BASH_PATCH_LEVEL=18","_BASH_LATEST_PATCH=19"],"Cmd":["bash"],"ArgsEscaped":true,"Image":"bash","Volumes":null,"WorkingDir":"","Entrypoint":["docker-entrypoint.sh"],"OnBuild":null,"Labels":{}},"Image":"sha256:59507b30b48ad1faa1fa804b635b1fe0d17c60315722d622d1ed89ca1481192b","NetworkSettings":{"Bridge":"","SandboxID":"87ca3fc53893f74df55a4088f0bcafd30184b7509235cf481b8d3623408ca186","HairpinMode":false,"LinkLocalIPv6Address":"","LinkLocalIPv6PrefixLen":0,"Networks":{"bridge":{"IPAMConfig":null,"Links":null,"Aliases":null,"NetworkID":"988689c32445827be3ddec799bc154d164275e445c14cbd7dd78136e1627bcc5","EndpointID":"b1c0eb4ac7dc1729f49150072ce0b2dbb3d9009b951ef2456b9a52ea52ff767e","Gateway":"172.17.0.1","IPAddress":"172.17.0.2","IPPrefixLen":16,"IPv6Gateway":"","GlobalIPv6Address":"","GlobalIPv6PrefixLen":0,"MacAddress":"02:42:ac:11:00:02","DriverOpts":null,"IPAMOperational":false}},"Service":null,"Ports":{},"SandboxKey":"/var/run/docker/netns/87ca3fc53893","SecondaryIPAddresses":null,"SecondaryIPv6Addresses":null,"IsAnonymousEndpoint":true,"HasSwarmEndpoint":false},"LogPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf-json.log","Name":"/dreamy_volhard","Driver":"overlay2","OS":"linux","MountLabel":"","ProcessLabel":"","RestartCount":0,"HasBeenStartedBefore":true,"HasBeenManuallyStopped":false,"MountPoints":{},"SecretReferences":null,"ConfigReferences":null,"AppArmorProfile":"docker-default","HostnamePath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/hostname","HostsPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/hosts","ShmPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/mounts/shm","ResolvConfPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/resolv.conf","SeccompProfile":"","NoNewPrivileges":false}`,
		ContainerStateRunning:    `{"StreamConfig":{},"State":{"Running":true,"Paused":false,"Restarting":false,"OOMKilled":false,"RemovalInProgress":false,"Dead":false,"Pid":75542,"ExitCode":0,"Error":"","StartedAt":"2018-07-29T10:28:00Z","FinishedAt":"0001-01-01T00:00:00Z","Health":null},"ID":"badb01badb01badb01badb01badb01badb01badb01badb01badb01badb01badb","Created":"2018-07-29T13:03:15.475112279Z","Managed":false,"Path":"docker-entrypoint.sh","Args":["bash"],"Config":{"Hostname":"badb01badb01","Domainname":"","User":"","AttachStdin":true,"AttachStdout":true,"AttachStderr":true,"Tty":true,"OpenStdin":true,"StdinOnce":true,"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","_BASH_GPG_KEY=7C0135FB088AAF6C66C650B9BB5869F064EA74AB","_BASH_VERSION=4.4","_BASH_PATCH_LEVEL=18","_BASH_LATEST_PATCH=19"],"Cmd":["bash"],"ArgsEscaped":true,"Image":"bash","Volumes":null,"WorkingDir":"","Entrypoint":["docker-entrypoint.sh"],"OnBuild":null,"Labels":{}},"Image":"sha256:59507b30b48ad1faa1fa804b635b1fe0d17c60315722d622d1ed89ca1481192b","NetworkSettings":{"Bridge":"","SandboxID":"87ca3fc53893f74df55a4088f0bcafd30184b7509235cf481b8d3623408ca186","HairpinMode":false,"LinkLocalIPv6Address":"","LinkLocalIPv6PrefixLen":0,"Networks":{"bridge":{"IPAMConfig":null,"Links":null,"Aliases":null,"NetworkID":"988689c32445827be3ddec799bc154d164275e445c14cbd7dd78136e1627bcc5","EndpointID":"b1c0eb4ac7dc1729f49150072ce0b2dbb3d9009b951ef2456b9a52ea52ff767e","Gateway":"172.17.0.1","IPAddress":"172.17.0.2","IPPrefixLen":16,"IPv6Gateway":"","GlobalIPv6Address":"","GlobalIPv6PrefixLen":0,"MacAddress":"02:42:ac:11:00:02","DriverOpts":null,"IPAMOperational":false}},"Service":null,"Ports":{},"SandboxKey":"/var/run/docker/netns/87ca3fc53893","SecondaryIPAddresses":null,"SecondaryIPv6Addresses":null,"IsAnonymousEndpoint":true,"HasSwarmEndpoint":false},"LogPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf-json.log","Name":"/dreamy_volhard","Driver":"overlay2","OS":"linux","MountLabel":"","ProcessLabel":"","RestartCount":0,"HasBeenStartedBefore":true,"HasBeenManuallyStopped":false,"MountPoints":{},"SecretReferences":null,"ConfigReferences":null,"AppArmorProfile":"docker-default","HostnamePath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/hostname","HostsPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/hosts","ShmPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/mounts/shm","ResolvConfPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/resolv.conf","SeccompProfile":"","NoNewPrivileges":false}`,
		ContainerStateRemoving:   `{"StreamConfig":{},"State":{"Running":false,"Paused":false,"Restarting":false,"OOMKilled":false,"RemovalInProgress":true,"Dead":false,"Pid":75542,"ExitCode":0,"Error":"","StartedAt":"2018-07-29T10:28:00Z","FinishedAt":"0001-01-01T00:00:00Z","Health":null},"ID":"badb01badb01badb01badb01badb01badb01badb01badb01badb01badb01badb","Created":"2018-07-29T13:03:15.475112279Z","Managed":false,"Path":"docker-entrypoint.sh","Args":["bash"],"Config":{"Hostname":"badb01badb01","Domainname":"","User":"","AttachStdin":true,"AttachStdout":true,"AttachStderr":true,"Tty":true,"OpenStdin":true,"StdinOnce":true,"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","_BASH_GPG_KEY=7C0135FB088AAF6C66C650B9BB5869F064EA74AB","_BASH_VERSION=4.4","_BASH_PATCH_LEVEL=18","_BASH_LATEST_PATCH=19"],"Cmd":["bash"],"ArgsEscaped":true,"Image":"bash","Volumes":null,"WorkingDir":"","Entrypoint":["docker-entrypoint.sh"],"OnBuild":null,"Labels":{}},"Image":"sha256:59507b30b48ad1faa1fa804b635b1fe0d17c60315722d622d1ed89ca1481192b","NetworkSettings":{"Bridge":"","SandboxID":"87ca3fc53893f74df55a4088f0bcafd30184b7509235cf481b8d3623408ca186","HairpinMode":false,"LinkLocalIPv6Address":"","LinkLocalIPv6PrefixLen":0,"Networks":{"bridge":{"IPAMConfig":null,"Links":null,"Aliases":null,"NetworkID":"988689c32445827be3ddec799bc154d164275e445c14cbd7dd78136e1627bcc5","EndpointID":"b1c0eb4ac7dc1729f49150072ce0b2dbb3d9009b951ef2456b9a52ea52ff767e","Gateway":"172.17.0.1","IPAddress":"172.17.0.2","IPPrefixLen":16,"IPv6Gateway":"","GlobalIPv6Address":"","GlobalIPv6PrefixLen":0,"MacAddress":"02:42:ac:11:00:02","DriverOpts":null,"IPAMOperational":false}},"Service":null,"Ports":{},"SandboxKey":"/var/run/docker/netns/87ca3fc53893","SecondaryIPAddresses":null,"SecondaryIPv6Addresses":null,"IsAnonymousEndpoint":true,"HasSwarmEndpoint":false},"LogPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf-json.log","Name":"/dreamy_volhard","Driver":"overlay2","OS":"linux","MountLabel":"","ProcessLabel":"","RestartCount":0,"HasBeenStartedBefore":true,"HasBeenManuallyStopped":false,"MountPoints":{},"SecretReferences":null,"ConfigReferences":null,"AppArmorProfile":"docker-default","HostnamePath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/hostname","HostsPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/hosts","ShmPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/mounts/shm","ResolvConfPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/resolv.conf","SeccompProfile":"","NoNewPrivileges":false}`,
		ContainerStatePaused:     `{"StreamConfig":{},"State":{"Running":false,"Paused":true,"Restarting":false,"OOMKilled":false,"RemovalInProgress":false,"Dead":false,"Pid":75542,"ExitCode":0,"Error":"","StartedAt":"2018-07-29T10:28:00Z","FinishedAt":"0001-01-01T00:00:00Z","Health":null},"ID":"badb01badb01badb01badb01badb01badb01badb01badb01badb01badb01badb","Created":"2018-07-29T13:03:15.475112279Z","Managed":false,"Path":"docker-entrypoint.sh","Args":["bash"],"Config":{"Hostname":"badb01badb01","Domainname":"","User":"","AttachStdin":true,"AttachStdout":true,"AttachStderr":true,"Tty":true,"OpenStdin":true,"StdinOnce":true,"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","_BASH_GPG_KEY=7C0135FB088AAF6C66C650B9BB5869F064EA74AB","_BASH_VERSION=4.4","_BASH_PATCH_LEVEL=18","_BASH_LATEST_PATCH=19"],"Cmd":["bash"],"ArgsEscaped":true,"Image":"bash","Volumes":null,"WorkingDir":"","Entrypoint":["docker-entrypoint.sh"],"OnBuild":null,"Labels":{}},"Image":"sha256:59507b30b48ad1faa1fa804b635b1fe0d17c60315722d622d1ed89ca1481192b","NetworkSettings":{"Bridge":"","SandboxID":"87ca3fc53893f74df55a4088f0bcafd30184b7509235cf481b8d3623408ca186","HairpinMode":false,"LinkLocalIPv6Address":"","LinkLocalIPv6PrefixLen":0,"Networks":{"bridge":{"IPAMConfig":null,"Links":null,"Aliases":null,"NetworkID":"988689c32445827be3ddec799bc154d164275e445c14cbd7dd78136e1627bcc5","EndpointID":"b1c0eb4ac7dc1729f49150072ce0b2dbb3d9009b951ef2456b9a52ea52ff767e","Gateway":"172.17.0.1","IPAddress":"172.17.0.2","IPPrefixLen":16,"IPv6Gateway":"","GlobalIPv6Address":"","GlobalIPv6PrefixLen":0,"MacAddress":"02:42:ac:11:00:02","DriverOpts":null,"IPAMOperational":false}},"Service":null,"Ports":{},"SandboxKey":"/var/run/docker/netns/87ca3fc53893","SecondaryIPAddresses":null,"SecondaryIPv6Addresses":null,"IsAnonymousEndpoint":true,"HasSwarmEndpoint":false},"LogPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf-json.log","Name":"/dreamy_volhard","Driver":"overlay2","OS":"linux","MountLabel":"","ProcessLabel":"","RestartCount":0,"HasBeenStartedBefore":true,"HasBeenManuallyStopped":false,"MountPoints":{},"SecretReferences":null,"ConfigReferences":null,"AppArmorProfile":"docker-default","HostnamePath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/hostname","HostsPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/hosts","ShmPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/mounts/shm","ResolvConfPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/resolv.conf","SeccompProfile":"","NoNewPrivileges":false}`,
		ContainerStateExited:     `{"StreamConfig":{},"State":{"Running":false,"Paused":false,"Restarting":false,"OOMKilled":false,"RemovalInProgress":false,"Dead":false,"Pid":75542,"ExitCode":0,"Error":"","StartedAt":"2018-07-29T10:28:00Z","FinishedAt":"2018-07-29T10:29:00Z","Health":null},"ID":"badb01badb01badb01badb01badb01badb01badb01badb01badb01badb01badb","Created":"2018-07-29T13:03:15.475112279Z","Managed":false,"Path":"docker-entrypoint.sh","Args":["bash"],"Config":{"Hostname":"badb01badb01","Domainname":"","User":"","AttachStdin":true,"AttachStdout":true,"AttachStderr":true,"Tty":true,"OpenStdin":true,"StdinOnce":true,"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","_BASH_GPG_KEY=7C0135FB088AAF6C66C650B9BB5869F064EA74AB","_BASH_VERSION=4.4","_BASH_PATCH_LEVEL=18","_BASH_LATEST_PATCH=19"],"Cmd":["bash"],"ArgsEscaped":true,"Image":"bash","Volumes":null,"WorkingDir":"","Entrypoint":["docker-entrypoint.sh"],"OnBuild":null,"Labels":{}},"Image":"sha256:59507b30b48ad1faa1fa804b635b1fe0d17c60315722d622d1ed89ca1481192b","NetworkSettings":{"Bridge":"","SandboxID":"87ca3fc53893f74df55a4088f0bcafd30184b7509235cf481b8d3623408ca186","HairpinMode":false,"LinkLocalIPv6Address":"","LinkLocalIPv6PrefixLen":0,"Networks":{"bridge":{"IPAMConfig":null,"Links":null,"Aliases":null,"NetworkID":"988689c32445827be3ddec799bc154d164275e445c14cbd7dd78136e1627bcc5","EndpointID":"b1c0eb4ac7dc1729f49150072ce0b2dbb3d9009b951ef2456b9a52ea52ff767e","Gateway":"172.17.0.1","IPAddress":"172.17.0.2","IPPrefixLen":16,"IPv6Gateway":"","GlobalIPv6Address":"","GlobalIPv6PrefixLen":0,"MacAddress":"02:42:ac:11:00:02","DriverOpts":null,"IPAMOperational":false}},"Service":null,"Ports":{},"SandboxKey":"/var/run/docker/netns/87ca3fc53893","SecondaryIPAddresses":null,"SecondaryIPv6Addresses":null,"IsAnonymousEndpoint":true,"HasSwarmEndpoint":false},"LogPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf-json.log","Name":"/dreamy_volhard","Driver":"overlay2","OS":"linux","MountLabel":"","ProcessLabel":"","RestartCount":0,"HasBeenStartedBefore":true,"HasBeenManuallyStopped":false,"MountPoints":{},"SecretReferences":null,"ConfigReferences":null,"AppArmorProfile":"docker-default","HostnamePath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/hostname","HostsPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/hosts","ShmPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/mounts/shm","ResolvConfPath":"/var/lib/docker/containers/98ca8f25f1641d4b4f3c2cb360eaf8763abbf000541679f44c5c782eadbbfdcf/resolv.conf","SeccompProfile":"","NoNewPrivileges":false}`,
	}
	for state, change := range changes {
		writeFile(t, configFilename, ([]byte)(change))
		dm.handleRename(0, sample)

		info = sensor.ContainerCache.LookupContainer(containerID, false)
		if assert.NotNil(t, info) {
			assert.Equal(t, state, info.State)
		}
	}

	// This rename should be ignored
	lastState := info.State
	data["newname"] = filepath.Join(sensor.tracingDir, containerID, "config.v2.json")
	writeFile(t, data["newname"].(string), ([]byte)(configData))
	setSampleRawData(sample, data)
	dm.handleRename(0, sample)

	info = sensor.ContainerCache.LookupContainer(containerID, false)
	if assert.NotNil(t, info) {
		assert.Equal(t, lastState, info.State)
	}

	// This unlink should be ignored
	data = expression.FieldValueMap{
		"pathname": filepath.Join(sensor.tracingDir, containerID, "config.v2.json"),
	}
	setSampleRawData(sample, data)
	dm.handleUnlink(0, sample)

	info = sensor.ContainerCache.LookupContainer(containerID, false)
	assert.NotNil(t, info)

	// Test handleUnlink
	data = expression.FieldValueMap{
		"pathname": configFilename,
	}
	setSampleRawData(sample, data)
	dm.handleUnlink(0, sample)

	info = sensor.ContainerCache.LookupContainer(containerID, false)
	assert.Nil(t, info)

	// Catch a couple of other minor things
	er := ErrorReader{}
	err := dm.processDockerConfig(perf.SampleID{}, configFilename, er)
	assert.Error(t, err)

	br := bytes.NewReader(([]byte)("this is not json and should fail to unmarshal"))
	err = dm.processDockerConfig(perf.SampleID{}, configFilename, br)
	assert.Error(t, err)
}

type ErrorReader struct{}

func (e ErrorReader) Read(p []byte) (n int, err error) {
	err = unix.EBADF
	return
}
