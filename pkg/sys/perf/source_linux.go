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

package perf

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/Happyholic1203/capsule8/pkg/sys"
	"github.com/Happyholic1203/capsule8/pkg/sys/proc"
	"github.com/golang/glog"

	"golang.org/x/sys/unix"
)

var clockOnce sync.Once

type defaultTimedEvent struct {
	eventfd int
	pollfds [1]unix.PollFd
}

func newDefaultTimedEvent() (*defaultTimedEvent, error) {
	eventfd, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		return nil, err
	}

	e := &defaultTimedEvent{
		eventfd: eventfd,
	}

	return e, nil
}

func (e *defaultTimedEvent) Close() {
	if e.eventfd != -1 {
		unix.Close(e.eventfd)
		e.eventfd = -1
	}
}

func (e *defaultTimedEvent) Signal() {
	// Increment the eventfd counter by 1
	b := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	unix.Write(e.eventfd, b)
}

func (e *defaultTimedEvent) Wait(timeout time.Duration) bool {
	if timeout >= 0 {
		timeout /= time.Millisecond
	}
	for {
		e.pollfds[0] = unix.PollFd{
			Fd:     int32(e.eventfd),
			Events: unix.POLLIN,
		}
		n, err := unix.Poll(e.pollfds[:], int(timeout))
		if err == unix.EAGAIN || err == unix.EINTR {
			continue
		}
		if err != nil {
			glog.Fatalf("unix.Poll: %v", err)
		}
		if n == 1 {
			// Reset the eventfd counter to 0 by reading it. We
			// don't care about its actual value, so just discard
			// it after reading.
			var b [8]byte
			unix.Read(e.eventfd, b[:])
			return true
		}
		return false
	}
}

var splitCloexec bool

// perfEventOpen is a raw interface to the perf_event_open syscall. Do not do
// any unnecessary mangling of EventAttr here (such as UseClockID) because this
// is used at start up to make the determination of whether that sort of
// mangling should be done.
func perfEventOpen(attr EventAttr, pid, cpu, groupFD int, flags uintptr) (int, error) {
	buf := new(bytes.Buffer)
	attr.write(buf)
	b := buf.Bytes()

	var doCloexec bool
	if splitCloexec && flags&PERF_FLAG_FD_CLOEXEC != 0 {
		doCloexec = true
		flags &= ^PERF_FLAG_FD_CLOEXEC
	}

retry:
	r1, _, errno := unix.Syscall6(unix.SYS_PERF_EVENT_OPEN,
		uintptr(unsafe.Pointer(&b[0])), uintptr(pid), uintptr(cpu),
		uintptr(groupFD), uintptr(flags), uintptr(0))
	if errno != 0 {
		if errno == unix.EINVAL && flags&PERF_FLAG_FD_CLOEXEC != 0 {
			flags &= ^PERF_FLAG_FD_CLOEXEC
			splitCloexec = true
			doCloexec = true
			goto retry
		}
		return int(-1), errno
	}
	if doCloexec {
		_, _, errno = unix.Syscall(unix.SYS_FCNTL, uintptr(r1),
			uintptr(unix.F_SETFL), uintptr(unix.FD_CLOEXEC))
		if errno != 0 {
			unix.Close(int(r1))
			return int(-1), errno
		}
	}
	return int(r1), nil
}

type defaultEventSource struct {
	fd       int
	streamID int
	parent   *defaultEventSourceLeader
}

func (s *defaultEventSource) Close() error {
	if s.fd != 1 {
		if err := unix.Close(s.fd); err != nil {
			return err
		}
		s.fd = -1
	}
	return nil
}

func (s *defaultEventSource) Disable() error {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(s.fd), PERF_EVENT_IOC_DISABLE, 1); errno != 0 {
		return errno
	}
	return nil
}
func (s *defaultEventSource) Enable() error {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(s.fd), PERF_EVENT_IOC_ENABLE, 1); errno != 0 {
		return errno
	}
	return nil
}

func (s *defaultEventSource) SetFilter(filter string) error {
	if f, err := unix.BytePtrFromString(filter); err != nil {
		return err
	} else if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(s.fd), PERF_EVENT_IOC_SET_FILTER, uintptr(unsafe.Pointer(f))); errno != 0 {
		return errno
	}
	return nil
}

func (s *defaultEventSource) SourceID() uint64 {
	return uint64(s.streamID)
}

type defaultEventSourceLeader struct {
	defaultEventSource
	pid        int
	cpu        int
	flags      uintptr
	rb         ringBuffer
	controller *defaultEventSourceController
}

func (s *defaultEventSourceLeader) Close() error {
	if err := s.rb.unmap(); err != nil {
		return err
	}
	if err := s.defaultEventSource.Close(); err != nil {
		return err
	}
	atomic.AddInt64(&s.controller.leaderCount, -1)
	return nil
}

func (s *defaultEventSourceLeader) Disable() error {
	event := unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(s.streamID & 0xffffffff),
		Pad:    int32((s.streamID >> 32) & 0xffffffff),
	}
	return unix.EpollCtl(s.controller.epollFD, unix.EPOLL_CTL_DEL, s.fd, &event)
}

func (s *defaultEventSourceLeader) Enable() error {
	event := unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(s.streamID & 0xffffffff),
		Pad:    int32((s.streamID >> 32) & 0xffffffff),
	}
	return unix.EpollCtl(s.controller.epollFD, unix.EPOLL_CTL_ADD, s.fd, &event)
}

func (s *defaultEventSourceLeader) NewEventSource(
	attr EventAttr,
	flags uintptr,
) (EventSource, error) {
	childSource := &defaultEventSource{
		parent: s,
	}

	if HaveClockID {
		attr.UseClockID = true
		attr.ClockID = unix.CLOCK_MONOTONIC_RAW
	} else {
		attr.UseClockID = false
		attr.ClockID = 0
	}
	var err error
	childSource.fd, err = perfEventOpen(attr, s.pid, s.cpu, s.fd, flags)
	if err != nil {
		return nil, err
	}

	if childSource.streamID, err = unix.IoctlGetInt(childSource.fd, PERF_EVENT_IOC_ID); err != nil {
		childSource.Close()
		return nil, err
	}

	return childSource, nil
}

func (s *defaultEventSourceLeader) Read(
	acquireBuffer func(size int) ([]byte, int),
) {
	s.rb.read(acquireBuffer)
}

type defaultEventSourceController struct {
	epollFD            int
	eventFD            int
	ncpu               int
	ringBufferNumPages int
	leaderCount        int64
	events             []unix.EpollEvent
	pending            []uint64
}

func newDefaultEventSourceController(
	opts eventMonitorOptions,
) (c *defaultEventSourceController, err error) {
	clockOnce.Do(func() {
		attr := EventAttr{
			SamplePeriod:    1,
			Disabled:        true,
			UseClockID:      true,
			ClockID:         unix.CLOCK_MONOTONIC_RAW,
			Watermark:       true,
			WakeupWatermark: 1,
		}
		var fd int
		if fd, err = perfEventOpen(attr, 0, -1, -1, 0); err == nil {
			glog.V(1).Infof("EventMonitor is using ClockID CLOCK_MONOTONIC_RAW")
			unix.Close(fd)
			HaveClockID = true
		}
		err = calculateTimeOffsets(opts.procfs)
	})
	if err != nil {
		return
	}

	c = &defaultEventSourceController{
		epollFD:            -1,
		eventFD:            -1,
		ncpu:               opts.procfs.NumCPU(),
		ringBufferNumPages: opts.ringBufferNumPages,
	}
	defer func() {
		if err != nil {
			c.Close()
			c = nil
		}
	}()

	if c.epollFD, err = unix.EpollCreate1(unix.EPOLL_CLOEXEC); err != nil {
		return
	}

	if c.eventFD, err = unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK); err != nil {
		return
	}

	event := unix.EpollEvent{
		Events: unix.EPOLLIN,
	}
	if err = unix.EpollCtl(c.epollFD, unix.EPOLL_CTL_ADD, c.eventFD, &event); err != nil {
		return
	}

	c.events = make([]unix.EpollEvent, 0, opts.defaultCacheSize+1)
	c.pending = make([]uint64, 0, opts.defaultCacheSize)

	return
}

func collectReferenceSamples(ncpu int) (int64, int64, []int64, error) {
	referenceEventAttr := EventAttr{
		Type:         PERF_TYPE_SOFTWARE,
		Config:       PERF_COUNT_SW_CPU_CLOCK,
		SampleFreq:   1,
		SampleType:   PERF_SAMPLE_TIME,
		Disabled:     true,
		Freq:         true,
		WakeupEvents: 1,
	}
	referenceEventAttr.computeSizes()

	rbs := make([]ringBuffer, ncpu)
	pollfds := make([]unix.PollFd, ncpu)
	for cpu := 0; cpu < ncpu; cpu++ {
		fd, err := perfEventOpen(referenceEventAttr, -1, cpu, -1,
			PERF_FLAG_FD_CLOEXEC)
		if err != nil {
			err = fmt.Errorf("Couldn't open reference event: %s", err)
			return 0, 0, nil, err
		}
		defer unix.Close(fd)
		pollfds[cpu] = unix.PollFd{
			Fd:     int32(fd),
			Events: unix.POLLIN,
		}

		if err = rbs[cpu].init(fd, 1); err != nil {
			err = fmt.Errorf("Couldn't allocate ringbuffer: %s", err)
			return 0, 0, nil, err
		}
		defer rbs[cpu].unmap()
	}

	// Enable all of the events we just registered
	for _, p := range pollfds {
		unix.Syscall(unix.SYS_IOCTL, uintptr(p.Fd), PERF_EVENT_IOC_ENABLE, 1)
	}

	var firstTime int64
	startTime := sys.CurrentMonotonicRaw()

	// Read all samples from each group, but keep only the first for each
	// Don't wait forever. Return a timeout error if samples don't arrive
	// within 2 seconds.
	const timeout = 2 * time.Second
	glog.V(2).Infof("Calculating CPU time offsets (max wait %d nsec)", timeout)

	nsamples := 0
	samples := make([]int64, ncpu)
	timeoutAt := sys.CurrentMonotonicRaw() + int64(timeout)
	for nsamples < ncpu {
		now := sys.CurrentMonotonicRaw()
		if now >= timeoutAt {
			return 0, 0, nil, errors.New("Timeout while reading clock offset samples")
		}
		n, err := unix.Poll(pollfds, int((timeoutAt-now)/int64(time.Millisecond)))
		if err != nil && err != unix.EINTR {
			return 0, 0, nil, err
		}
		if n == 0 {
			continue
		}
		if firstTime == 0 {
			firstTime = sys.CurrentMonotonicRaw()
		}

		for cpu, p := range pollfds {
			if p.Revents&unix.POLLIN != unix.POLLIN {
				continue
			}

			var data []byte
			rbs[cpu].read(func(size int) ([]byte, int) {
				data = make([]byte, size)
				return data, 0
			})
			if data != nil {
				s := Sample{}
				_, err = s.read(data, &referenceEventAttr, nil)
				if err == nil {
					samples[cpu] = int64(s.Time)
					nsamples++
				}
			}

			if samples[cpu] != 0 {
				pollfds[cpu].Events &= ^unix.POLLIN
			}
		}
	}

	return startTime, firstTime, samples, nil
}

func calculateTimeOffsets(procfs proc.FileSystem) error {
	ncpu := procfs.HostFileSystem().NumCPU()
	TimeOffsets = make([]int64, ncpu)
	if HaveClockID {
		return nil
	}

	// Obtain references samples, one for each CPU.
	startTime, firstTime, samples, err := collectReferenceSamples(ncpu)
	if err != nil {
		return err
	}

	TimeBase = startTime
	for cpu, sample := range samples {
		TimeOffsets[cpu] = sample - (firstTime - startTime)
		glog.V(2).Infof("EventMonitor CPU %d time offset is %d\n",
			cpu, TimeOffsets[cpu])
	}

	return nil
}

func (c *defaultEventSourceController) NewEventSourceLeader(
	attr EventAttr,
	pid, cpu int,
	flags uintptr,
) (EventSourceLeader, error) {
	var err error
	s := &defaultEventSourceLeader{
		pid:        pid,
		cpu:        cpu,
		flags:      flags,
		controller: c,
	}

	if HaveClockID {
		attr.UseClockID = true
		attr.ClockID = unix.CLOCK_MONOTONIC_RAW
	} else {
		attr.UseClockID = false
		attr.ClockID = 0
	}
	if s.fd, err = perfEventOpen(attr, pid, cpu, -1, flags); err != nil {
		return nil, err
	}

	if s.streamID, err = unix.IoctlGetInt(s.fd, PERF_EVENT_IOC_ID); err != nil {
		s.Close()
		return nil, err
	}

	if err = s.rb.init(s.fd, c.ringBufferNumPages); err != nil {
		s.Close()
		return nil, err
	}

	event := unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(s.streamID & 0xffffffff),
		Pad:    int32((s.streamID >> 32) & 0xffffffff),
	}
	if err = unix.EpollCtl(c.epollFD, unix.EPOLL_CTL_ADD, s.fd, &event); err != nil {
		s.Close()
		return nil, err
	}

	atomic.AddInt64(&c.leaderCount, 1)
	return s, nil
}

func (c *defaultEventSourceController) Close() {
	if c.eventFD != -1 {
		unix.Close(c.eventFD)
		c.eventFD = -1
	}
	if c.epollFD != -1 {
		unix.Close(c.epollFD)
		c.epollFD = -1
	}
}

func (c *defaultEventSourceController) Wait() ([]uint64, error) {
	leaderCount := int(atomic.LoadInt64(&c.leaderCount))

	if cap(c.events) < leaderCount+1 {
		size := roundPow2(leaderCount + 1)
		c.events = make([]unix.EpollEvent, 0, size*2)
	}
	c.events = c.events[:leaderCount+1]

	if cap(c.pending) < leaderCount {
		size := roundPow2(leaderCount)
		c.pending = make([]uint64, 0, size*2)
	}
	c.pending = c.pending[:0]

	for {
		n, err := unix.EpollWait(c.epollFD, c.events, -1)
		if err != nil {
			// Yes, this can actually happen. From what I've read
			// about the handling of EINTR in Go it seems like it
			// shouldn't be necessary, but when I tried taking it
			// out, I got a crash due to EINTR here, so in it stays
			if err == unix.EINTR {
				continue
			}
			return nil, err
		}

		for i := 0; i < n; i++ {
			e := c.events[i]
			if e.Fd == 0 && e.Pad == 0 {
				if e.Events & ^uint32(unix.EPOLLIN) != 0 {
					return nil, unix.ECANCELED
				}
				if e.Events&unix.EPOLLIN != unix.EPOLLIN {
					continue
				}
				// Read the value of the eventfd and reset to 0
				var b [8]byte
				unix.Read(c.eventFD, b[:])
				return nil, nil
			} else if e.Events&unix.EPOLLIN != 0 {
				cookie := (uint64(e.Pad) << 32) | uint64(e.Fd)
				c.pending = append(c.pending, cookie)
			}
		}

		if len(c.pending) > 0 {
			return c.pending, nil
		}
	}
}

func (c *defaultEventSourceController) Wakeup() {
	// Increment the eventfd counter by 1
	b := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	unix.Write(c.eventFD, b)
}

type ringBuffer struct {
	fd       int
	memory   []byte
	metadata *metadata
	data     []byte
}

func (rb *ringBuffer) init(fd int, pageCount int) error {
	if pageCount <= 0 {
		glog.Fatalf("ringBuffer.init(pageCount=%d)!", pageCount)
	}
	if rb.memory != nil {
		return unix.EALREADY
	}

	pageSize := os.Getpagesize()
	memory, err := unix.Mmap(fd, 0, (pageCount+1)*pageSize,
		unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return err
	}

	rb.fd = fd
	rb.memory = memory
	rb.metadata = (*metadata)(unsafe.Pointer(&memory[0]))
	rb.data = memory[pageSize:]

	for {
		seq := atomic.LoadUint32(&rb.metadata.Lock)
		if seq%2 != 0 {
			// seqlock must be even before value is read
			continue
		}

		version := atomic.LoadUint32(&rb.metadata.Version)
		compatVersion := atomic.LoadUint32(&rb.metadata.CompatVersion)

		if atomic.LoadUint32(&rb.metadata.Lock) != seq {
			// seqlock must be even and the same after values have been read
			continue
		}

		if version != 0 || compatVersion != 0 {
			rb.unmap()
			return errors.New("Incompatible ring buffer memory layout version")
		}

		break
	}

	return nil
}

func (rb *ringBuffer) unmap() error {
	if rb.memory != nil {
		if err := unix.Munmap(rb.memory); err != nil {
			return err
		}
		rb.memory = nil
		rb.metadata = nil
		rb.data = nil
	}
	return nil
}

func (rb *ringBuffer) read(acquireBuffer func(size int) ([]byte, int)) {
	dataTail := rb.metadata.DataTail
	dataHead := atomic.LoadUint64(&rb.metadata.DataHead)
	if dataHead <= dataTail {
		// It is not unusual for dataHead == dataTail, even though we
		// should only be getting here if the kernel has signaled that
		// the mapped fd is ready for reading. The reason is due to
		// timing ... the kernel could flag the fd after we've already
		// read the ring buffer on the previous pass, so we get a
		// spurious wakeup. On the other hand, if dataHead < dataTail,
		// something has gone seriously wrong somewhere. Either way,
		// ignore the problem and hope that the kernel corrects it.
		return
	}

	dataBegin := int(dataTail % uint64(len(rb.data)))
	dataEnd := int(dataHead % uint64(len(rb.data)))
	if dataEnd >= dataBegin {
		data, offset := acquireBuffer(dataEnd - dataBegin)
		copy(data[offset:], rb.data[dataBegin:dataEnd])
	} else {
		x := len(rb.data) - dataBegin
		data, offset := acquireBuffer(dataEnd + x)
		copy(data[offset:], rb.data[dataBegin:])
		copy(data[offset+x:], rb.data[:dataEnd])
	}

	// Write dataHead to dataTail to let kernel know that we've
	// consumed the data up to it.
	dataTail = dataHead
	atomic.StoreUint64(&rb.metadata.DataTail, dataTail)
}
