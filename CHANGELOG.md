## 0.15.0-alpha (June 21, 2018)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  * Fall back to user land filters if kernel filters fail to set ([#205](https://github.com/Happyholic1203/capsule8/pull/205))

IMPROVEMENTS:

  * Refactor to use internal AST rather than protobuf AST ([#218](https://github.com/Happyholic1203/capsule8/pull/218))
  * Update kernel check script with colors and config ([#202](https://github.com/Happyholic1203/capsule8/pull/202))
  * Add an example of a negative filter ([#209](https://github.com/Happyholic1203/capsule8/pull/209))

BUG FIXES:

  * Add missing nil check for systems without Docker installed ([#220](https://github.com/Happyholic1203/capsule8/pull/220))
  * Separate startup scans from cache/monitor instantiation ([#217](https://github.com/Happyholic1203/capsule8/pull/217))

## 0.14.0-alpha (May 30, 2018)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  * Add a command-line flag to print version information and exit ([#213](https://github.com/Happyholic1203/capsule8/pull/213))
  * Add optional hooks to the telemetry service ([#212](https://github.com/Happyholic1203/capsule8/pull/212))

IMPROVEMENTS:

  * Remove dead code that is no longer used anywhere ([#211](https://github.com/Happyholic1203/capsule8/pull/211))

BUG FIXES:

  * Make sure that ProcessEvent.fork_child_id gets set properly ([#214](https://github.com/Happyholic1203/capsule8/pull/214))

## 0.13.0-alpha (May 24, 2018)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  * Surface hardware/software performance events through the sensor API ([#203](https://github.com/Happyholic1203/capsule8/pull/203))

IMPROVEMENTS:

  None

BUG FIXES:

  * Do not use do_execveat_common for process exec monitoring ([#208](https://github.com/Happyholic1203/capsule8/pull/208))
  * Set the ProcessID for a task existing when the sensor starts up ([#207](https://github.com/Happyholic1203/capsule8/pull/207))
  * Fix a problem with subscribing to PROCESS_EVENT_TYPE_UPDATE events ([#206](https://github.com/Happyholic1203/capsule8/pull/206))

## 0.12.0-alpha (May 11, 2018)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  * Add a script to check whether the sensor is likely to function or not ([#199](https://github.com/Happyholic1203/capsule8/pull/199))
  * Update cgroup monitoring to work with all supported kernels ([#198](https://github.com/Happyholic1203/capsule8/pull/198))

IMPROVEMENTS:

  * Document known issues and update minimum system requirements ([#201](https://github.com/Happyholic1203/capsule8/pull/201))
  * Use `syscalls/sys_{enter,exit}` if `raw_syscalls` versions are not present ([#200](https://github.com/Happyholic1203/capsule8/pull/200))
  * Handle out of order fork events on kernels older than 3.9 ([#191](https://github.com/Happyholic1203/capsule8/pull/191))
  * Add Pete to code owners ([#195](https://github.com/Happyholic1203/capsule8/pull/195))

BUG FIXES:

  * Fix copying of mutex values ([#197](https://github.com/Happyholic1203/capsule8/pull/197))
  * Remove duplicate `__cgroup_procs_write` kprobe ([#196](https://github.com/Happyholic1203/capsule8/pull/196))

## 0.11.0-alpha (Apr 26, 2018)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  * Send `PROCESS_EVENT_TYPE_UPDATE` events when cwd for a task changes ([#188](https://github.com/Happyholic1203/capsule8/pull/188))

IMPROVEMENTS:

  * Generate process events for subscriptions from process cache events ([#182](https://github.com/Happyholic1203/capsule8/pull/182))
  * Update vendored gRPC dependency to [1.11.2](https://github.com/grpc/grpc-go/releases/tag/v1.11.2) ([#181](https://github.com/Happyholic1203/capsule8/pull/181))
  * Move subscriptions-cli's main.go to where Makefile will find it ([#179](https://github.com/Happyholic1203/capsule8/pull/179))

BUG FIXES:
  * Simplify golint go get command in circle ci pipeline ([#192](https://github.com/Happyholic1203/capsule8/pull/192))
  * Child tasks should inherit credentials from parent tasks ([#189](https://github.com/Happyholic1203/capsule8/pull/189))
  * Manually clone go lint in circle ci ([#190](https://github.com/Happyholic1203/capsule8/pull/190))
  * Fix the offsets for decoding credentials from the kernel ([#185](https://github.com/Happyholic1203/capsule8/pull/185))

## 0.8.1-alpha (Apr 11, 2018)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  None

IMPROVEMENTS:

  None

BUG FIXES:

  * Fix the offsets for decoding credentials from the kernel ([#186](https://github.com/Happyholic1203/capsule8/pull/186))

Notes:

  This release is on the `release-0.8` branch

## 0.10.1-alpha (Apr 9, 2018)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  None

IMPROVEMENTS:

  * Update vendored gRPC dependency to [1.11.2](https://github.com/grpc/grpc-go/releases/tag/v1.11.2) ([#181](https://github.com/Happyholic1203/capsule8/pull/181))
  * Move subscriptions-cli's main.go to where Makefile will find it ([#179](https://github.com/Happyholic1203/capsule8/pull/179))

BUG FIXES:

  None

## 0.10.0-alpha (Apr 5, 2018)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  None

IMPROVEMENTS:

  * Stabilize ProcessId by using boot id, pid, and start time ([#172](https://github.com/Happyholic1203/capsule8/pull/172))

BUG FIXES:

  * Fix mounts parsing ([#177](https://github.com/Happyholic1203/capsule8/pull/177))

## 0.9.0-alpha (Mar 22, 2018)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  * Add an event sent to a client after processing its subscription ([#169](https://github.com/Happyholic1203/capsule8/pull/169))

IMPROVEMENTS:

  * Filter docker/oci config from events based on ContainerView in subscriptions ([#171](https://github.com/Happyholic1203/capsule8/pull/171))
  * Remove the use of channels in EventMonitor ([#167](https://github.com/Happyholic1203/capsule8/pull/167))
  * Remove the use of the streams package from the sensor ([#163](https://github.com/Happyholic1203/capsule8/pull/163))
  * Add example CLI for easily subscribing to telemetry ([#162](https://github.com/Happyholic1203/capsule8/pull/162))

BUG FIXES:

  * Spell guarantee correctly in system docs ([#173](https://github.com/Happyholic1203/capsule8/pull/173))
  * Fix an intermittent problem with EventMonitor.Close hanging ([#170](https://github.com/Happyholic1203/capsule8/pull/170))
  * Update vendor submodule to latest hash ([#168](https://github.com/Happyholic1203/capsule8/pull/168))
  * Fix benchmark crash on startup introduced in #157 ([#166](https://github.com/Happyholic1203/capsule8/pull/166))

## 0.8.0-alpha (Mar 7, 2018)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  * Add support for running the sensor on CentOS 6 ([#145](https://github.com/Happyholic1203/capsule8/pull/145))

IMPROVEMENTS:

  * Fix the architecture image to reflect telemetry service existing in pkg/sensor ([#161](https://github.com/Happyholic1203/capsule8/pull/161))
  * Add link in README to api protocol docs ([#160](https://github.com/Happyholic1203/capsule8/pull/160))
  * Add documentation ([#149](https://github.com/Happyholic1203/capsule8/pull/149))
  * Use monotonic clocks for computing timeouts ([#156](https://github.com/Happyholic1203/capsule8/pull/156))
  * Add support for counter-based hardware perf events to EventMonitor ([#155](https://github.com/Happyholic1203/capsule8/pull/155))
  * Remove inotify support for triggers and recursive directory monitoring ([#158](https://github.com/Happyholic1203/capsule8/pull/158))
  * Add api proto definitions telemetry docs ([#159](https://github.com/Happyholic1203/capsule8/pull/159))
  * Make the process cache better handle out-of-order events ([#151](https://github.com/Happyholic1203/capsule8/pull/151))

BUG FIXES:

  * Update benchmark test to latest changes of subscription logic ([#157](https://github.com/Happyholic1203/capsule8/pull/157))
  * Do not emit telemetry events for samples coming from the sensor itself ([#152](https://github.com/Happyholic1203/capsule8/pull/152))
  * Handle timestamp type in expression package ([#154](https://github.com/Happyholic1203/capsule8/pull/154))

## 0.7.0-alpha (Feb 21, 2018)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  None

IMPROVEMENTS:

  * Update vendoring with grpc version 1.9.2 ([#147](https://github.com/Happyholic1203/capsule8/pull/147))
  * Add container info to cache side channel example ([#137](https://github.com/Happyholic1203/capsule8/pull/137))
  * Add 1-second timeout and blocking to grpc.Dial in Telemetry Client ([#136](https://github.com/Happyholic1203/capsule8/pull/136))
  * Improve handling of sample timestamps in EventMonitor ([#134](https://github.com/Happyholic1203/capsule8/pull/134))
  * Updated example telemetry client to enable printing events as JSON ([#143](https://github.com/Happyholic1203/capsule8/pull/143))

BUG FIXES:

  * Fix problems found with the release of Go 1.10 ([#148](https://github.com/Happyholic1203/capsule8/pull/148))
  * Wait for the pollLoop to finish in Instance.Close ([#144](https://github.com/Happyholic1203/capsule8/pull/144))
  * Protect the stream from multiple close calls ([#146](https://github.com/Happyholic1203/capsule8/pull/146))
  * Fix TestSubdirs so that it stops failing so often ([#141](https://github.com/Happyholic1203/capsule8/pull/141))
  * Add a missing Unlock call in mapTaskCache.LookupTaskAndLeader ([#139](https://github.com/Happyholic1203/capsule8/pull/139))

## 0.6.1-alpha (Feb 8, 2018)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  None

IMPROVEMENTS:

  None

BUG FIXES:

  * Add a missing Unlock call in mapTaskCache.LookupTaskAndLeader ([#139](https://github.com/Happyholic1203/capsule8/pull/139))

## 0.6.0-alpha (Feb 7, 2018)

BACKWARDS INCOMPATIBILITIES:

  * `oci_config_json` is no longer set in telemetry after being temporarily disabled in [#132](https://github.com/Happyholic1203/capsule8/pull/132)

FEATURES:

  * Expose task TGID in telemetry events ([#130](https://github.com/Happyholic1203/capsule8/pull/130))
  * Add a public API to the container cache and expose it all ([#129](https://github.com/Happyholic1203/capsule8/pull/129))
  * Make Sensor.processCache public to expose its public methods ([#120](https://github.com/Happyholic1203/capsule8/pull/120))
  * Scan the proc filesystem to populate the task cache on startup ([#127](https://github.com/Happyholic1203/capsule8/pull/127))
  * Update the process cache API to be more friendly to lookups ([#126](https://github.com/Happyholic1203/capsule8/pull/126))
  * Add a monitor to watch for runc managed containers ([#118](https://github.com/Happyholic1203/capsule8/pull/118) - this was later disabled by [#132](https://github.com/Happyholic1203/capsule8/pull/132))

IMPROVEMENTS:

  * Guard against infinite loops in container id/info lookup ([#124](https://github.com/Happyholic1203/capsule8/pull/124))
  * Update CircleCI and require lint cleanliness going forward ([#121](https://github.com/Happyholic1203/capsule8/pull/121))

BUG FIXES:

  * Temporarily disable the OCI monitor ([#132](https://github.com/Happyholic1203/capsule8/pull/132))
  * Don't bail out of scanning /proc when processes disappear during the scan ([#131](https://github.com/Happyholic1203/capsule8/pull/131))
  * Use a kprobe instead of Ubuntu-specific `fs/do_sys_open` tracepoint ([#128](https://github.com/Happyholic1203/capsule8/pull/128))
  * Remove looping behavior from LookupLeader ([#125](https://github.com/Happyholic1203/capsule8/pull/125))
  * Fix copy/paste error in rewrite of CreateModeMask ([#123](https://github.com/Happyholic1203/capsule8/pull/123))
  * Clean the leaking of file descriptors with inotify ([#117](https://github.com/Happyholic1203/capsule8/pull/117))
  * Flip stopped flag while holding exclusive lock ([#116](https://github.com/Happyholic1203/capsule8/pull/116))

## 0.5.0-alpha (Jan 24, 2018)

BACKWARDS INCOMPATIBILITIES:

  * `CONTAINER_EVENT_TYPE_UPDATED` is added to the telemetry event proto definition in [#108](https://github.com/Happyholic1203/capsule8/pull/108)

FEATURES:

  * Allow client to specify all or none service failure behaviour ([#112](https://github.com/Happyholic1203/capsule8/pull/112))
  * Surface process credentials in telemetry events ([#110](https://github.com/Happyholic1203/capsule8/pull/110))
  * Overhaul container caching and related telemetry events ([#108](https://github.com/Happyholic1203/capsule8/pull/108))

IMPROVEMENTS:

  * Gracefully stop services during all or nothing termination ([#115](https://github.com/Happyholic1203/capsule8/pull/115))
  * Update naming of fields in Credentials struct to comply with Go naming conventions ([#114](https://github.com/Happyholic1203/capsule8/pull/114))
  * Don't wait for telemetry clients to finish before stopping ([#109](https://github.com/Happyholic1203/capsule8/pull/109))
  * Change expression.FieldTypeMap to map[string]int32 ([#107](https://github.com/Happyholic1203/capsule8/pull/107))

BUG FIXES:

  * Ensure that all expected keys for container events are always set ([#113](https://github.com/Happyholic1203/capsule8/pull/113))

## 0.4.0-alpha (Jan 11, 2018)

BACKWARDS INCOMPATIBILITIES:

  * In the api definitions what had formerly been called `Event`s are now `TelemetryEvent`s [#99](https://github.com/Happyholic1203/capsule8/pull/99)

FEATURES:

  * Add cache side channel detection ([#100](https://github.com/Happyholic1203/capsule8/pull/100))
  * Add preliminary uprobe support ([#95](https://github.com/Happyholic1203/capsule8/pull/95))

IMPROVEMENTS:

  * Perf event attr exclusive fails silently on most distro kernels, don't use it ([#103](https://github.com/Happyholic1203/capsule8/pull/103))
  * Reduce perf/event_group event attr fixups ([#101](https://github.com/Happyholic1203/capsule8/pull/101))
  * Rename event to telemetry event ([#99](https://github.com/Happyholic1203/capsule8/pull/99))
  * Add meltdown detector example that uses the Telemetry API ([#98](https://github.com/Happyholic1203/capsule8/pull/98))
  * Add meltdown detector example ([#97](https://github.com/Happyholic1203/capsule8/pull/97))
  * Add gRPC gateway reverse proxies for Telemetry Service ([#96](https://github.com/Happyholic1203/capsule8/pull/96))

BUG FIXES:

  None

## 0.3.0-alpha (Dec 29, 2017)

BACKWARDS INCOMPATIBILITIES:

  * Configuration variables were renamed and some were removed in [#92](https://github.com/Happyholic1203/capsule8/pull/92)

FEATURES:

  * Allow TLS configuration for telemetry server ([#91](https://github.com/Happyholic1203/capsule8/pull/91))
  * Enable syscall args in API and in functional test ([#86](https://github.com/Happyholic1203/capsule8/pull/86))
  * Use a kprobe to track process command-line information ([#80](https://github.com/Happyholic1203/capsule8/pull/80))
  * Properly retrieve syscall arguments and add support for filtering on them ([#78](https://github.com/Happyholic1203/capsule8/pull/78))

IMPROVEMENTS:

  * Refactor CI to use CircleCI 2.0 ([#90](https://github.com/Happyholic1203/capsule8/pull/90))
  * Apply consistency to network endpoint address naming ([#92](https://github.com/Happyholic1203/capsule8/pull/92))
  * Clean up all golint warnings ([#89](https://github.com/Happyholic1203/capsule8/pull/89))
  * Make the process info cache size configurable ([#88](https://github.com/Happyholic1203/capsule8/pull/88))
  * Move contributing guidelines and issue template into .github ([#87](https://github.com/Happyholic1203/capsule8/pull/87))
  * Add issue template ([#84](https://github.com/Happyholic1203/capsule8/pull/84))
  * Default to https for vendor submodule ([#83](https://github.com/Happyholic1203/capsule8/pull/83))
  * Update vendoring of api and aws tools ([#79](https://github.com/Happyholic1203/capsule8/pull/79))
  * Add make target to run the sensor in the background ([#73](https://github.com/Happyholic1203/capsule8/pull/73))

BUG FIXES:

  * Fix regenerating code from .protos ([#85](https://github.com/Happyholic1203/capsule8/pull/85))
  * Remove accidentally committed binary ([#81](https://github.com/Happyholic1203/capsule8/pull/81))
  * Remove accidentally committed binaries ([#77](https://github.com/Happyholic1203/capsule8/pull/77))

## 0.2.1-alpha (Dec 29, 2017)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  None

IMPROVEMENTS:

  * Add make target to run the sensor in the background ([#73](https://github.com/Happyholic1203/capsule8/pull/73))

BUG FIXES:

  * Remove accidentally committed binary and update vendoring ([#82](https://github.com/Happyholic1203/capsule8/pull/82))
  * Remove accidentally committed binaries ([#77](https://github.com/Happyholic1203/capsule8/pull/77))

## 0.2.0-alpha (Dec 14, 2017)

BACKWARDS INCOMPATIBILITIES:

  * Event filtering changed in [#55](https://github.com/Happyholic1203/capsule8/pull/55) with updates to the underlying API definitions.

FEATURES:

  * Default to system wide event monitor even when running in container ([#62](https://github.com/Happyholic1203/capsule8/pull/62))
  * Use single event monitor for all subscriptions ([#61](https://github.com/Happyholic1203/capsule8/pull/61))
  * Use expression filtering from API for event based filtering ([#55](https://github.com/Happyholic1203/capsule8/pull/55))
  * Add process credential tracking ([#57](https://github.com/Happyholic1203/capsule8/pull/57))

IMPROVEMENTS:

  * Add copyright statement and license to all source files ([#76](https://github.com/Happyholic1203/capsule8/pull/76))
  * Add kinesis telemetry ingestor example ([#71](https://github.com/Happyholic1203/capsule8/pull/71))
  * Refactor network functional tests ([#72](https://github.com/Happyholic1203/capsule8/pull/72))
  * Import telemetry API definitions that used to be vendored, directly to this respository ([#70](https://github.com/Happyholic1203/capsule8/pull/70))
  * Create docker image for functional testing ([#63](https://github.com/Happyholic1203/capsule8/pull/63))
  * Separate our sensor start and stop logic ([#68](https://github.com/Happyholic1203/capsule8/pull/68))
  * Update service constructors to pass sensor reference ([#66](https://github.com/Happyholic1203/capsule8/pull/66))
  * Add functional testing for network, syscall and kernelcall events ([#54](https://github.com/Happyholic1203/capsule8/pull/54))
  * Update expression use to programmatically create expression trees ([#60](https://github.com/Happyholic1203/capsule8/pull/60))
  * Improve and add to unit testing of /proc/[pid]/cgroup parsing ([#53](https://github.com/Happyholic1203/capsule8/pull/53))
  * Remove sleeps in functional tests to make them more demanding ([#52](https://github.com/Happyholic1203/capsule8/pull/52))
  * Use functions to configure the new event monitor input ([#37](https://github.com/Happyholic1203/capsule8/pull/37))
  * Refactoring to set up the single event monitor work ([#31](https://github.com/Happyholic1203/capsule8/pull/31))

BUG FIXES:

  * Fix identification of container IDs in a kubernetes environment ([#69](https://github.com/Happyholic1203/capsule8/pull/69))
  * Fix duplicate events off by one error ([#64](https://github.com/Happyholic1203/capsule8/pull/64))
  * Fix container information identification from Docker versions before 1.13.0 ([#56](https://github.com/Happyholic1203/capsule8/pull/56))
  * Fix 'args' type information handling in various kernel versions ([#51](https://github.com/Happyholic1203/capsule8/pull/51))


## 0.1.1-alpha (Dec 14, 2017)

BACKWARDS INCOMPATIBILITIES:

  None

FEATURES:

  None

IMPROVEMENTS:

  None

BUG FIXES:

  * Fix container information identification from Docker versions before 1.13.0 ([#65](https://github.com/Happyholic1203/capsule8/pull/65))
  * Fix identification of container IDs in a kubernetes environment ([#67](https://github.com/Happyholic1203/capsule8/pull/67))
