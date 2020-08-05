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

// This is the implementation of the EventMonitor. It is meant to retrieve
// samples from the Linux kernel's perf interface, which is accessed via
// perf_event_open(2) and ringbuffers shared with the kernel via mmap'd memory.
//
// The API provided to consumers is simple. The consumer may register
// tracepoints, kprobes, uprobes, or hardware/software counters. For each
// registration, any data obtained from the kernel is passed back via a func
// that's also registered. Each of these registrations is assigned an event ID
// that is used internally, but mostly later to allow the consumer to also
// unregister previous registrations.
//
// Registrations may be made into groups. If no group is specified, the default
// group (0) is used. Any number of groups may be registered. Events in the
// same group have their sample data placed into the same ring buffer by the
// kernel. It's also easy to enable, disable, or unregister events in batch by
// enabling, disabling, or unregistering a group. Grouping is also important on
// CentOS 6 platforms, because unregistering a single event from a group may
// cause the kernel to panic. Unregistering a whole group at a time works
// around this problem.
//
// The EventMonitor may be in two states: running or stopped. If it is stopped,
// which is the default state, no processing of sample data from the kernel is
// done. This is not a particularly useful state; however, event and group
// registrations can be done while in the stopped state. While the EventMonitor
// is in the running state, two goroutines are used to work cooperatively to
// obtain and handle the sample data from the kernel.
//
// The event source goroutine is started when EventMonitor.Run is called. This
// goroutine is responsible for retrieving sample data from the kernel's ring
// buffers. Since the kernel will drop events when the ring buffers are full,
// this goroutine does nothing but copy the data out of the ring buffers so
// that we drop as few samples from the kernel as possible. This goroutine
// simply places the data into buffers that it passes off to the handler
// goroutine.
//
// The handler goroutine is the goroutine on which EventMonitor.Run is called.
// This gives the consumer the option of allowing the EventMonitor to run
// either synchronously or asynchronously. The handler goroutine pulls sample
// data from the event source goroutine, decodes it, and passes each sample to
// the consumer by calling the func that was registered for the event.

package perf

import (
	"bufio"
	"bytes"
	"debug/elf"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
	"unsafe"

	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys/proc"
	"github.com/Happyholic1203/capsule8/pkg/sys/proc/procfs"
	"github.com/golang/glog"

	"golang.org/x/sys/unix"
)

var (
	// HaveClockID is true if the system supports the ClockID attribute.
	HaveClockID bool

	// TimeBase is the time at which TimeOffsets were determined.
	TimeBase int64

	// TimeOffsets are the time offsets to use for each CPU.
	TimeOffsets []int64
)

type eventMonitorOptions struct {
	eventSourceController EventSourceController
	procfs                proc.FileSystem
	flags                 uintptr
	defaultEventAttr      *EventAttr
	perfEventDir          string
	tracingDir            string
	ringBufferNumPages    int
	cgroups               []string
	pids                  []int
	defaultCacheSize      int
}

// EventMonitorOption is used to implement optional arguments for
// NewEventMonitor. It must be exported, but it is not typically
// used directly.
type EventMonitorOption func(*eventMonitorOptions)

func newEventMonitorOptions() eventMonitorOptions {
	return eventMonitorOptions{}
}

func (opts *eventMonitorOptions) processOptions(
	options ...EventMonitorOption,
) {
	for _, option := range options {
		option(opts)
	}
}

// WithFlags is used to set optional flags when creating a new EventMonitor.
// The flags are passed to the low-level perf_event_open() system call.
func WithFlags(flags uintptr) EventMonitorOption {
	return func(o *eventMonitorOptions) {
		o.flags = flags
	}
}

// WithDefaultEventAttr is used to set an optional EventAttr struct to be used
// by default when registering events and no EventAttr is specified as part of
// the registration.
func WithDefaultEventAttr(defaultEventAttr *EventAttr) EventMonitorOption {
	return func(o *eventMonitorOptions) {
		o.defaultEventAttr = defaultEventAttr
	}
}

// WithEventSourceController is used to set the event source controller to be
// used. If left unspecified, the default system event source controller will
// be used. Any controller specified here will be owned immediately by
// NewEventMonitor, which primarily means that its Close method will be called
// if any error occurs while creating the new EventMonitor. If an EventMonitor
// is created successfully, the event source controller's Close method will not
// be called until the monitor's Close method is called.
func WithEventSourceController(controller EventSourceController) EventMonitorOption {
	return func(o *eventMonitorOptions) {
		o.eventSourceController = controller
	}
}

// WithProcFileSystem is used to set the proc.FileSystem to use. The system
// default will be used if one is not specified.
func WithProcFileSystem(procfs proc.FileSystem) EventMonitorOption {
	return func(o *eventMonitorOptions) {
		o.procfs = procfs
	}
}

// WithPerfEventDir is used to set an optional directory to use for monitoring
// cgroups. This should only be necessary if the perf_event cgroup fs is not
// mounted in the usual location.
func WithPerfEventDir(dir string) EventMonitorOption {
	return func(o *eventMonitorOptions) {
		o.perfEventDir = dir
	}
}

// WithTracingDir is used to set an alternate mountpoint to use for managing
// tracepoints, kprobes, and uprobes.
func WithTracingDir(dir string) EventMonitorOption {
	return func(o *eventMonitorOptions) {
		o.tracingDir = dir
	}
}

// WithRingBufferNumPages is used to set the size of the ringbuffers used to
// retrieve samples from the kernel.
func WithRingBufferNumPages(numPages int) EventMonitorOption {
	return func(o *eventMonitorOptions) {
		o.ringBufferNumPages = numPages
	}
}

// WithCgroup is used to add a cgroup to the set of sources to monitor.
func WithCgroup(cgroup string) EventMonitorOption {
	return func(o *eventMonitorOptions) {
		o.cgroups = append(o.cgroups, cgroup)
	}
}

// WithCgroups is used to add a list of cgroups to the set of sources to
// monitor.
func WithCgroups(cgroups []string) EventMonitorOption {
	return func(o *eventMonitorOptions) {
		o.cgroups = append(o.cgroups, cgroups...)
	}
}

// WithPid is used to add a pid to the set of sources to monitor.
func WithPid(pid int) EventMonitorOption {
	return func(o *eventMonitorOptions) {
		o.pids = append(o.pids, pid)
	}
}

// WithPids is used to add a list of pids to the set of sources to monitor.
func WithPids(pids []int) EventMonitorOption {
	return func(o *eventMonitorOptions) {
		o.pids = append(o.pids, pids...)
	}
}

type registerEventOptions struct {
	disabled  bool
	eventAttr *EventAttr
	filter    string
	handlerFn TraceEventHandlerFn
	name      string
}

// RegisterEventOption is used to implement optional arguments for event
// registration methods. It must be exported, but it is not typically used
// directly.
type RegisterEventOption func(*registerEventOptions)

func newRegisterEventOptions() registerEventOptions {
	return registerEventOptions{
		disabled: true,
	}
}

func (opts *registerEventOptions) processOptions(
	options ...RegisterEventOption,
) {
	for _, option := range options {
		option(opts)
	}
}

// WithEventDisabled is used to register the event in a disabled state.
func WithEventDisabled() RegisterEventOption {
	return func(o *registerEventOptions) {
		o.disabled = true
	}
}

// WithEventEnabled is used to register the event in an enabled state.
func WithEventEnabled() RegisterEventOption {
	return func(o *registerEventOptions) {
		o.disabled = false
	}
}

// WithEventAttr is used to register the event with an EventAttr struct
// instead of using the EventMonitor's default.
func WithEventAttr(eventAttr *EventAttr) RegisterEventOption {
	return func(o *registerEventOptions) {
		o.eventAttr = eventAttr
	}
}

// WithFilter is used to set a filter for the event.
func WithFilter(filter string) RegisterEventOption {
	return func(o *registerEventOptions) {
		o.filter = filter
	}
}

// WithTracingEventName is used to specify the name of a kprobe or uprobe to
// use for registration instead of an automatically generated one.
func WithTracingEventName(name string) RegisterEventOption {
	return func(o *registerEventOptions) {
		o.name = name
	}
}

// EventType represents the type of an event (tracepoint, hardware, etc.)
type EventType int

const (
	// EventTypeInvalid is not a valid event type
	EventTypeInvalid EventType = iota

	// EventTypeTracepoint is a trace event (PERF_TYPE_TRACEPOINT)
	EventTypeTracepoint

	// EventTypeKprobe is a kernel probe
	EventTypeKprobe
	// EventTypeUprobe is a user probe
	EventTypeUprobe

	// EventTypeHardware is a hardware event (PERF_TYPE_HARDWARE)
	EventTypeHardware

	// EventTypeSoftware is a software event (PERF_TYPE_SOFTWARE)
	EventTypeSoftware

	// EventTypeHardwareCache is a hardware cache event (PERF_TYPE_HW_CACHE)
	EventTypeHardwareCache

	// EventTypeRaw is a raw event (PERF_TYPE_RAW)
	EventTypeRaw

	// EventTypeBreakpoint is a breakpoint event (PERF_TYPE_BREAKPOINT)
	EventTypeBreakpoint

	// EventTypeDynamicPMU is a dynamic PMU event
	EventTypeDynamicPMU
)

// EventTypeNames is a mapping of EventType to a human-readable string that is
// the name of the symbolic constant.
var EventTypeNames = map[EventType]string{
	EventTypeTracepoint:    "EventTypeTracepoint",
	EventTypeKprobe:        "EventTypeKprobe",
	EventTypeUprobe:        "EventTypeUprobe",
	EventTypeHardware:      "EventTypeHardware",
	EventTypeSoftware:      "EventTypeSoftware",
	EventTypeHardwareCache: "EventTypeHardwareCache",
	EventTypeRaw:           "EventTypeRaw",
	EventTypeBreakpoint:    "EventTypeBreakpoint",
	EventTypeDynamicPMU:    "EventTypeDynamicPMU",
}

func eventTypeFromPerfType(t uint32) EventType {
	switch t {
	case PERF_TYPE_HARDWARE:
		return EventTypeHardware
	case PERF_TYPE_HW_CACHE:
		return EventTypeHardwareCache
	case PERF_TYPE_SOFTWARE:
		return EventTypeSoftware
	case PERF_TYPE_BREAKPOINT:
		return EventTypeBreakpoint
	case PERF_TYPE_RAW:
		return EventTypeRaw
	case PERF_TYPE_TRACEPOINT:
		// Could be kprobe or uprobe, but not enough information to
		// make a determination
		return EventTypeTracepoint
	}
	glog.Fatalf("Unrecognized event type %d", t)
	return EventTypeInvalid
}

func perfTypeFromEventType(t EventType) uint32 {
	switch t {
	case EventTypeHardware:
		return PERF_TYPE_HARDWARE
	case EventTypeSoftware:
		return PERF_TYPE_SOFTWARE
	case EventTypeTracepoint, EventTypeKprobe, EventTypeUprobe:
		return PERF_TYPE_TRACEPOINT
	case EventTypeHardwareCache:
		return PERF_TYPE_HW_CACHE
	case EventTypeRaw:
		return PERF_TYPE_RAW
	case EventTypeBreakpoint:
		return PERF_TYPE_BREAKPOINT
	}
	glog.Fatalf("Unrecognized event type %d", t)
	return 0
}

// EventMonitorLostRecordFn is the signature of a function that is called when
// lost records are reported by the kernel. The first argument is the event ID
// of the first valid record recorded after a loss of records, the second
// argument is the SampleID information for the LostRecord, and the third
// argument is the number of records that were lost.
type EventMonitorLostRecordFn func(uint64, int32, SampleID, uint64)

func roundPow2(limit int) int {
	size := 2
	for size < limit {
		size <<= 1
	}
	return size
}

func roundPow2Exp(n int) int {
	e := uint(1)
	for 1<<e < n {
		e++
	}
	return int(e)
}

type eventSampleHandler interface {
	handleSample(*registeredEvent, *Sample, *EventMonitor)
}

// CounterEventValue is a counter value returned from the kernel. The EventType
// and Config values are what were used to register the counter group member,
// and Value is the value returned with the sample.
type CounterEventValue struct {
	EventType EventType
	Config    uint64
	Value     uint64
}

// CounterEventHandlerFn is the signature of a function to call to handle a
// counter event sample. The first argument is the event ID of the sample to be
// handled, the second is the sample to be handled, the third is a map of event
// counter IDs to values, the fourth is the total time the event has been
// enabled, and the fifth is the total time the event has been running.
type CounterEventHandlerFn func(uint64, *Sample, []CounterEventValue, uint64, uint64)

type counterEventSampleHandler struct {
	handlerFn CounterEventHandlerFn
}

func (h counterEventSampleHandler) handleSample(
	event *registeredEvent,
	sample *Sample,
	monitor *EventMonitor,
) {
	switch sample.Type {
	case PERF_RECORD_LOST:
		group := event.group
		if sample.Lost > 0 && group.lostRecordFn != nil {
			group.lostRecordFn(event.id, group.groupID,
				sample.SampleID, sample.Lost)
		}
	case PERF_RECORD_SAMPLE:
		if h.handlerFn != nil {
			attrMap := monitor.eventAttrMap.getMap()
			counters := make([]CounterEventValue, 0, len(sample.V.Values))
			for _, v := range sample.V.Values {
				if attr, ok := attrMap[v.ID]; ok {
					counters = append(counters, CounterEventValue{
						EventType: eventTypeFromPerfType(attr.Type),
						Config:    attr.Config,
						Value:     v.Value,
					})
				}
			}

			h.handlerFn(event.id, sample, counters,
				sample.V.TimeEnabled, sample.V.TimeRunning)
		}
	}
}

// TraceEventHandlerFn is the signature of a function to call to handle a
// sample. The first argument is the event ID of the event source that produced
// the sample. The second argument is the sample, and the final argument is the
// parsed sample data.
type TraceEventHandlerFn func(uint64, *Sample)

type traceEventSampleHandler struct {
	handlerFn TraceEventHandlerFn
}

func (h traceEventSampleHandler) handleSample(
	event *registeredEvent,
	sample *Sample,
	monitor *EventMonitor,
) {
	switch sample.Type {
	case PERF_RECORD_LOST:
		group := event.group
		if sample.Lost > 0 && group.lostRecordFn != nil {
			group.lostRecordFn(event.id, group.groupID,
				sample.SampleID, sample.Lost)
		}
	case PERF_RECORD_SAMPLE:
		if h.handlerFn != nil {
			eventType := *(*uint16)(unsafe.Pointer(&sample.RawData[0]))
			sample.TraceFormat, _ = monitor.traceFormats.lookup(eventType)
			if sample.TraceFormat != nil && !event.filterSample(sample) {
				// When both the sensor and the process generating the sample
				// are in containers, the sample.PID and sample.TID fields will
				// be zero. Use "common_pid" from the trace event data instead.
				if sample.TID == 0 {
					pid, _ := sample.GetSignedInt32("common_pid")
					sample.TID = uint32(pid)
				}
				h.handlerFn(event.id, sample)
			}
		}
	}
}

type registeredEvent struct {
	id           uint64
	name         string
	sources      []EventSource // one source per cpu
	fields       expression.FieldTypeMap
	filter       *expression.Expression
	handler      eventSampleHandler
	eventType    EventType
	group        *eventMonitorGroup
	leader       bool
	kernelFilter bool
	formatID     uint16
}

func (event *registeredEvent) filterSample(valueGetter expression.FieldValueGetter) bool {
	if event.filter == nil {
		return false
	}
	result, err := event.filter.Evaluate(valueGetter)
	return err != nil || !expression.IsValueTrue(result)
}

func (event *registeredEvent) setFilter(filter *expression.Expression) error {
	switch event.eventType {
	case EventTypeKprobe, EventTypeTracepoint, EventTypeUprobe:
		// ok
	default:
		return fmt.Errorf("Event %d does not support filtering",
			event.id)
	}

	if filter == nil {
		if event.kernelFilter {
			for _, source := range event.sources {
				source.SetFilter("")
			}
		}
		event.filter = nil
		event.kernelFilter = false
		return nil
	}

	// First try to set the filter in the kernel. If that does not
	// work, fall back to filtering in user space.
	if err := filter.ValidateKernelFilter(); err == nil {
		filterString := filter.KernelFilterString()
		for _, source := range event.sources {
			if err = source.SetFilter(filterString); err != nil {
				break
			}
		}
		if err == nil {
			event.filter = nil
			event.kernelFilter = true
			return nil
		}
	}
	if event.kernelFilter {
		for _, source := range event.sources {
			source.SetFilter("")
		}
	}
	event.filter = filter
	event.kernelFilter = false
	return nil
}

const (
	perfGroupLeaderStateActive int32 = iota
	perfGroupLeaderStateClosing
	perfGroupLeaderStateClosed
)

type perfGroupLeader struct {
	source EventSourceLeader

	// This is the event's state. Normally it will be active. When a group
	// is being removed, it will transition to closing, which means that
	// the ringbuffer servicing goroutine should ignore it. That goroutine
	// will call .cleanup() for the event and transition it to the closed
	// state, which means that it can be safely removed by any goroutine at
	// any point in the future.
	state int32
}

func (pgl *perfGroupLeader) cleanup() {
	pgl.source.Close()
	atomic.StoreInt32(&pgl.state, perfGroupLeaderStateClosed)
}

func (pgl *perfGroupLeader) active() bool {
	return pgl != nil &&
		atomic.LoadInt32(&pgl.state) == perfGroupLeaderStateActive
}

type eventMonitorGroup struct {
	name         string
	leaders      []*perfGroupLeader
	events       map[uint64]*registeredEvent
	monitor      *EventMonitor
	groupID      int32
	lostRecordFn EventMonitorLostRecordFn
}

func (group *eventMonitorGroup) cleanup() {
	// First we disable all group leaders. This is necessary because of
	// CentOS 6 kernel bugs that could cause a kernel panic if we don't do
	// this.
	for _, pgl := range group.leaders {
		pgl.source.Disable()
	}

	// Now we can unregister all of the events
	monitor := group.monitor
	for _, event := range group.events {
		monitor.removeRegisteredEvent(event)
	}
}

func (group *eventMonitorGroup) disable() {
	for _, event := range group.events {
		for _, source := range event.sources {
			source.Disable()
		}
	}
}

func (group *eventMonitorGroup) enable() {
	for _, event := range group.events {
		for _, source := range event.sources {
			source.Enable()
		}
	}
}

func (group *eventMonitorGroup) perfEventOpen(
	name string,
	eventAttr EventAttr,
	flags uintptr,
) (sources []EventSource, err error) {
	glog.V(2).Infof("Opening perf event: %s (%d) in group %d %q",
		name, eventAttr.Config, group.groupID, group.name)

	newsources := make([]EventSource, 0, len(group.leaders))
	defer func() {
		if err != nil {
			for j := len(newsources) - 1; j >= 0; j-- {
				newsources[j].Close()
			}
		}
	}()

	for _, pgl := range group.leaders {
		var source EventSource
		if source, err = pgl.source.NewEventSource(eventAttr, flags); err != nil {
			return
		}
		newsources = append(newsources, source)
	}

	sources = newsources
	return
}

// EventMonitor is a high-level interface to the Linux kernel's perf_event
// infrastructure.
type EventMonitor struct {
	// Ordering of fields is intentional to keep the most frequently used
	// fields together at the head of the struct in an effort to increase
	// cache locality

	// Mutable by the thread on which Stop is called. Checked constantly
	// by the event source and handler goroutines.
	stopRequested atomic.Value // bool

	// Mutable only by the handler goroutine, but checked constantly by the
	// event source goroutine.
	handlingSamples atomic.Value // bool

	// Mutable by various goroutines and also needed by the event source
	// goroutine.
	groupLeaders safePerfGroupLeaderMap // source id : group leader data

	// Mutable by various goroutines and also needed by the handler
	// goroutine.
	eventAttrMap safeEventAttrMap        // stream id : event attr
	eventIDMap   safeUInt64Map           // stream id : event id
	traceFormats safeTraceEventFormatMap // trace id : trace format
	events       safeRegisteredEventMap  // event id : event

	// This TimedEvent is used by the event source goroutine to wake up the
	// handler goroutine when new data has arrived. The handler must also
	// wait with a timeout, which Go does not support, so a "home grown"
	// solution is used instead.
	handlerEvent TimedEvent

	// --- end of first cache line

	// Mutable only by the event source goroutine. No protection is needed.
	currentBuffer []byte
	bufferList    []eventSourceBuffer

	// Mutable only by the handler goroutine. No protection required.
	pendingSamples           [][]Sample
	pendingSamplesBufferList []eventSourceBuffer
	sampleBatchFreeList      [][][]Sample
	sampleFreeList           [31]*sampleList
	sampleListFreeList       *sampleList
	indicesFreeList          [][]int

	// --- end of second cache line

	// This lock protects everything mutable below this point.
	lock sync.Mutex

	// Mutable by the event source and the handler goroutines. Mutex
	// protection is needed.
	bufferFreeList          [][]byte
	bufferListFreeList      [][]eventSourceBuffer
	bufferListQueueHead     *queuedBufferList
	bufferListQueueTail     *queuedBufferList
	bufferListQueueFreeList *queuedBufferList

	// Mutable by various goroutines, and also needed by the event source
	// goroutine. Protected by the EventMonitor lock.
	dyingGroups []*eventMonitorGroup

	// --- end of third cache line
	// --- ordering below this point doesn't matter

	// Mutable by various goroutines, but infrequently. Primarily when
	// registering new groups, tracepoints, probes, etc.
	nextEventID uint64
	nextProbeID uint64
	nextGroupID int32
	groups      map[int32]*eventMonitorGroup

	// Mutable only by the monitor goroutine, but readable by others
	isRunning atomic.Value // bool

	// Immutable. Used infrequently.
	defaultAttr        EventAttr
	tracingDir         string
	procFS             proc.FileSystem
	perfEventOpenFlags uintptr
	cgroups            []int
	pids               []int
	ringBufferNumPages int
	defaultCacheSize   int

	// This is immutable. It's used only by the event source goroutine, but
	// it keeps a local reference to it. It doesn't need to be anywhere
	// special in this struct.
	eventSourceController EventSourceController

	// Used only once during shutdown
	cond sync.Cond
	wg   sync.WaitGroup
}

type sampleList struct {
	next    *sampleList
	samples []Sample
}

type queuedBufferList struct {
	next *queuedBufferList
	list []eventSourceBuffer
}

func fixupEventAttr(eventAttr *EventAttr) {
	// Adjust certain fields in eventAttr that must be set a certain way
	// Super important! In order for the sample parsing code to function
	// (types.go), PERF_SAMPLE_IDENTIFIER _must_ be set in SampleType, and
	// SampleIDAll _must_ be true.
	eventAttr.SampleType |= PERF_SAMPLE_STREAM_ID | PERF_SAMPLE_IDENTIFIER | PERF_SAMPLE_TIME
	eventAttr.Disabled = true
	eventAttr.Pinned = false
	eventAttr.SampleIDAll = true

	if eventAttr.Freq && eventAttr.SampleFreq == 0 {
		eventAttr.SampleFreq = 1
	} else if !eventAttr.Freq && eventAttr.SamplePeriod == 0 {
		eventAttr.SamplePeriod = 1
	}

	// Either WakeupWatermark or WakeupEvents may be used, but at least
	// one must be non-zero, because EventMonitor does not poll.
	if eventAttr.Watermark && eventAttr.WakeupWatermark == 0 {
		eventAttr.WakeupWatermark = 1
	} else if !eventAttr.Watermark && eventAttr.WakeupEvents == 0 {
		eventAttr.WakeupEvents = 1
	}
}

// DoesTracepointExist returns true if the named tracepoint exists on the
// system; otherwise, it returns false.
func (monitor *EventMonitor) DoesTracepointExist(name string) bool {
	dirname := filepath.Join(monitor.tracingDir, "events", name)
	if i, err := os.Stat(dirname); err == nil {
		return i.IsDir()
	}
	return false
}

func (monitor *EventMonitor) writeTraceCommand(name string, cmd string) error {
	filename := filepath.Join(monitor.tracingDir, name)
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		glog.Fatalf("Couldn't open %s WO+A: %s", filename, err)
	}
	defer file.Close()

	_, err = file.Write([]byte(cmd))
	return err
}

func (monitor *EventMonitor) addKprobe(
	name string,
	address string,
	onReturn bool,
	output string,
) error {
	output = strings.Join(strings.Fields(output), " ")

	var definition string
	if onReturn {
		definition = fmt.Sprintf("r:%s %s %s", name, address, output)
	} else {
		definition = fmt.Sprintf("p:%s %s %s", name, address, output)
	}

	glog.V(1).Infof("Adding kprobe: '%s'", definition)
	return monitor.writeTraceCommand("kprobe_events", definition)
}

func (monitor *EventMonitor) removeKprobe(name string) error {
	return monitor.writeTraceCommand("kprobe_events",
		fmt.Sprintf("-:%s", name))
}

func (monitor *EventMonitor) addUprobe(
	name string,
	bin string,
	address string,
	onReturn bool,
	output string,
) error {
	output = strings.Join(strings.Fields(output), " ")

	var definition string
	if onReturn {
		definition = fmt.Sprintf("r:%s %s:%s %s", name, bin, address, output)
	} else {
		definition = fmt.Sprintf("p:%s %s:%s %s", name, bin, address, output)
	}

	glog.V(1).Infof("Adding uprobe: '%s'", definition)
	return monitor.writeTraceCommand("uprobe_events", definition)
}

func (monitor *EventMonitor) removeUprobe(name string) error {
	return monitor.writeTraceCommand("uprobe_events",
		fmt.Sprintf("-:%s", name))
}

func (monitor *EventMonitor) newProbeName() string {
	probeName := monitor.NextProbeName(0)
	monitor.nextProbeID++
	return probeName
}

// NextProbeName is used primarily for unit testing. It returns the next probe
// name that will be used by either RegisterKprobe or RegisterUprobe. Any delta
// specified is added to the counter (intended for use by unit testing)
func (monitor *EventMonitor) NextProbeName(delta uint64) string {
	return fmt.Sprintf("capsule8/sensor_%d_%d", unix.Getpid(),
		monitor.nextProbeID+1+delta)
}

func (monitor *EventMonitor) newRegisteredEvent(
	name string,
	newsources []EventSource,
	fields expression.FieldTypeMap,
	filter *expression.Expression,
	eventType EventType,
	handler eventSampleHandler,
	attr *EventAttr,
	group *eventMonitorGroup,
	leader bool,
	formatID uint16,
) uint64 {
	eventid := monitor.nextEventID
	monitor.nextEventID++

	if len(newsources) > 0 {
		attrMap := newEventAttrMap()
		idMap := newUInt64Map()
		for _, source := range newsources {
			id := source.SourceID()
			attr.computeSizes()
			attrMap[id] = attr
			idMap[id] = eventid
		}

		if monitor.isRunning.Load().(bool) {
			monitor.eventAttrMap.update(attrMap)
			monitor.eventIDMap.update(idMap)
		} else {
			monitor.eventAttrMap.updateInPlace(attrMap)
			monitor.eventIDMap.updateInPlace(idMap)
		}
	}

	event := &registeredEvent{
		id:        eventid,
		name:      name,
		sources:   newsources,
		fields:    fields,
		handler:   handler,
		eventType: eventType,
		group:     group,
		leader:    leader,
		formatID:  formatID,
	}
	group.events[eventid] = event

	monitor.events.insert(eventid, event)
	event.setFilter(filter)
	return eventid
}

func (monitor *EventMonitor) newRegisteredPerfEvent(
	name string,
	config uint64,
	fields expression.FieldTypeMap,
	opts registerEventOptions,
	eventType EventType,
	handler eventSampleHandler,
	formatID uint16,
	groupID int32,
) (uint64, error) {
	// This should be called with monitor.lock held.

	var (
		attr   EventAttr
		err    error
		filter *expression.Expression
		flags  uintptr
	)

	if opts.eventAttr == nil {
		attr = monitor.defaultAttr
	} else {
		attr = *opts.eventAttr
		fixupEventAttr(&attr)
	}
	attr.Type = perfTypeFromEventType(eventType)
	attr.Config = config
	attr.Disabled = opts.disabled

	switch eventType {
	case EventTypeTracepoint, EventTypeKprobe, EventTypeUprobe:
		flags = PERF_FLAG_FD_OUTPUT | PERF_FLAG_FD_NO_GROUP
		if opts.filter != "" {
			filter, err = expression.ParseString(opts.filter,
				expression.ParseModeKernelFilter, fields)
			if err != nil {
				return 0, fmt.Errorf("Error in filter: %v", err)
			}
		}
	default:
		if opts.filter != "" {
			return 0, fmt.Errorf("Event type does not support filtering")
		}
	}

	group, ok := monitor.groups[groupID]
	if !ok {
		return 0, fmt.Errorf("Group ID %d does not exist", groupID)
	}

	newsources, err := group.perfEventOpen(name, attr, flags)
	if err != nil {
		return 0, err
	}

	eventid := monitor.newRegisteredEvent(name, newsources, fields, filter,
		eventType, handler, &attr, group, false, formatID)
	return eventid, nil
}

func (monitor *EventMonitor) newRegisteredTraceEvent(
	name string,
	handlerFn TraceEventHandlerFn,
	opts registerEventOptions,
	eventType EventType,
	groupID int32,
) (uint64, error) {
	// This should be called with monitor.lock held.
	id, format, err := getTraceEventFormat(monitor.tracingDir, name)
	if err != nil {
		return 0, err
	}
	monitor.traceFormats.insert(id, format)

	fields := make(expression.FieldTypeMap, len(format))
	for _, v := range format {
		fields[v.FieldName] = v.DataType
	}

	eventid, err := monitor.newRegisteredPerfEvent(name, uint64(id),
		fields, opts, eventType, traceEventSampleHandler{handlerFn: handlerFn},
		id, groupID)
	if err != nil {
		if eventType != EventTypeTracepoint {
			monitor.traceFormats.remove(id)
		}
		return 0, err
	}

	return eventid, nil
}

// ReserveEventID is used to reserve an event ID.
func (monitor *EventMonitor) ReserveEventID() uint64 {
	monitor.lock.Lock()
	eventid := monitor.nextEventID
	monitor.nextEventID++
	monitor.lock.Unlock()

	return eventid
}

// CounterEventGroupMember defines a counter event group member at registration
// time. Each member must have an event type of software, hardware, or
// hardware cache, as well as a configuration value that specifies what counter
// information to return.
type CounterEventGroupMember struct {
	EventType EventType
	Config    uint64
}

// RegisterCounterEventGroup registers a performance counter event group.
func (monitor *EventMonitor) RegisterCounterEventGroup(
	name string,
	counters []CounterEventGroupMember,
	handlerFn CounterEventHandlerFn,
	lostRecordFn EventMonitorLostRecordFn,
	options ...RegisterEventOption,
) (int32, uint64, error) {
	if len(counters) < 1 {
		return 0, 0, errors.New("At least one counter must be specified")
	}
	for i, c := range counters {
		switch c.EventType {
		case EventTypeHardware, EventTypeHardwareCache, EventTypeSoftware:
			continue
		default:
			s, ok := EventTypeNames[c.EventType]
			if !ok {
				s = fmt.Sprintf("%d", c.EventType)
			}
			return 0, 0, fmt.Errorf("Counter %d event type %s is invalid",
				i, s)
		}
	}

	opts := newRegisterEventOptions()
	opts.processOptions(options...)
	if len(opts.filter) > 0 {
		return 0, 0, errors.New("Counter events do not support filters")
	}

	if opts.eventAttr == nil {
		opts.eventAttr = &EventAttr{}
	} else {
		attr := *opts.eventAttr
		opts.eventAttr = &attr
	}
	opts.eventAttr.SampleType |= PERF_SAMPLE_READ
	opts.eventAttr.ReadFormat = PERF_FORMAT_GROUP | PERF_FORMAT_ID |
		PERF_FORMAT_TOTAL_TIME_ENABLED | PERF_FORMAT_TOTAL_TIME_RUNNING
	opts.eventAttr.Disabled = opts.disabled
	fixupEventAttr(opts.eventAttr)

	// For our purposes, leaders should always be pinned. Note that
	// fixupEventAttr() sets Pinned to false for all other events in the
	// group.
	leaderAttr := *opts.eventAttr
	leaderAttr.Type = perfTypeFromEventType(counters[0].EventType)
	leaderAttr.Config = counters[0].Config
	leaderAttr.Pinned = true
	group, err := monitor.newEventGroup(leaderAttr)
	if err != nil {
		return 0, 0, err
	}
	group.name = name
	group.lostRecordFn = lostRecordFn

	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	monitor.registerNewEventGroup(group)
	handler := counterEventSampleHandler{handlerFn: handlerFn}
	newsources := make([]EventSource, len(group.leaders))
	for i, leader := range group.leaders {
		newsources[i] = leader.source
	}
	eventID := monitor.newRegisteredEvent(name, newsources, nil, nil,
		counters[0].EventType, handler, &leaderAttr, group, true, 0)
	if err != nil {
		monitor.unregisterEventGroup(group)
		return 0, 0, err
	}

	for i := 1; i < len(counters); i++ {
		_, err = monitor.newRegisteredPerfEvent(
			name, counters[i].Config, nil, opts,
			counters[i].EventType, handler, 0, group.groupID)
		if err != nil {
			monitor.unregisterEventGroup(group)
			return 0, 0, err
		}
	}

	return group.groupID, eventID, nil
}

// RegisterTracepoint is used to register a tracepoint with an EventMonitor.
// The tracepoint is selected by name and it must exist in the running Linux
// kernel. An event ID is returned that is unique to the EventMonitor and is
// to be used to unregister the event. The event ID will also be passed to
// the EventMonitor's dispatch function.
func (monitor *EventMonitor) RegisterTracepoint(
	name string,
	handlerFn TraceEventHandlerFn,
	groupID int32,
	options ...RegisterEventOption,
) (uint64, error) {
	opts := newRegisterEventOptions()
	opts.processOptions(options...)

	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	return monitor.newRegisteredTraceEvent(name, handlerFn, opts,
		EventTypeTracepoint, groupID)
}

// RegisterKprobe is used to register a kprobe with an EventMonitor. The kprobe
// will first be registered with the kernel, and then registered with the
// EventMonitor. An event ID is returned that is unqiue to the EventMonitor and
// is to be used to unregister the event. The event ID will also be passed to
// the EventMonitor's dispatch function.
func (monitor *EventMonitor) RegisterKprobe(
	address string,
	onReturn bool,
	output string,
	handlerFn TraceEventHandlerFn,
	groupID int32,
	options ...RegisterEventOption,
) (uint64, error) {
	opts := newRegisterEventOptions()
	opts.processOptions(options...)

	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	var name string
	if opts.name == "" {
		name = monitor.newProbeName()
	} else {
		name = fmt.Sprintf("capsule8/sensor_%d_%s",
			os.Getpid(), opts.name)
	}
	err := monitor.addKprobe(name, address, onReturn, output)
	if err != nil {
		return 0, err
	}

	eventid, err := monitor.newRegisteredTraceEvent(name, handlerFn, opts,
		EventTypeKprobe, groupID)
	if err != nil {
		monitor.removeKprobe(name)
		return 0, err
	}

	return eventid, nil
}

// RegisterUprobe is used to register a uprobe with an EventMonitor. The uprobe
// will first be registered with the kernel, and then registered with the
// EventMonitor. An event ID is returned that is unique to the EventMonitor and
// is to be used to unregister the event. The event ID will also be passed to
// the EventMonitor's dispatch function.
func (monitor *EventMonitor) RegisterUprobe(
	bin string,
	address string,
	onReturn bool,
	output string,
	handlerFn TraceEventHandlerFn,
	groupID int32,
	options ...RegisterEventOption,
) (uint64, error) {
	opts := newRegisterEventOptions()
	opts.processOptions(options...)

	// If the address looks like a symbol that needs to be resolved, it
	// must be resolved here and now. The kernel does not do symbol
	// resolution for uprobes.
	if address[0] == '_' || unicode.IsLetter(rune(address[0])) {
		var err error
		address, err = monitor.resolveSymbol(bin, address)
		if err != nil {
			return 0, err
		}
	}

	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	var name string
	if opts.name == "" {
		name = monitor.newProbeName()
	} else {
		name = fmt.Sprintf("capsule8/sensor_%d_%s",
			os.Getpid(), opts.name)
	}
	err := monitor.addUprobe(name, bin, address, onReturn, output)
	if err != nil {
		return 0, err
	}

	eventid, err := monitor.newRegisteredTraceEvent(name, handlerFn, opts,
		EventTypeUprobe, groupID)
	if err != nil {
		monitor.removeUprobe(name)
		return 0, err
	}

	return eventid, nil
}

func baseAddress(file *elf.File, vaddr uint64) uint64 {
	if file.FileHeader.Type != elf.ET_EXEC {
		return 0
	}

	for _, prog := range file.Progs {
		if prog.Type != elf.PT_LOAD {
			continue
		}

		if vaddr < prog.Vaddr || vaddr >= prog.Vaddr+prog.Memsz {
			continue
		}

		return prog.Vaddr
	}

	return 0
}

func symbolOffset(file *elf.File, name string, symbols []elf.Symbol) uint64 {
	for _, sym := range symbols {
		if sym.Name == name {
			return sym.Value - baseAddress(file, sym.Value)
		}
	}

	return 0
}

func (monitor *EventMonitor) resolveSymbol(bin, symbol string) (string, error) {
	file, err := elf.Open(bin)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// We don't know how to deal with anything other than ET_DYN or
	// ET_EXEC types.
	if file.FileHeader.Type != elf.ET_DYN && file.FileHeader.Type != elf.ET_EXEC {
		return "", fmt.Errorf("Executable is of unsupported ELF type %d",
			file.FileHeader.Type)
	}

	// Check symbols followed by dynamic symbols. Ignore errors from either
	// one, because they'll just be about the sections not existing, which
	// is fine. In the end, we'll generate our own error to return to the
	// caller if the symbol isn't found.

	var offset uint64
	symbols, _ := file.Symbols()
	offset = symbolOffset(file, symbol, symbols)
	if offset == 0 {
		symbols, _ = file.DynamicSymbols()
		offset = symbolOffset(file, symbol, symbols)
		if offset == 0 {
			return "", fmt.Errorf("Symbol %q not found in %q",
				symbol, bin)
		}
	}

	return fmt.Sprintf("%#x", offset), nil
}

func (monitor *EventMonitor) removeRegisteredEvent(event *registeredEvent) {
	// This should be called with monitor.lock held

	monitor.events.remove(event.id)

	// event.sources may legitimately be nil for non-perf_event-based events
	if event.sources != nil {
		ids := make([]uint64, 0, len(event.sources))
		for _, source := range event.sources {
			ids = append(ids, source.SourceID())
			if !event.leader {
				source.Close()
			}
		}

		if monitor.isRunning.Load().(bool) {
			monitor.eventAttrMap.remove(ids)
			monitor.eventIDMap.remove(ids)
		} else {
			monitor.eventAttrMap.removeInPlace(ids)
			monitor.eventIDMap.removeInPlace(ids)
		}
	}

	delete(event.group.events, event.id)

	switch event.eventType {
	case EventTypeKprobe:
		monitor.removeKprobe(event.name)
		monitor.traceFormats.remove(event.formatID)
	case EventTypeUprobe:
		monitor.removeUprobe(event.name)
		monitor.traceFormats.remove(event.formatID)
	}
}

// UnregisterEvent is used to remove a previously registered event from an
// EventMonitor. The event can be of any type and is specified by the event
// ID that was returned when the event was initially registered with the
// EventMonitor.
func (monitor *EventMonitor) UnregisterEvent(eventid uint64) error {
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	if event, ok := monitor.events.lookup(eventid); ok {
		monitor.removeRegisteredEvent(event)
		return nil
	}
	return errors.New("event is not registered")
}

// RegisteredEventType returns the type of an event
func (monitor *EventMonitor) RegisteredEventType(
	eventID uint64,
) (EventType, bool) {
	if event, ok := monitor.events.lookup(eventID); ok {
		return event.eventType, true
	}
	return EventTypeInvalid, false
}

// RegisteredEventFields returns the fields that are defined for the specified
// event identifier.
func (monitor *EventMonitor) RegisteredEventFields(
	eventID uint64,
) expression.FieldTypeMap {
	if event, ok := monitor.events.lookup(eventID); ok {
		return event.fields
	}
	return nil
}

// Close gracefully cleans up an EventMonitor instance. If the EventMonitor
// is still running when Close is called, it will first be stopped. After
// Close completes, the EventMonitor instance cannot be reused.
func (monitor *EventMonitor) Close() error {
	// if the monitor is running, stop it and wait for it to stop
	monitor.Stop(true)

	// This lock isn't strictly necessary -- by the time .Close() is
	// called, it would be a programming error for multiple go routines
	// to be trying to close the monitor or update events. It doesn't
	// hurt to lock, so do it anyway just to be on the safe side.
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	for _, group := range monitor.groups {
		group.cleanup()
	}
	monitor.groups = nil

	// Make a copy of monitor.events.getMap() values so that we're not
	// enumerating and mutating the map at the same time.
	eventsMap := monitor.events.getMap()
	eventsList := make([]*registeredEvent, 0, len(eventsMap))
	for _, event := range eventsMap {
		eventsList = append(eventsList, event)
	}
	for _, event := range eventsList {
		monitor.removeRegisteredEvent(event)
	}

	if len(monitor.eventAttrMap.getMap()) != 0 {
		panic("internal error: stray event attrs left after monitor Close")
	}

	if len(monitor.eventIDMap.getMap()) != 0 {
		panic("internal error: stray event IDs left after monitor Close")
	}

	groups := monitor.groupLeaders.getMap()
	for _, pgl := range groups {
		pgl.cleanup()
	}

	if monitor.cgroups != nil {
		for _, fd := range monitor.cgroups {
			unix.Close(fd)
		}
		monitor.cgroups = nil
	}

	if monitor.eventSourceController != nil {
		monitor.eventSourceController.Close()
		monitor.eventSourceController = nil
	}
	if monitor.handlerEvent != nil {
		monitor.handlerEvent.Close()
		monitor.handlerEvent = nil
	}

	return nil
}

// Disable is used to disable a registered event. The event to disable is
// specified by its event ID as returned when the event was initially
// registered with the EventMonitor.
func (monitor *EventMonitor) Disable(eventid uint64) {
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	if event, ok := monitor.events.lookup(eventid); ok {
		for _, source := range event.sources {
			source.Disable()
		}
	}
}

// DisableAll disables all events that are registered with the EventMonitor.
func (monitor *EventMonitor) DisableAll() {
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	for _, event := range monitor.events.getMap() {
		for _, source := range event.sources {
			source.Disable()
		}
	}
}

// DisableGroup disables all events for an event group.
func (monitor *EventMonitor) DisableGroup(groupID int32) error {
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	if group, ok := monitor.groups[groupID]; ok {
		group.disable()
		return nil
	}
	return fmt.Errorf("Group ID %d does not exist", groupID)
}

// Enable is used to enable a registered event. The event to enable is
// specified by its event ID as returned when the event was initially
// registered with the EventMonitor.
func (monitor *EventMonitor) Enable(eventid uint64) {
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	if event, ok := monitor.events.lookup(eventid); ok {
		for _, source := range event.sources {
			source.Enable()
		}
	}
}

// EnableAll enables all events that are registered with the EventMonitor.
func (monitor *EventMonitor) EnableAll() {
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	for _, event := range monitor.events.getMap() {
		for _, source := range event.sources {
			source.Enable()
		}
	}
}

// EnableGroup enables all events for an event group.
func (monitor *EventMonitor) EnableGroup(groupID int32) error {
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	if group, ok := monitor.groups[groupID]; ok {
		group.enable()
		return nil
	}
	return fmt.Errorf("Group ID %d does not exist", groupID)
}

// SetFilter is used to set or remove a filter from a registered event.
func (monitor *EventMonitor) SetFilter(
	eventid uint64,
	filter string,
) error {
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	if event, ok := monitor.events.lookup(eventid); ok {
		expr, err := expression.ParseString(filter,
			expression.ParseModeKernelFilter, event.fields)
		if err != nil {
			return fmt.Errorf("Error in filter: %v", err)
		}
		return event.setFilter(expr)
	}
	return fmt.Errorf("Event %d does not exist", eventid)
}

type sampleMerger struct {
	samples     [][]Sample
	indices     []int
	origIndices []int
}

func (m *sampleMerger) next() (Sample, bool) {
	nSources := len(m.indices)
	if nSources == 0 {
		return Sample{}, true
	}

	// Samples from each ringbuffer will be in timestamp order; therefore,
	// we can simply look at the first element in each source to find the
	// next one to return.

	if nSources == 1 {
		sample := m.samples[0][m.indices[0]]
		m.indices[0]++
		if m.indices[0] == len(m.samples[0]) {
			m.samples = nil
			m.indices = nil
		}
		return sample, false
	}

	index := 0
	value := m.samples[0][m.indices[0]].Time
	for i := 1; i < nSources; i++ {
		if v := m.samples[i][m.indices[i]].Time; v < value {
			index = i
			value = v
		}
	}
	sample := m.samples[index][m.indices[index]]

	m.indices[index]++
	if m.indices[index] == len(m.samples[index]) {
		// Make sure to actually swap these because of the shenanigans
		// we'll be engaging in later with free lists. If we don't swap
		// we will end up with a double release on the samples.
		nSources--
		m.indices[index], m.indices[nSources] = m.indices[nSources], m.indices[index]
		m.indices = m.indices[:nSources]
		m.samples[index], m.samples[nSources] = m.samples[nSources], m.samples[index]
		m.samples = m.samples[:nSources]
	}

	return sample, false
}

func (monitor *EventMonitor) newSampleMerger(
	samples [][]Sample,
) sampleMerger {
	indices := monitor.acquireIndices(len(samples), len(samples))
	return sampleMerger{
		samples:     samples,
		indices:     indices,
		origIndices: indices,
	}
}

func (monitor *EventMonitor) acquireIndices(l, c int) []int {
	// This function is called only from the handler goroutine

	n := len(monitor.indicesFreeList) - 1
	for i := n; i >= 0; i-- {
		if cap(monitor.indicesFreeList[i]) >= c {
			indices := monitor.indicesFreeList[i]
			if i != n {
				monitor.indicesFreeList[i] =
					monitor.indicesFreeList[n]
			}
			monitor.indicesFreeList = monitor.indicesFreeList[:n]
			if l > 0 {
				indices = indices[:l]
				for x := l - 1; x >= 0; x-- {
					indices[x] = 0
				}
			}
			return indices
		}
	}

	size := monitor.defaultCacheSize
	if c > size {
		size = roundPow2(c) * 2
	}
	return make([]int, l, size)
}

func (monitor *EventMonitor) releaseIndices(indices []int) {
	monitor.indicesFreeList = append(monitor.indicesFreeList, indices[:0])
}

// eventSourceBuffer is used to collect buffers of sample data from event
// sources, one per ring buffer. The buffer field is always the root of a
// (possibly) shared byte slice so that comparisons can be done later to
// safely release it for re-use. The offset field points to the start of
// the buffer that's in use by this eventSourceBuffer, and the size field
// is the number of bytes belonging to this eventSourceBuffer.
type eventSourceBuffer struct {
	buffer []byte
	offset int
	size   int
}

func (esb *eventSourceBuffer) countSamples() (n int) {
	data := esb.buffer[esb.offset : esb.offset+esb.size]
	for len(data) >= sizeofEventHeader {
		// Each sample begins with an eventHeader struct
		// eventHeader.Size begins at offset 6
		l := int(*(*uint16)(unsafe.Pointer(&data[6])))
		if l == 0 || l > len(data) {
			glog.V(2).Infof("Bad eventHeader length: %d (%d left)",
				l, len(data))
			return
		}
		data = data[l:]
		n++
	}
	return
}

func (monitor *EventMonitor) setupNewBufferList(c int) {
	// This function is called only from the event source goroutine;
	// however, the buffer list free list is shared with the handler
	// goroutine. Therefore, portions must be protected with a lock.

	// This function is responsible for setting up a new collection of
	// sample data from event sources. A buffer list is a list of
	// eventSourceBuffer structs that'll be released for re-use again
	// later when it's no longer needed.

	monitor.currentBuffer = nil

	monitor.lock.Lock()
	n := len(monitor.bufferListFreeList) - 1
	for i := n; i >= 0; i-- {
		if cap(monitor.bufferListFreeList[i]) >= c {
			monitor.bufferList = monitor.bufferListFreeList[i]
			if i != n {
				monitor.bufferListFreeList[i] =
					monitor.bufferListFreeList[n]
			}
			monitor.bufferListFreeList =
				monitor.bufferListFreeList[:n]
			monitor.lock.Unlock()
			return
		}
	}
	monitor.lock.Unlock()

	// Allocate a new buffer list. Possibly allocate it larger than the
	// currently needed limit, because it's later going to be cached for
	// reuse. Use the monitor's default cache size unless the requested
	// limit is larger, in which case use that limit rounded up to the
	// nearest power of 2.
	size := monitor.defaultCacheSize
	if c > size {
		size = roundPow2(c) * 2
	}
	monitor.bufferList = make([]eventSourceBuffer, 0, size)
}

func (monitor *EventMonitor) acquireBuffer(size int) ([]byte, int) {
	// This function is called only from the event source goroutine;
	// however, the buffer list free list is shared with the handler
	// goroutine. Therefore, portions must be protected with a lock.

	// This is called by the event source code to acquire a buffer for use.
	// If there is room in the current buffer to share with the request, do
	// that; otherwise, grab a buffer from a free list or allocate a new
	// one. On exit, currentBuffer should always be non-nil, and the
	// returned buffer should be added to bufferList.
	b := eventSourceBuffer{
		size: size,
	}

	if buffer := monitor.currentBuffer; buffer != nil && size <= cap(buffer)-len(buffer) {
		b.offset = len(buffer)
		monitor.currentBuffer = buffer[:b.offset+size]
	} else {
		monitor.lock.Lock()
		if l := len(monitor.bufferFreeList); l > 0 {
			monitor.currentBuffer = monitor.bufferFreeList[l-1][:size]
			monitor.bufferFreeList = monitor.bufferFreeList[:l-1]
			monitor.lock.Unlock()
		} else {
			monitor.lock.Unlock()
			monitor.currentBuffer = make([]byte, size,
				monitor.ringBufferNumPages*os.Getpagesize())
		}
	}

	b.buffer = monitor.currentBuffer
	monitor.bufferList = append(monitor.bufferList, b)
	return b.buffer, b.offset
}

func (monitor *EventMonitor) dequeueBufferList(
	wait time.Duration,
) []eventSourceBuffer {
	// This function is called only from the handler goroutine; however,
	// the buffer list free list is shared with the event source goroutine.
	// Therefore, portions must be protected with a lock.

	monitor.lock.Lock()

	// Don't loop here to retry when the wait returns because the caller
	// may need to check for the monitor's stop request.

	var list []eventSourceBuffer
	if q := monitor.bufferListQueueHead; q != nil {
		if q.next == nil {
			monitor.bufferListQueueTail = nil
		}
		monitor.bufferListQueueHead = q.next
		list = q.list
		q.list = nil
		q.next = monitor.bufferListQueueFreeList
		monitor.bufferListQueueFreeList = q
		monitor.lock.Unlock()
	} else {
		monitor.lock.Unlock()
		monitor.handlerEvent.Wait(wait)
	}

	return list
}

func (monitor *EventMonitor) enqueueBufferList() {
	// This function is called only from the event source goroutine;
	// however, the queue is shared with the handler goroutine. Therefore,
	// we must protect it with a lock.

	monitor.lock.Lock()

	q := monitor.bufferListQueueFreeList
	if q == nil {
		q = &queuedBufferList{}
	} else {
		monitor.bufferListQueueFreeList = q.next
		q.next = nil
	}
	q.list = monitor.bufferList
	monitor.bufferList = nil

	if monitor.bufferListQueueTail == nil {
		monitor.bufferListQueueHead = q
	} else {
		monitor.bufferListQueueTail.next = q
	}
	monitor.bufferListQueueTail = q

	monitor.lock.Unlock()

	if !monitor.handlingSamples.Load().(bool) {
		monitor.handlerEvent.Signal()
	}
}

func (monitor *EventMonitor) releaseBufferList(list []eventSourceBuffer) {
	// This function is called only from the handler goroutine; however,
	// the buffer list free list is shared with the event source goroutine.
	// Therefore, we must protect it with a lock.

	monitor.lock.Lock()

	// First release all buffers back to the buffer free list
	if l := len(list); l > 0 {
		monitor.bufferFreeList =
			append(monitor.bufferFreeList,
				list[0].buffer)
		for i := 1; i < l; i++ {
			prev := uintptr(unsafe.Pointer(&list[i-1].buffer[0]))
			this := uintptr(unsafe.Pointer(&list[i].buffer[0]))
			if prev != this {
				monitor.bufferFreeList =
					append(monitor.bufferFreeList,
						list[i].buffer)
			}
		}
	}

	// Then release the buffer list itself back to the buffer list free list
	monitor.bufferListFreeList = append(monitor.bufferListFreeList, list[:0])

	monitor.lock.Unlock()
}

func (monitor *EventMonitor) acquireSampleBatch(l, c int) [][]Sample {
	// This function is called only from the handler goroutine

	n := len(monitor.sampleBatchFreeList) - 1
	for i := n; i >= 0; i-- {
		if cap(monitor.sampleBatchFreeList[i]) >= c {
			batch := monitor.sampleBatchFreeList[i]
			if i != n {
				monitor.sampleBatchFreeList[i] =
					monitor.sampleBatchFreeList[n]
			}
			monitor.sampleBatchFreeList =
				monitor.sampleBatchFreeList[:n]
			if l > 0 {
				batch = batch[:l]
				for x := l - 1; x >= 0; x-- {
					batch[x] = nil
				}
			}
			return batch[:l]
		}
	}

	// Allocate a new batch and return it. It'll get put into the free list
	// later.
	size := monitor.defaultCacheSize
	if c > size {
		size = roundPow2(c) * 2
	}
	return make([][]Sample, l, size)
}

func (monitor *EventMonitor) releaseSampleBatch(batch [][]Sample) {
	// This function is called only from the handler goroutine

	monitor.sampleBatchFreeList = append(monitor.sampleBatchFreeList, batch[:0])
}

func (monitor *EventMonitor) acquireSampleList(l, c int) []Sample {
	// This function is called only from the handler goroutine

	idx := roundPow2Exp(c) - 1
	list := monitor.sampleFreeList[idx]
	if list == nil {
		return make([]Sample, l, roundPow2(c))
	}
	monitor.sampleFreeList[idx] = list.next
	list.next = monitor.sampleListFreeList
	monitor.sampleListFreeList = list
	samples := list.samples[:l]
	list.samples = nil
	for i := l - 1; i >= 0; i-- {
		samples[i] = Sample{}
	}
	return samples
}

func (monitor *EventMonitor) releaseSampleList(samples []Sample) {
	// This function is called only from the handler goroutine

	list := monitor.sampleListFreeList
	if list == nil {
		list = &sampleList{}
	} else {
		monitor.sampleListFreeList = list.next
	}

	list.samples = samples[:0]
	idx := roundPow2Exp(cap(samples)) - 1
	list.next = monitor.sampleFreeList[idx]
	monitor.sampleFreeList[idx] = list
}

func (monitor *EventMonitor) readSamples(
	b eventSourceBuffer,
	formatMap map[uint64]*EventAttr,
) []Sample {
	// This function is only called from the handler goroutine.
	// This function is expected to never return nil.

	nsamples := b.countSamples()
	samples := monitor.acquireSampleList(nsamples, nsamples)

	var i int
	data := b.buffer[b.offset : b.offset+b.size]
	for len(data) >= sizeofEventHeader {
		n, err := samples[i].read(data, nil, formatMap)
		data = data[n:]
		if err == nil {
			i++
		}
	}
	samples = samples[:i]

	if !HaveClockID {
		for i = 0; i < len(samples); i++ {
			samples[i].Time =
				uint64(int64(samples[i].Time) -
					TimeOffsets[samples[i].CPU] +
					TimeBase)
		}
	}

	return samples
}

func (monitor *EventMonitor) handleSamples(samples [][]Sample) {
	// This function is only called from the handler goroutine.

	eventIDMap := monitor.eventIDMap.getMap()
	eventMap := monitor.events.getMap()

	m := monitor.newSampleMerger(samples)
	for {
		sample, done := m.next()
		if done {
			break
		}

		streamID := sample.SampleID.StreamID
		eventid, ok := eventIDMap[streamID]
		if !ok {
			continue
		}
		event, ok := eventMap[eventid]
		if !ok {
			continue
		}

		event.handler.handleSample(event, &sample, monitor)
	}
	monitor.releaseIndices(m.origIndices)
}

func (monitor *EventMonitor) flushPendingSamples() {
	monitor.handlingSamples.Store(true)

	samples := monitor.pendingSamples
	monitor.pendingSamples = nil

	monitor.handleSamples(samples)

	for _, s := range samples {
		monitor.releaseSampleList(s)
	}
	monitor.releaseSampleBatch(samples)
	monitor.releaseBufferList(monitor.pendingSamplesBufferList)
	monitor.pendingSamplesBufferList = nil

	monitor.handlingSamples.Store(false)
}

func (monitor *EventMonitor) handlerLoop() {
	// This is the handler goroutine's main loop. It runs on the goroutine
	// that calls monitor.Run.

	for !monitor.stopRequested.Load().(bool) {
		var timeout time.Duration = -1 // -1 == wait forever
		if monitor.pendingSamples != nil {
			timeout = 100 * time.Millisecond
		}
		list := monitor.dequeueBufferList(timeout)
		if list == nil {
			if monitor.pendingSamples != nil {
				monitor.flushPendingSamples()
			}
			continue
		}

		monitor.handlingSamples.Store(true)
		batchSize := len(list) + len(monitor.pendingSamples)
		samples := monitor.acquireSampleBatch(0, batchSize)
		for _, pendingSamples := range monitor.pendingSamples {
			samples = append(samples, pendingSamples)
		}
		if cap(monitor.pendingSamples) >= len(list)-1 {
			monitor.pendingSamples = monitor.pendingSamples[:0]
		} else if monitor.pendingSamples != nil {
			oldPendingSamples := monitor.pendingSamples
			monitor.pendingSamples =
				monitor.acquireSampleBatch(0, len(list)-1)
			monitor.releaseSampleBatch(oldPendingSamples)
		} else {
			monitor.pendingSamples =
				monitor.acquireSampleBatch(0, len(list)-1)
		}

		var lastTimestamp uint64
		formatMap := monitor.eventAttrMap.getMap()
		for _, b := range list {
			groupSamples := monitor.readSamples(b, formatMap)
			l := len(groupSamples)
			if l == 0 {
				monitor.releaseSampleList(groupSamples)
				continue
			}
			if lastTimestamp == 0 {
				lastTimestamp = groupSamples[l-1].Time
			} else {
				for i := l - 1; i >= 0; i-- {
					if groupSamples[i].Time <= lastTimestamp {
						break
					}
					l--
				}
				if l != len(groupSamples) {
					n := len(groupSamples) - l
					pendingSamples :=
						monitor.acquireSampleList(n, n)
					copy(pendingSamples, groupSamples[l:])
					groupSamples = groupSamples[:l]
					monitor.pendingSamples =
						append(monitor.pendingSamples,
							pendingSamples)
				}
			}
			if l > 0 {
				samples = append(samples, groupSamples)
			} else {
				monitor.releaseSampleList(groupSamples)
			}
		}

		monitor.handleSamples(samples)

		if monitor.pendingSamplesBufferList != nil {
			monitor.releaseBufferList(monitor.pendingSamplesBufferList)
		}
		if len(monitor.pendingSamples) == 0 {
			monitor.releaseBufferList(list)
			if monitor.pendingSamples != nil {
				monitor.releaseSampleBatch(monitor.pendingSamples)
			}
			monitor.pendingSamples = nil
			monitor.pendingSamplesBufferList = nil
		} else {
			monitor.pendingSamplesBufferList = list
		}
		for _, s := range samples {
			monitor.releaseSampleList(s)
		}
		monitor.releaseSampleBatch(samples)

		monitor.handlingSamples.Store(false)
	}
}

func (monitor *EventMonitor) eventSourceLoop() {
	// This is the event source loop. It runs on a goroutine all its own
	// and does nothing but read data from the ringbuffers and queue that
	// data up for the handler goroutine to deal with.

	// Do this to avoid a runtime.newobject call for each iteration below.
	// Why? Because passing monitor.acquireBuffer each time requires a new
	// object to be created to bind monitor.acquireBuffer to this specific
	// instance of monitor. It's always the same instance, so we should
	// just do it once.
	acquireBuffer := monitor.acquireBuffer

	controller := monitor.eventSourceController
	for !monitor.stopRequested.Load().(bool) {
		// controller.Wait() will return the list of groups that are
		// ready for reading, but we're not going to pay any attention
		// to that and poll all of the ringbuffers because the kernel's
		// wakeup notifications here seem to be really wonky and slow.
		// If we only pay attention to the ids that controller.Wait says
		// are ready, we end up getting a lot of ordering problems that
		// just go away by reading all of the ringbuffers.
		if _, err := controller.Wait(); err != nil {
			glog.Fatalf("Unexpected error while servicing event sources: %v", err)
		}

		if leaders := monitor.groupLeaders.getMap(); len(leaders) > 0 {
			monitor.setupNewBufferList(len(leaders))
			for _, pgl := range leaders {
				if pgl.active() {
					pgl.source.Read(acquireBuffer)
				}
			}
			monitor.enqueueBufferList()
		}

		monitor.checkDyingGroups(true)
	}

	monitor.wg.Done()
}

func (monitor *EventMonitor) checkDyingGroups(later bool) {
	// It's pretty rare that dyingGroups will be non-nil, so it would be
	// nice if we could avoid the lock here, but the reality is that futex
	// based locks are pretty lightweight, so live with it unless profiling
	// shows that it's a problem.
	monitor.lock.Lock()
	dyingGroups := monitor.dyingGroups
	monitor.dyingGroups = nil
	monitor.lock.Unlock()

	if len(dyingGroups) > 0 {
		if later {
			go func() {
				monitor.finalizeDyingGroups(dyingGroups)
			}()
		} else {
			monitor.finalizeDyingGroups(dyingGroups)
		}
	}
}

func (monitor *EventMonitor) finalizeDyingGroups(groups []*eventMonitorGroup) {
	ids := make(map[uint64]struct{})
	for _, group := range groups {
		for _, pgl := range group.leaders {
			if atomic.LoadInt32(&pgl.state) == perfGroupLeaderStateClosing {
				pgl.cleanup()
			}
			ids[pgl.source.SourceID()] = struct{}{}
		}
	}
	monitor.groupLeaders.remove(ids)
}

// Run puts an EventMonitor into the running state. While an EventMonitor is
// running, samples will be pulled from event sources, decoded, and dispatched
// to functions as specified when kprobes, tracepoints, etc. are registered.
func (monitor *EventMonitor) Run() error {
	monitor.lock.Lock()
	if monitor.isRunning.Load().(bool) {
		monitor.lock.Unlock()
		return errors.New("monitor is already running")
	}
	monitor.isRunning.Store(true)
	monitor.stopRequested.Store(false)
	monitor.lock.Unlock()

	monitor.wg.Add(1)
	go monitor.eventSourceLoop()

	monitor.handlerLoop()

	monitor.lock.Lock()
	monitor.isRunning.Store(false)
	monitor.cond.Broadcast()
	monitor.lock.Unlock()

	monitor.checkDyingGroups(false)

	return nil
}

// Stop stops a running EventMonitor. If the EventMonitor is not running, this
// function does nothing. Once an EventMonitor has been stopped, it may be
// restarted again. Whether Stop waits for the EventMonitor to fully stop is
// optional, but if the caller does not wait there is no other mechanism by
// which the caller may learn whether the EventMonitor is stopped.
func (monitor *EventMonitor) Stop(wait bool) {
	monitor.lock.Lock()

	if !monitor.isRunning.Load().(bool) {
		monitor.lock.Unlock()
		return
	}

	// Request a stop by setting the flag and waking all of our goroutines
	// up.
	monitor.stopRequested.Store(true)
	monitor.eventSourceController.Wakeup() // eventSourceLoop
	monitor.handlerEvent.Signal()          // handlerLoop

	if !wait {
		monitor.lock.Unlock()
	} else {
		for monitor.isRunning.Load().(bool) {
			// Wait for condition to signal that Run() is done
			monitor.cond.Wait()
		}
		monitor.lock.Unlock()

		// Wait for other goroutines to exit
		monitor.wg.Wait()
	}
}

var groupEventAttr = EventAttr{
	Type:     PERF_TYPE_SOFTWARE,
	Config:   PERF_COUNT_SW_DUMMY, // Added in Linux 3.12
	Disabled: true,
}

func (monitor *EventMonitor) initializeGroupLeaders(
	pid int,
	flags uintptr,
	attr EventAttr,
) (pgls []*perfGroupLeader, err error) {
	ncpu := monitor.procFS.NumCPU()
	pgls = make([]*perfGroupLeader, ncpu)
	for cpu := 0; cpu < ncpu; cpu++ {
		pgl := &perfGroupLeader{
			state: perfGroupLeaderStateActive,
		}
		pgl.source, err =
			monitor.eventSourceController.NewEventSourceLeader(
				attr, pid, cpu, flags)
		if err != nil {
			break
		}

		pgls[cpu] = pgl
	}

	if err != nil {
		for _, pgl := range pgls {
			if pgl == nil {
				break
			}
			pgl.cleanup()
		}
	}

	return
}

func (monitor *EventMonitor) newEventGroup(
	attr EventAttr,
) (*eventMonitorGroup, error) {
	ncpu := monitor.procFS.NumCPU()
	nleaders := (len(monitor.cgroups) + len(monitor.pids)) * ncpu
	leaders := make([]*perfGroupLeader, 0, nleaders)

	if monitor.cgroups != nil {
		flags := monitor.perfEventOpenFlags | PERF_FLAG_PID_CGROUP
		for _, fd := range monitor.cgroups {
			pgls, err := monitor.initializeGroupLeaders(fd, flags,
				attr)
			if err != nil {
				for _, pgl := range leaders {
					pgl.cleanup()
				}
				return nil, err
			}
			leaders = append(leaders, pgls...)
		}
	}

	if monitor.pids != nil {
		flags := monitor.perfEventOpenFlags
		for _, pid := range monitor.pids {
			pgls, err := monitor.initializeGroupLeaders(pid, flags,
				attr)
			if err != nil {
				for _, pgl := range leaders {
					pgl.cleanup()
				}
				return nil, err
			}
			leaders = append(leaders, pgls...)
		}
	}

	return &eventMonitorGroup{
		leaders: leaders,
		events:  make(map[uint64]*registeredEvent),
		monitor: monitor,
	}, nil
}

func (monitor *EventMonitor) registerNewEventGroup(group *eventMonitorGroup) {
	// This should be called with monitor.lock LOCKED!

	group.groupID = monitor.nextGroupID
	monitor.nextGroupID++
	monitor.groups[group.groupID] = group

	if monitor.isRunning.Load().(bool) {
		monitor.groupLeaders.update(group.leaders)
	} else {
		monitor.groupLeaders.updateInPlace(group.leaders)
	}
}

// RegisterEventGroup creates a new event group that can be used for grouping
// events. If the kernel reports lost records, the function specified here will
// be called with relevent information.
func (monitor *EventMonitor) RegisterEventGroup(
	name string,
	lostRecordFn EventMonitorLostRecordFn,
) (int32, error) {
	group, err := monitor.newEventGroup(groupEventAttr)
	if err != nil {
		return -1, err
	}
	group.name = name
	group.lostRecordFn = lostRecordFn

	monitor.lock.Lock()
	monitor.registerNewEventGroup(group)
	monitor.lock.Unlock()

	if len(group.name) == 0 {
		group.name = fmt.Sprintf("EventGroup %d", group.groupID)
	}

	return group.groupID, nil
}

func (monitor *EventMonitor) unregisterEventGroup(group *eventMonitorGroup) {
	// This should be called with monitor.lock LOCKED!!

	delete(monitor.groups, group.groupID)

	group.cleanup()

	if !monitor.isRunning.Load().(bool) {
		ids := make(map[uint64]struct{}, len(group.leaders))
		for _, pgl := range group.leaders {
			ids[pgl.source.SourceID()] = struct{}{}
			pgl.cleanup()
		}
		monitor.groupLeaders.removeInPlace(ids)
	} else {
		monitor.dyingGroups = append(monitor.dyingGroups, group)
		for _, pgl := range group.leaders {
			atomic.StoreInt32(&pgl.state, perfGroupLeaderStateClosing)
		}
	}
}

// UnregisterEventGroup removes a registered event group. If there are any
// events registered with the event group, they will be unregistered as well.
func (monitor *EventMonitor) UnregisterEventGroup(groupID int32) error {
	monitor.lock.Lock()
	defer monitor.lock.Unlock()

	var err error
	if group, ok := monitor.groups[groupID]; ok {
		monitor.unregisterEventGroup(group)
	} else {
		err = fmt.Errorf("Group ID %d does not exist", groupID)
	}
	return err
}

func doProbeCleanup(
	tracingDir, eventsFile string,
	activePids, deadPids map[int]bool,
) {
	eventsFilename := filepath.Join(tracingDir, eventsFile)
	data, err := ioutil.ReadFile(eventsFilename)
	if err != nil {
		return
	}

	var file *os.File

	// Read one line at a time and check for capsule8/sensor_ probes. The
	// pid that created the probe is encoded within. If the pid is dead,
	// remove the probe.
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		name := line[2:strings.Index(line, " ")]
		if !strings.HasPrefix(name, "capsule8/sensor_") {
			continue
		}

		// Capsule8 sensor names are of the form sensor_<pid>_<count>
		var pid int
		fmt.Sscanf(name, "capsule8/sensor_%d_", &pid)
		if activePids[pid] {
			continue
		} else if !deadPids[pid] {
			if syscall.Kill(pid, 0) != syscall.ESRCH {
				activePids[pid] = true
				continue
			}
			deadPids[pid] = true
		}

		cmd := fmt.Sprintf("-:%s\n", name)
		if file == nil {
			file, err = os.OpenFile(eventsFilename, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				glog.Errorf("Couldn't open %s WO+A: %s", eventsFilename, err)
				return
			}
			defer file.Close()
		}
		file.Write([]byte(cmd))
		glog.V(1).Infof("Removed stale probe from %s: %s", eventsFile, name)
	}
}

func cleanupStaleProbes(tracingDir string) {
	activePids := make(map[int]bool)
	deadPids := make(map[int]bool)

	activePids[os.Getpid()] = true

	doProbeCleanup(tracingDir, "kprobe_events", activePids, deadPids)
	doProbeCleanup(tracingDir, "uprobe_events", activePids, deadPids)
}

// NewEventMonitor creates a new EventMonitor instance in the stopped state.
// Once an EventMonitor instance is returned from this function, its Close
// method must be called to clean it up gracefully, even if no events are
// registered or it is never put into the running state.
func NewEventMonitor(options ...EventMonitorOption) (monitor *EventMonitor, err error) {
	opts := eventMonitorOptions{}
	opts.processOptions(options...)

	defer func() {
		if err != nil {
			if monitor != nil {
				monitor.Close()
				monitor = nil
			} else if opts.eventSourceController != nil {
				opts.eventSourceController.Close()
			}
		}
	}()

	// Use the specified procfs as-is or find the host procfs to use if
	// not explicitly specified.
	if opts.procfs == nil {
		var fs *procfs.FileSystem
		if fs, err = procfs.NewFileSystem(""); err != nil {
			return
		}
		if opts.procfs = fs.HostFileSystem(); opts.procfs == nil {
			err = errors.New("Unable to determine host procfs")
			return
		}
	}

	if opts.ringBufferNumPages <= 0 {
		opts.ringBufferNumPages = 8
	}
	if opts.defaultCacheSize == 0 {
		opts.defaultCacheSize = 16
	}
	ncpu := opts.procfs.HostFileSystem().NumCPU()
	opts.defaultCacheSize = roundPow2(opts.defaultCacheSize*ncpu) * 2

	var eventAttr EventAttr
	if opts.defaultEventAttr == nil {
		eventAttr = EventAttr{
			SampleType: PERF_SAMPLE_CPU | PERF_SAMPLE_RAW,
		}
	} else {
		eventAttr = *opts.defaultEventAttr
	}
	fixupEventAttr(&eventAttr)

	// Only allow certain flags to be passed
	opts.flags &= PERF_FLAG_FD_CLOEXEC

	// If no tracing dir was specified, scan mounts for one
	if len(opts.tracingDir) == 0 {
		opts.tracingDir = opts.procfs.TracingDir()
	}
	cleanupStaleProbes(opts.tracingDir)

	// If no perf_event cgroup mountpoint was specified, scan mounts for one
	if len(opts.perfEventDir) == 0 && len(opts.cgroups) > 0 {
		opts.perfEventDir = opts.procfs.PerfEventDir()

		// If we didn't find one, we can't monitor specific cgroups
		if len(opts.perfEventDir) == 0 {
			err = errors.New("Can't monitor specific cgroups without perf_event cgroupfs")
			return
		}
	}

	// If no pids or cgroups were specified, default to monitoring the
	// whole system (pid -1)
	if len(opts.pids) == 0 && len(opts.cgroups) == 0 {
		opts.pids = append(opts.pids, -1)
	}

	// Use the system default event source controller if a specific one to
	// use is not specified.
	if opts.eventSourceController == nil {
		// The shenanigans here are intentional to avoid Go insanity
		// in assigning (*defaultEventSourceController)(nil) to
		// opts.eventSourceController if newDefaultEventSourceController
		// also returns an error (triggering a panic in the deferred
		// cleanup above)
		var controller EventSourceController
		controller, err = newDefaultEventSourceController(opts)
		if err != nil {
			return
		}
		opts.eventSourceController = controller
	}
	monitor = &EventMonitor{
		eventSourceController: opts.eventSourceController,
		nextEventID:           1,
		groups:                make(map[int32]*eventMonitorGroup),
		defaultAttr:           eventAttr,
		tracingDir:            opts.tracingDir,
		procFS:                opts.procfs,
		ringBufferNumPages:    opts.ringBufferNumPages,
		perfEventOpenFlags:    opts.flags | PERF_FLAG_FD_CLOEXEC,
		defaultCacheSize:      opts.defaultCacheSize,
	}
	monitor.cond = sync.Cond{L: &monitor.lock}
	monitor.isRunning.Store(false)
	monitor.stopRequested.Store(false)
	monitor.handlingSamples.Store(false)
	monitor.handlerEvent, err = newDefaultTimedEvent()
	if err != nil {
		return
	}

	// Preallocate cache freelists
	monitor.bufferFreeList =
		make([][]byte, 0, monitor.defaultCacheSize)
	monitor.bufferListFreeList =
		make([][]eventSourceBuffer, 0, monitor.defaultCacheSize)
	monitor.sampleBatchFreeList =
		make([][][]Sample, 0, monitor.defaultCacheSize)

	if len(opts.cgroups) > 0 {
		cgroups := make(map[string]bool, len(opts.cgroups))
		monitor.cgroups = make([]int, 0, len(opts.cgroups))
		for _, cgroup := range opts.cgroups {
			if cgroups[cgroup] {
				glog.V(1).Infof("Ignoring duplicate cgroup %s",
					cgroup)
				continue
			}
			cgroups[cgroup] = true

			var fd int
			path := filepath.Join(opts.perfEventDir, cgroup)
			fd, err = unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
			if err != nil {
				return
			}
			monitor.cgroups = append(monitor.cgroups, fd)
		}
	}

	if len(opts.pids) > 0 {
		pids := make(map[int]bool, len(opts.pids))
		monitor.pids = make([]int, 0, len(opts.pids))
		for _, pid := range opts.pids {
			if pids[pid] {
				glog.V(1).Infof("Ignoring duplicate pid %d",
					pid)
				continue
			}
			pids[pid] = true

			monitor.pids = append(monitor.pids, pid)
		}
	}

	return
}
