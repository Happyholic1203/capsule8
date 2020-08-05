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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Happyholic1203/capsule8/pkg/config"
	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"
	"github.com/Happyholic1203/capsule8/pkg/sys/proc"
	"github.com/Happyholic1203/capsule8/pkg/sys/proc/procfs"

	"github.com/golang/glog"

	"golang.org/x/sys/unix"
)

type newSensorOptions struct {
	runtimeDir            string
	supportDir            string
	perfEventDir          string
	tracingDir            string
	dockerContainerDir    string
	ociContainerDir       string
	procFS                proc.FileSystem
	eventSourceController perf.EventSourceController
	cleanupFuncs          []func()
	cgroupNames           []string
	sensorID              string
	ringBufferNumPages    int
}

// NewSensorOption is used to implement optional arguments for NewSensor.
// It must be exported, but it is not typically used directly.
type NewSensorOption func(*newSensorOptions)

// WithRuntimeDir is used to set the runtime state directory to use for the
// sensor.
func WithRuntimeDir(runtimeDir string) NewSensorOption {
	return func(o *newSensorOptions) {
		o.runtimeDir = runtimeDir
	}
}

// WithSupportDir is used to set the support directory to use for the sensor.
// The support directory contains files that the sensor uses for various
// operations (e.g., kernel struct offset table)
func WithSupportDir(supportDir string) NewSensorOption {
	return func(o *newSensorOptions) {
		o.supportDir = supportDir
	}
}

// WithSensorID is used to define a specific sensor ID.
func WithSensorID(id string) NewSensorOption {
	return func(o *newSensorOptions) {
		o.sensorID = id
	}
}

// WithDockerContainerDir is used to set the directory to monitor for Docker
// container activity.
func WithDockerContainerDir(dockerContainerDir string) NewSensorOption {
	return func(o *newSensorOptions) {
		o.dockerContainerDir = dockerContainerDir
	}
}

// WithOciContainerDir is used to set the directory to monitor for OCI
// container activity.
func WithOciContainerDir(ociContainerDir string) NewSensorOption {
	return func(o *newSensorOptions) {
		o.ociContainerDir = ociContainerDir
	}
}

// WithProcFileSystem is used to set the proc.FileSystem to use. The system
// default will be used if one is not specified.
func WithProcFileSystem(procFS proc.FileSystem) NewSensorOption {
	return func(o *newSensorOptions) {
		o.procFS = procFS
	}
}

// WithEventSourceController is used to set the perf.EventSourceController to
// use. This is not used by the sensor itself, but passed through when a new
// EventMonitor is created.
func WithEventSourceController(controller perf.EventSourceController) NewSensorOption {
	return func(o *newSensorOptions) {
		o.eventSourceController = controller
	}
}

// WithPerfEventDir is used to set an optional directory to use for monitoring
// groups. This should only be necessary if the perf_event cgroup is not
// mounted in the usual location.
func WithPerfEventDir(perfEventDir string) NewSensorOption {
	return func(o *newSensorOptions) {
		o.perfEventDir = perfEventDir
	}
}

// WithTracingDir is used to set an alternate mountpoint to use for managing
// tracepoints, kprobes, and uprobes.
func WithTracingDir(tracingDir string) NewSensorOption {
	return func(o *newSensorOptions) {
		o.tracingDir = tracingDir
	}
}

// WithCleanupFunc is used to register a cleanup function that will be called
// when the sensor is stopped. Multiple cleanup functions may be registered,
// and will be called in the reverse order in which the were registered.
func WithCleanupFunc(cleanupFunc func()) NewSensorOption {
	return func(o *newSensorOptions) {
		o.cleanupFuncs = append(o.cleanupFuncs, cleanupFunc)
	}
}

// WithCgroupName configures a cgroup name to be monitored.
func WithCgroupName(cgroupName string) NewSensorOption {
	return func(o *newSensorOptions) {
		o.cgroupNames = append(o.cgroupNames, cgroupName)
	}
}

// WithRingBufferNumPages sets the number of memory pages to use for event
// monitoring ring buffers.
func WithRingBufferNumPages(ringBufferNumPages int) NewSensorOption {
	return func(o *newSensorOptions) {
		o.ringBufferNumPages = ringBufferNumPages
	}
}

// Number of random bytes to generate for Sensor Id
const sensorIDLengthBytes = 32

// StructField represents the offset and size of a kernel struct field. If the
// field offset information is not known, both size and offset will be 0.
type StructField struct {
	Offset int `json:"offset"`
	Size   int `json:"size"`
}

// Sensor represents the state of a sensor instance.
type Sensor struct {
	// Unique Id for this sensor. Sensor Ids are ephemeral.
	ID string

	// Sensor-unique event sequence number. Each event sent from this
	// sensor to any subscription has a unique sequence number for the
	// indicated sensor Id.
	sequenceNumber uint64

	// Record the value of CLOCK_MONOTONIC_RAW when the sensor starts.
	// All event monotimes are relative to this value.
	bootMonotimeNanos int64

	// Metrics counters for this sensor
	Metrics MetricsCounters

	// If temporary fs mounts are made at startup, they're stored here.
	perfEventDir string
	tracingDir   string

	// A sensor-global event monitor that is used for events to aid in
	// caching process information
	monitor atomic.Value // *perf.EventMonitor

	// A reference to the host proc filesystem in use.
	ProcFS proc.FileSystem

	// A lookup table of available kernel symbols. The key is the symbol
	// name as would be used with RegisterKprobe. The value is the actual
	// symbol that should be used, which is normally the same, but can
	// sometimes differ due to compiler name mangling.
	kallsyms map[string]string

	// Per-sensor caches and monitors
	ProcessCache   *ProcessInfoCache
	ContainerCache *ContainerCache
	FileMonitor    *FileMonitor
	dockerMonitor  *dockerMonitor
	ociMonitor     *ociMonitor

	// Mapping of event ids to subscriptions. It is used to figure out
	// which subscriptions should be receiving events for a given eventid.
	// This is used for meta events like process, file, and container
	// events. This is not used for LostRecord events.
	eventMap *safeEventSinkMap

	// Mapping of subscription ids to subscriptions. It is used by
	// DispatchEventToAllSubscriptions, primarily to deliver LostRecord
	// events.
	subscriptionMap *safeSubscriptionMap

	// Used by syscall events to handle syscall enter events with
	// argument filters
	dummySyscallEventID    uint64
	dummySyscallEventCount int64

	// A reference to the event source controller in use.
	EventSourceController perf.EventSourceController

	// Kernel struct offsets determined at startup, referenced rarely
	dentryStructName        StructField // struct dentry :: d_name :: name (char *)
	dentryStructParent      StructField // struct dentry :: d_parent (struct dentry *)
	inodeSuperBlockOffset   StructField // struct inode :: i_sb (struct super_block	*)
	superblockMountsOffset  StructField // struct super_block :: s_mounts (struct list_head)
	mountMntInstanceOffset  StructField // struct mount :: mnt_instance (struct list_head)
	mountMntOffset          StructField // struct mount :: mnt (struct vfsmount)
	mountMntIDOffset        StructField // struct mount :: mnt_id (int)
	taskStructPID           StructField // struct task_struct :: pid (pid_t)
	taskStructTGID          StructField // struct task_struct :: tgid (pid_t)
	taskStructRealStartTime StructField // struct task_struct :: real_start_time (struct timespec)

	// Function signature information determined at startup
	fsnotifyParentHasPath bool

	// Runtime options configured during NewSensor, but not used until
	// later
	runtimeDir         string
	supportDir         string
	dockerContainerDir string
	ociContainerDir    string
	cgroupNames        []string
	ringBufferNumPages int

	// Cleanup functions to be run (in reverse order) when the sensor is
	// stopped.
	cleanupFuncs []func()
}

// NewSensor creates a new Sensor instance.
func NewSensor(options ...NewSensorOption) (*Sensor, error) {
	opts := newSensorOptions{
		runtimeDir:         config.Global.RunDir,
		supportDir:         config.Global.SupportDir,
		dockerContainerDir: config.Sensor.DockerContainerDir,
		ociContainerDir:    config.Sensor.OciContainerDir,
		cgroupNames:        config.Sensor.CgroupName,
		ringBufferNumPages: config.Sensor.RingBufferPages,
	}
	for _, option := range options {
		option(&opts)
	}

	if opts.procFS == nil {
		fs, err := procfs.NewFileSystem("")
		if err != nil {
			return nil, err
		}
		opts.procFS = fs.HostFileSystem()
		if opts.procFS == nil {
			return nil, errors.New("Cannot resolve host proc filesystem")
		}
	}
	if len(opts.perfEventDir) == 0 {
		opts.perfEventDir = opts.procFS.PerfEventDir()
	}
	if len(opts.tracingDir) == 0 {
		opts.tracingDir = opts.procFS.TracingDir()
	}
	if opts.sensorID == "" {
		randomBytes := make([]byte, sensorIDLengthBytes)
		rand.Read(randomBytes)
		opts.sensorID = hex.EncodeToString(randomBytes)
	}

	s := &Sensor{
		ID:                    opts.sensorID,
		bootMonotimeNanos:     sys.CurrentMonotonicRaw(),
		perfEventDir:          opts.perfEventDir,
		tracingDir:            opts.tracingDir,
		ProcFS:                opts.procFS,
		eventMap:              newSafeEventSinkMap(),
		subscriptionMap:       newSafeSubscriptionMap(),
		EventSourceController: opts.eventSourceController,
		runtimeDir:            opts.runtimeDir,
		supportDir:            opts.supportDir,
		dockerContainerDir:    opts.dockerContainerDir,
		ociContainerDir:       opts.ociContainerDir,
		cleanupFuncs:          opts.cleanupFuncs,
		cgroupNames:           opts.cgroupNames,
		ringBufferNumPages:    opts.ringBufferNumPages,
	}
	s.monitor.Store((*perf.EventMonitor)(nil))

	return s, nil
}

// Monitor returns a reference to the sensor's EventMonitor instance.
func (s *Sensor) Monitor() *perf.EventMonitor {
	return s.monitor.Load().(*perf.EventMonitor)
}

// Start starts a sensor instance running.
func (s *Sensor) Start() error {
	var buf unix.Utsname
	if err := unix.Uname(&buf); err == nil {
		machine := strings.TrimRight(string(buf.Machine[:]), "\000")
		nodename := strings.TrimRight(string(buf.Nodename[:]), "\000")
		sysname := strings.TrimRight(string(buf.Sysname[:]), "\000")
		release := strings.TrimRight(string(buf.Release[:]), "\000")
		version := strings.TrimRight(string(buf.Version[:]), "\000")
		glog.Infof("%s %s %s %s %s",
			machine, nodename, sysname, release, version)
	}

	// We require that our run dir (usually /var/run/capsule8) exists.
	// Ensure that now before proceeding any further.
	if err := os.MkdirAll(s.runtimeDir, 0700); err != nil {
		glog.Warningf("Couldn't mkdir %s: %v", s.runtimeDir, err)
		return err
	}

	// If there is no mounted tracefs, the Sensor really can't do anything.
	// Try mounting our own private mount of it.
	if !config.Sensor.DontMountTracing && len(s.tracingDir) == 0 {
		// If we couldn't find one, try mounting our own private one
		glog.V(2).Info("Can't find mounted tracefs, mounting one")
		if err := s.mountTraceFS(); err != nil {
			glog.V(1).Info(err)
			return err
		}
	}

	// If there is no mounted cgroupfs for the perf_event cgroup, we can't
	// efficiently separate processes in monitored containers from host
	// processes. We can run without it, but it's better performance when
	// available.
	if !config.Sensor.DontMountPerfEvent && len(s.perfEventDir) == 0 {
		glog.V(2).Info("Can't find mounted perf_event cgroupfs, mounting one")
		if err := s.mountPerfEventCgroupFS(); err != nil {
			glog.V(1).Info(err)
			// This is not a fatal error condition, proceed on
		}
	}

	// Create the sensor-global event monitor. This EventMonitor instance
	// will be used for all perf_event events
	err := s.createEventMonitor()
	if err != nil {
		s.Stop()
		return err
	}

	s.kallsyms, err = s.ProcFS.KernelTextSymbolNames()
	if err != nil {
		glog.Warningf("Could not load kernel symbols: %v", err)
	}

	// Before we create caches and start adding probes/traces, attempt to
	// find some kernel struct/field offsets that could be useful.
	if !s.lookupStructFieldOffsets(os.Args[0]) {
		glog.Infof("Unable to use high speed sensor instrumentation, "+
			"reverting to generic instrumentation. Depending upon "+
			"your workload this could impact cpu load. Please "+
			"contact c8 to check for availability of an updated "+
			"%s/task_struct.json file", s.supportDir)
	}

	s.lookupFunctionSignatures()

	s.ContainerCache = NewContainerCache(s)
	s.ProcessCache = NewProcessInfoCache(s)
	s.ProcessCache.Start()
	s.FileMonitor = NewFileMonitor(s)

	if len(s.dockerContainerDir) > 0 {
		s.dockerMonitor = newDockerMonitor(s, s.dockerContainerDir)
		if s.dockerMonitor != nil {
			s.dockerMonitor.start()
		}
	}
	/* Temporarily disable the OCI monitor until a better means of
	   supporting it is found.
	if len(s.ociContainerDir) > 0 {
		s.ociMonitor = newOciMonitor(s, s.ociContainerDir)
		s.ociMonitor.start()
	}
	*/

	// Make sure that all events registered with the sensor's event monitor
	// are active
	s.Monitor().EnableGroup(0)

	return nil
}

// Stop stops a running sensor instance.
func (s *Sensor) Stop() {
	if monitor := s.Monitor(); monitor != nil {
		glog.V(2).Info("Stopping sensor-global EventMonitor")
		monitor.Close()
		s.monitor.Store((*perf.EventMonitor)(nil))
		glog.V(2).Info("Sensor-global EventMonitor stopped successfully")
	}

	for x := len(s.cleanupFuncs) - 1; x >= 0; x-- {
		s.cleanupFuncs[x]()
	}
}

func (s *Sensor) determineTracingMountInfo(filesystems []string) (string, string, string, error) {
	var haveDebugFS, haveTraceFS bool
	for _, fs := range filesystems {
		switch fs {
		case "debugfs":
			haveDebugFS = true
		case "tracefs":
			haveTraceFS = true
		}
	}

	var (
		err                          error
		fsType, mountDir, tracingDir string
	)
	if haveTraceFS {
		fsType = "tracefs"
		mountDir = filepath.Join(s.runtimeDir, "tracing")
		tracingDir = mountDir
	} else if haveDebugFS {
		fsType = "debugfs"
		mountDir = filepath.Join(s.runtimeDir, "debug")
		tracingDir = filepath.Join(mountDir, "tracing")
	} else {
		err = errors.New("No debugfs or tracefs filesystem support")
	}

	return fsType, mountDir, tracingDir, err
}

func (s *Sensor) mountTraceFS() error {
	filesystems := s.ProcFS.SupportedFilesystems()
	fsType, mountDir, tracingDir, err := s.determineTracingMountInfo(filesystems)
	if err != nil {
		return err
	}
	err = sys.MountTempFS(fsType, mountDir, fsType, 0, "")
	if err == nil {
		s.tracingDir = tracingDir
		s.cleanupFuncs = append(s.cleanupFuncs, func() {
			glog.V(2).Infof("Unmounting temporary %s mount at %s", fsType, mountDir)
			err = sys.UnmountTempFS(mountDir, fsType)
			if err != nil {
				glog.V(2).Infof("Could not unmount %s: %v", mountDir, err)
			}
		})
	}
	return err
}

func (s *Sensor) mountPerfEventCgroupFS() error {
	dir := filepath.Join(s.runtimeDir, "perf_event")
	err := sys.MountTempFS("cgroup", dir, "cgroup", 0, "perf_event")
	if err == nil {
		s.perfEventDir = dir
		s.cleanupFuncs = append(s.cleanupFuncs, func() {
			glog.V(2).Infof("Unmounting temporary cgroup mount at %s", dir)
			err = sys.UnmountTempFS(dir, "cgroup")
			if err != nil {
				glog.V(2).Infof("Could not unmount %s: %v", dir, err)
			}
		})
	}
	return err
}

func (s *Sensor) buildMonitorGroups() ([]string, []int, error) {
	var (
		cgroupList []string
		pidList    []int
		system     bool
	)

	cgroups := make(map[string]bool)
	for _, cgroup := range s.cgroupNames {
		if len(cgroup) == 0 || cgroup == "/" {
			system = true
			continue
		}
		if cgroups[cgroup] {
			continue
		}
		cgroups[cgroup] = true
		cgroupList = append(cgroupList, cgroup)
	}

	// Try a system-wide perf event monitor if requested or as
	// a fallback if no cgroups were requested
	if system || len(s.perfEventDir) == 0 || len(cgroupList) == 0 {
		pidList = append(pidList, -1)
	}

	return cgroupList, pidList, nil
}

func (s *Sensor) createEventMonitor() error {
	eventMonitorOptions := []perf.EventMonitorOption{}
	eventMonitorOptions = append(eventMonitorOptions,
		perf.WithProcFileSystem(s.ProcFS))
	eventMonitorOptions = append(eventMonitorOptions,
		perf.WithEventSourceController(s.EventSourceController))
	eventMonitorOptions = append(eventMonitorOptions,
		perf.WithRingBufferNumPages(s.ringBufferNumPages))

	if len(s.tracingDir) > 0 {
		eventMonitorOptions = append(eventMonitorOptions,
			perf.WithTracingDir(s.tracingDir))
	}

	cgroups, pids, err := s.buildMonitorGroups()
	if err != nil {
		return err
	}

	if len(cgroups) == 0 && len(pids) == 0 {
		glog.Fatal("Can't create event monitor with no cgroups or pids")
	}

	if len(pids) > 0 {
		glog.V(1).Info("Creating new system-wide event monitor")
		eventMonitorOptions = append(eventMonitorOptions,
			perf.WithPids(pids))
	}

	var optionsWithoutCgroups []perf.EventMonitorOption
	copy(optionsWithoutCgroups, eventMonitorOptions)

	if len(cgroups) > 0 && len(s.perfEventDir) > 0 {
		glog.V(1).Infof("Creating new perf event monitor on cgroups %s",
			strings.Join(cgroups, ","))

		eventMonitorOptions = append(eventMonitorOptions,
			perf.WithPerfEventDir(s.perfEventDir),
			perf.WithCgroups(cgroups))
	}

	monitor, err := perf.NewEventMonitor(eventMonitorOptions...)
	if err != nil {
		// If a cgroup-specific event monitor could not be created,
		// fall back to a system-wide event monitor.
		if len(cgroups) > 0 &&
			(len(pids) == 0 || (len(pids) == 1 && pids[0] == -1)) {

			glog.Warningf("Couldn't create perf event monitor on cgroups %s: %s",
				strings.Join(cgroups, ","), err)

			glog.V(1).Info("Creating new system-wide event monitor")
			monitor, err = perf.NewEventMonitor(optionsWithoutCgroups...)
		}
		if err != nil {
			glog.V(1).Infof("Couldn't create event monitor: %s", err)
			return err
		}
	}
	s.monitor.Store(monitor)

	go func() {
		if runErr := monitor.Run(); runErr != nil {
			glog.Fatal(err)
		}
		glog.V(2).Info("EventMonitor.Run() returned; exiting goroutine")
	}()

	return nil
}

// IsKernelSymbolAvailable checks to see if the specified kprobe symbol is
// available for use in the running kernel.
func (s *Sensor) IsKernelSymbolAvailable(symbol string) bool {
	// If the kallsyms mapping is nil, the table could not be
	// loaded for some reason; assume anything is available
	ok := true
	if s.kallsyms != nil {
		_, ok = s.kallsyms[symbol]
	}
	return ok
}

// ActualKernelSymbol returns the actual kernel symbol to use. For some symbols,
// the linker does some rewriting and system calls have different prefixes in
// Linux 4.17+ kernels.
func (s *Sensor) ActualKernelSymbol(symbol string) (string, error) {
	if s.kallsyms != nil {
		if actual, ok := s.kallsyms[symbol]; ok {
			if actual != symbol {
				glog.V(2).Infof("Using %q for kprobe symbol %q",
					actual, symbol)
				return actual, nil
			}
		} else if strings.HasPrefix(symbol, "sys_") {
			// Linux 4.17 changes how syscall handlers are done. It adds a `__x64_`
			// prefix and also changes how arguments are handled in the syscall handler.
			// Automatically try to prepend `__x64_` if we're registering a kprobe
			// on a syscall handler, and if it succeeds, rewrite the kprobe fetch args.
			if actual, ok = s.kallsyms["__x64_"+symbol]; ok {
				glog.V(2).Infof("Using %q for kprobe symbol %q",
					actual, symbol)
				return actual, nil
			}
		} else {
			return "", fmt.Errorf("Kernel symbol not found: %s", symbol)
		}
	}
	return symbol, nil
}

// Map for rewriting kprobe fetch args in kernel 4.17+
// N.B. %di must come first to avoid replacing a %di in an already replaced
// expression.
// N.B. %cx actually needs to be replaced with pt_regs->r10. Since the syscall
// handlers used to have "real" arguments, registers were setup according to the
// x64 _C_ ABI, however now the syscalls only get a pointer to the register state
// at the time the syscall entered, which means the registers are setup in the
// x64 _syscall_ ABI.
var fetchArgsReplacements = [][2]string{
	{"%di", "+0x70(%di)"}, // pt_regs+0x70
	{"%si", "+0x68(%di)"},
	{"%dx", "+0x60(%di)"},
	{"%cx", "+0x38(%di)"}, // This is actually replacing RCX with R10
	{"%r8", "+0x48(%di)"},
	{"%r9", "+0x40(%di)"},
	{"%ax", "+0x50(%di)"},
}

func rewriteSyscallFetchargs(fetchargs string) string {
	// rewrite `output` (the kprobe fetch args) to account for
	// the only argument to the syscall handler being `pt_regs *regs`
	for _, rewritePair := range fetchArgsReplacements {
		srcReg := rewritePair[0]
		dstExpr := rewritePair[1]
		fetchargs = strings.Replace(fetchargs, srcReg, dstExpr, -1)
	}
	glog.V(2).Infof("Rewrote kprobe fetch args to %q", fetchargs)
	return fetchargs
}

// RegisterKprobe registers a kprobe with the sensor's EventMonitor instance,
// but before doing so, ensures that the kernel symbol is available and potentially
// transforms it to account for new kernel changes.
func (s *Sensor) RegisterKprobe(
	address string,
	onReturn bool,
	output string,
	handlerFn perf.TraceEventHandlerFn,
	groupid int32,
	options ...perf.RegisterEventOption,
) (uint64, error) {
	address, err := s.ActualKernelSymbol(address)
	if err != nil {
		return 0, err
	}
	if strings.HasPrefix(address, "__x64_sys_") {
		output = rewriteSyscallFetchargs(output)
	}
	return s.Monitor().RegisterKprobe(address, onReturn, output, handlerFn,
		groupid, options...)
}

// FindSupportFile looks for a supporting file by name and returns the path to
// it. The current working directory is checked first, followed by the path
// from which the calling executable launched, followed by Sensor.supportDir.
func (s *Sensor) FindSupportFile(exeFileName, name string, mode uint32) string {
	var filename string

	if wd, err := os.Getwd(); err == nil {
		filename = filepath.Join(wd, name)
		if err = unix.Access(filename, mode); err == nil {
			return filename
		}
	}

	if executableName, err := exec.LookPath(exeFileName); err == nil {
		filename = filepath.Join(filepath.Dir(executableName), name)
		if err = unix.Access(filename, mode); err == nil {
			return filename
		}
	}

	filename = filepath.Join(s.supportDir, name)
	if err := unix.Access(filename, mode); err == nil {
		return filename
	}

	return ""
}

type taskStructFields struct {
	RealStartTime StructField `json:"real_start_time"`
	PID           StructField `json:"pid"`
	TGID          StructField `json:"tgid"`
}

func (s *Sensor) lookupTaskStructFields(
	dataFilename string,
	release string,
) (fieldData []taskStructFields, ok bool) {
	data, err := ioutil.ReadFile(dataFilename)
	if err != nil {
		glog.V(1).Infof("Could not read %s: %v", dataFilename, err)
		return
	}

	var dataMap map[string][]taskStructFields
	if err = json.Unmarshal(data, &dataMap); err != nil {
		glog.V(1).Infof("Could not unmarshal %s: %v", dataFilename, err)
		return
	}

	fieldData, ok = dataMap[release]
	return
}

func (s *Sensor) lookupStructFieldOffsets(exeFileName string) bool {
	//
	// struct dentry
	//

	if s.IsKernelSymbolAvailable("__d_lookup_rcu") {
		// This is a kernel version >= 2.6.38
		s.dentryStructName = StructField{
			Offset: 40,
			Size:   8,
		}
		s.dentryStructParent = StructField{
			Offset: 24,
			Size:   8,
		}
	} else {
		// This is a kernel version < 2.6.38
		s.dentryStructName = StructField{
			Offset: 56,
			Size:   8,
		}
		s.dentryStructParent = StructField{
			Offset: 40,
			Size:   8,
		}
	}

	//
	// struct inode
	//

	if s.IsKernelSymbolAvailable("timespec64_trunc") {
		// This is a kernel version >= 4.18
		s.inodeSuperBlockOffset = StructField{
			Offset: 64,
			Size:   8,
		}
	} else {
		// This is a kernel version < 4.18
		s.inodeSuperBlockOffset = StructField{
			Offset: 40,
			Size:   8,
		}
	}

	//
	// struct super_block
	//

	// TODO: these static offsets cover kernels in our support matrix,
	// but between those versions fields were added and removed to make the offsets still match
	// add support for these intermediary kernel versions
	s.superblockMountsOffset = StructField{
		Offset: 192,
		Size:   16,
	}

	//
	// struct mount
	//

	s.mountMntInstanceOffset = StructField{
		Offset: 96,
		Size:   16,
	}
	s.mountMntOffset = StructField{
		Offset: 32,
		Size:   24,
	}
	s.mountMntIDOffset = StructField{
		Offset: 252,
		Size:   4,
	}

	//
	// struct task_struct
	//

	dataFilename := s.FindSupportFile(exeFileName, "task_struct.json", unix.R_OK)
	if dataFilename == "" {
		glog.V(1).Infof("Could not locate support file: task_struct.json")
		return false
	}
	glog.V(2).Infof("Found support file: %s", dataFilename)

	var buf unix.Utsname
	if err := unix.Uname(&buf); err != nil {
		glog.V(1).Infof("Uname failure: %v", err)
		return false
	}
	release := strings.TrimRight(string(buf.Release[:]), "\000")
	fieldData, ok := s.lookupTaskStructFields(dataFilename, release)
	if !ok {
		return false
	}
	for _, fields := range fieldData {
		if fields.PID.Size != 4 || fields.TGID.Size != 4 ||
			(fields.RealStartTime.Size != 16 &&
				fields.RealStartTime.Size != 8) {
			glog.V(1).Infof("Unexpected task_struct field offset sizes (%#v)", fields)
			continue
		}
		if s.testTaskStructOffsets(fields) {
			return true
		}
	}

	glog.V(1).Infof("Could not validate task struct offsets for release %q", release)
	return false
}

func (s *Sensor) lookupFunctionSignatures() {
	// fsnotify_mask was removed in v2.6.36-rc1, when the file argument was added to __fsnotify_parent
	// Complete history of __fsnotify_parent: https://gist.github.com/rpetrich/009d8969e77721d921ea3241b5321bc2
	s.fsnotifyParentHasPath = !s.IsKernelSymbolAvailable("fsnotify_mask")
}

func (s *Sensor) compareTaskStructSample(
	sample expression.FieldValueMap,
	reportedPID, reportedTGID int32,
	reportedStartTime int64,
	fields taskStructFields,
) bool {
	sampledPID := sample["pid"].(int32)
	sampledTGID := sample["tgid"].(int32)

	var sampledStartTime int64
	timeSecs := sample["time_secs"].(int64)
	if timeNsecs, ok := sample["time_nsecs"].(int64); ok {
		sampledStartTime = ((timeSecs * 1e9) + timeNsecs) / 1e7
	} else {
		// timeSecs is actually coming from the kernel as nsecs
		sampledStartTime = timeSecs / 1e7
	}
	if reportedPID != sampledPID {
		glog.V(2).Infof("Reported PID (%d) did not match sampled PID (%d) at offset %d",
			reportedPID, sampledPID, fields.PID.Offset)
		return false
	}
	if reportedTGID != sampledTGID {
		glog.V(2).Infof("Reported TGID (%d) did not match sampled TGID (%d) at offset %d",
			reportedTGID, sampledTGID, fields.TGID.Offset)
		return false
	}
	if reportedStartTime != sampledStartTime {
		glog.V(2).Infof("Reported start time (%d) did not match sampled start time (%d) at offset %d",
			reportedStartTime, sampledStartTime, fields.RealStartTime.Offset)
		return false
	}
	glog.V(1).Infof("Found usable struct task_struct offsets for pid, tgid, and real_start_time")
	s.taskStructPID = fields.PID
	s.taskStructTGID = fields.TGID
	s.taskStructRealStartTime = fields.RealStartTime
	return true
}

func (s *Sensor) testTaskStructOffsets(fields taskStructFields) bool {
	// Create an event monitor group with a single kprobe in it. This kprobe
	// will collect data from the field offsets
	monitor := s.Monitor()
	groupid, err := monitor.RegisterEventGroup("task_struct", nil)
	if err != nil {
		glog.V(1).Infof("Could not create event group for task_struct verification: %v", err)
		return false
	}
	defer monitor.UnregisterEventGroup(groupid)

	var (
		fetchargs string
		samples   []expression.FieldValueMap
		lock      sync.Mutex
	)
	switch fields.RealStartTime.Size {
	case 8:
		fetchargs = fmt.Sprintf("pid=+%d(%%cx):s32 tgid=+%d(%%cx):s32 time_secs=+%d(%%cx):s64",
			fields.PID.Offset, fields.TGID.Offset,
			fields.RealStartTime.Offset)
	case 16:
		fetchargs = fmt.Sprintf("pid=+%d(%%cx):s32 tgid=+%d(%%cx):s32 time_secs=+%d(%%cx):s64 time_nsecs=+%d(%%cx):s64",
			fields.PID.Offset, fields.TGID.Offset,
			fields.RealStartTime.Offset, fields.RealStartTime.Offset+8)
	}
	_, err = s.RegisterKprobe("do_task_stat", false, fetchargs,
		func(_ uint64, sample *perf.Sample) {
			lock.Lock()
			data, _ := sample.DecodeRawData()
			samples = append(samples, data)
			lock.Unlock()
		},
		groupid,
		perf.WithEventEnabled())

	reportedPID, reportedTGID, reportedStartTime :=
		reportTaskStructFields(s.ProcFS)
	if reportedPID == 0 || reportedTGID == 0 || reportedStartTime == 0 {
		return false
	}

	// Loop here. We probably already have the sample we're looking for,
	// but there are timing issues involved so maybe not. Scan what we've
	// got and if there's nothing, wait a second and scan again.
	for i := 0; i < 2; i++ {
		lock.Lock()
		for _, sample := range samples {
			if s.compareTaskStructSample(sample,
				reportedPID, reportedTGID, reportedStartTime,
				fields) {
				lock.Unlock()
				return true
			}
		}
		samples = samples[:0]
		lock.Unlock()
		time.Sleep(1 * time.Second)
	}
	return false
}

// NewSubscription creates a new telemetry subscription
func (s *Sensor) NewSubscription() *Subscription {
	subscriptionID := atomic.AddUint64(&s.Metrics.Subscriptions, 1)

	// Use an empty dispatch function until Subscription.Run is called with
	// the real dispatch function to use. This is to avoid an extra branch
	// during dispatch to check for a nil dispatchFn. Since under normal
	// operation this case is impossible, it's a waste to add the check
	// when it's so easy to handle otherwise during the subscription
	// window.
	return &Subscription{
		sensor:         s,
		subscriptionID: subscriptionID,
		dispatchFn:     func(e TelemetryEvent) {},
	}
}

// DispatchEvent dispatches a telemetry event to all subscribers that are
// listening for it.
func (s *Sensor) DispatchEvent(
	eventid uint64,
	event TelemetryEvent,
	valueGetter expression.FieldValueGetter,
) {
	eventMap := s.eventMap.getMap()
	eventSinks := eventMap[eventid]
	for _, es := range eventSinks {
		if es.filter != nil {
			v, err := es.filter.Evaluate(valueGetter)
			if err != nil {
				glog.V(1).Infof("Expression evaluation error: %s", err)
				continue
			}
			if !expression.IsValueTrue(v) {
				continue
			}
		}
		subscr := es.subscription
		containerInfo := event.CommonTelemetryEventData().Container
		if !subscr.containerFilter.Match(containerInfo) {
			continue
		}
		subscr.dispatchFn(event)
	}
}

// DispatchEventToAllSubscriptions dispatches a telemetry event to all
// subscriptions regardless of whether they are listening for the event or not.
// It is effectively a broadcast that cannot be ignored.
func (s *Sensor) DispatchEventToAllSubscriptions(event TelemetryEvent) {
	subscriptions := s.subscriptionMap.getMap()
	for _, s := range subscriptions {
		s.dispatchFn(event)
	}
}
