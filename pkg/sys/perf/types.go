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

package perf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"runtime"
	"time"
	"unsafe"

	"github.com/Happyholic1203/capsule8/pkg/expression"
)

func checkRawDataSize(rawData []byte, want int) error {
	if len(rawData) < want {
		_, _, line, _ := runtime.Caller(1)
		return fmt.Errorf("Expected %d bytes at line %d; got %d",
			want, line, len(rawData))
	}
	return nil
}

/*
   struct perf_event_attr {
       __u32 type;         // Type of event
       __u32 size;         // Size of attribute structure
       __u64 config;       // Type-specific configuration

       union {
           __u64 sample_period;    // Period of sampling
           __u64 sample_freq;      // Frequency of sampling
       };

       __u64 sample_type;  // Specifies values included in sample
       __u64 read_format;  // Specifies values returned in read

       __u64 disabled       : 1,   // off by default
             inherit        : 1,   // children inherit it
             pinned         : 1,   // must always be on PMU
             exclusive      : 1,   // only group on PMU
             exclude_user   : 1,   // don't count user
             exclude_kernel : 1,   // don't count kernel
             exclude_hv     : 1,   // don't count hypervisor
             exclude_idle   : 1,   // don't count when idle
             mmap           : 1,   // include mmap data
             comm           : 1,   // include comm data
             freq           : 1,   // use freq, not period
             inherit_stat   : 1,   // per task counts
             enable_on_exec : 1,   // next exec enables
             task           : 1,   // trace fork/exit
             watermark      : 1,   // wakeup_watermark
             precise_ip     : 2,   // skid constraint
             mmap_data      : 1,   // non-exec mmap data
             sample_id_all  : 1,   // sample_type all events
             exclude_host   : 1,   // don't count in host
             exclude_guest  : 1,   // don't count in guest
             exclude_callchain_kernel : 1,
                                   // exclude kernel callchains
             exclude_callchain_user   : 1,
                                   // exclude user callchains
             mmap2          :  1,  // include mmap with inode data
             comm_exec      :  1,  // flag comm events that are due to exec
             use_clockid    :  1,  // use clockid for time fields
             context_switch :  1,  // context switch data

             __reserved_1   : 37;

       union {
           __u32 wakeup_events;    // wakeup every n events
           __u32 wakeup_watermark; // bytes before wakeup
       };

       __u32     bp_type;          // breakpoint type

       union {
           __u64 bp_addr;          // breakpoint address
           __u64 config1;          // extension of config
       };

       union {
           __u64 bp_len;           // breakpoint length
           __u64 config2;          // extension of config1
       };
       __u64 branch_sample_type;   // enum perf_branch_sample_type
       __u64 sample_regs_user;     // user regs to dump on samples
       __u32 sample_stack_user;    // size of stack to dump on samples
       __s32 clockid;              // clock to use for time fields
       __u64 sample_regs_intr;     // regs to dump on samples
       __u32 aux_watermark;        // aux bytes before wakeup
       __u16 sample_max_stack;     // max frames in callchain
       __u16 __reserved_2;         // align to u64
   };
*/

// EventAttr is a translation of the Linux kernel's struct perf_event_attr
// into Go. It provides detailed configuration information for the event
// being created.
type EventAttr struct {
	Type                   uint32
	Size                   uint32
	Config                 uint64
	SamplePeriod           uint64
	SampleFreq             uint64
	SampleType             uint64
	ReadFormat             uint64
	Disabled               bool
	Inherit                bool
	Pinned                 bool
	Exclusive              bool
	ExcludeUser            bool
	ExcludeKernel          bool
	ExcludeHV              bool
	ExcludeIdle            bool
	Mmap                   bool
	Comm                   bool
	Freq                   bool
	InheritStat            bool
	EnableOnExec           bool
	Task                   bool
	Watermark              bool
	PreciseIP              uint8
	MmapData               bool
	SampleIDAll            bool
	ExcludeHost            bool
	ExcludeGuest           bool
	ExcludeCallchainKernel bool
	ExcludeCallchainUser   bool
	Mmap2                  bool
	CommExec               bool
	UseClockID             bool
	ContextSwitch          bool
	WakeupEvents           uint32
	WakeupWatermark        uint32
	BPType                 uint32
	BPAddr                 uint64
	Config1                uint64
	BPLen                  uint64
	Config2                uint64
	BranchSampleType       uint64
	SampleRegsUser         uint64
	SampleStackUser        uint32
	ClockID                int32
	SampleRegsIntr         uint64
	AuxWatermark           uint32
	SampleMaxStack         uint16

	// sizeofSampleID is a cached size of SampleID data expected in each
	// sample produced by the kernel
	sizeofSampleID int

	// sizeofSampleRecord is a cached size of sample data expected in each
	// sample produced by the kernel. This includes the size of SampleID
	// data because that data is not rendered as a SampleID struct the way
	// it is for other record types. This does not include variable length
	// data (e.g., IPs, RawData, V.CounterGroup.Values, etc.)
	sizeofSampleRecord int
}

func (ea *EventAttr) computeSizes() {
	// Compute sizeofSampleID
	ea.sizeofSampleID = 0
	if ea.SampleType&PERF_SAMPLE_TID != 0 {
		ea.sizeofSampleID += 8
	}
	if ea.SampleType&PERF_SAMPLE_TIME != 0 {
		ea.sizeofSampleID += 8
	}
	if ea.SampleType&PERF_SAMPLE_ID != 0 {
		ea.sizeofSampleID += 8
	}
	if ea.SampleType&PERF_SAMPLE_STREAM_ID != 0 {
		ea.sizeofSampleID += 8
	}
	if ea.SampleType&PERF_SAMPLE_CPU != 0 {
		ea.sizeofSampleID += 8
	}
	if ea.SampleType&PERF_SAMPLE_IDENTIFIER != 0 {
		ea.sizeofSampleID += 8
	}

	// Compute sizeofSampleRecord
	ea.sizeofSampleRecord = 0
	if ea.SampleType&PERF_SAMPLE_IDENTIFIER != 0 {
		ea.sizeofSampleRecord += 8
	}
	if ea.SampleType&PERF_SAMPLE_IP != 0 {
		ea.sizeofSampleRecord += 8
	}
	if ea.SampleType&PERF_SAMPLE_TID != 0 {
		ea.sizeofSampleRecord += 8
	}
	if ea.SampleType&PERF_SAMPLE_TIME != 0 {
		ea.sizeofSampleRecord += 8
	}
	if ea.SampleType&PERF_SAMPLE_ADDR != 0 {
		ea.sizeofSampleRecord += 8
	}
	if ea.SampleType&PERF_SAMPLE_ID != 0 {
		ea.sizeofSampleRecord += 8
	}
	if ea.SampleType&PERF_SAMPLE_STREAM_ID != 0 {
		ea.sizeofSampleRecord += 8
	}
	if ea.SampleType&PERF_SAMPLE_CPU != 0 {
		ea.sizeofSampleRecord += 8
	}
	if ea.SampleType&PERF_SAMPLE_PERIOD != 0 {
		ea.sizeofSampleRecord += 8
	}
	if ea.SampleType&PERF_SAMPLE_READ != 0 {
		if ea.ReadFormat&PERF_FORMAT_GROUP != 0 {
			ea.sizeofSampleRecord += 8
			if ea.ReadFormat&PERF_FORMAT_TOTAL_TIME_ENABLED != 0 {
				ea.sizeofSampleRecord += 8
			}
			if ea.ReadFormat&PERF_FORMAT_TOTAL_TIME_RUNNING != 0 {
				ea.sizeofSampleRecord += 8
			}
		} else {
			ea.sizeofSampleRecord += 8
			if ea.ReadFormat&PERF_FORMAT_TOTAL_TIME_ENABLED != 0 {
				ea.sizeofSampleRecord += 8
			}
			if ea.ReadFormat&PERF_FORMAT_TOTAL_TIME_RUNNING != 0 {
				ea.sizeofSampleRecord += 8
			}
			if ea.ReadFormat&PERF_FORMAT_ID != 0 {
				ea.sizeofSampleRecord += 8
			}
		}
	}
	if ea.SampleType&PERF_SAMPLE_CALLCHAIN != 0 {
		// Fixed 8 bytes + variable
		ea.sizeofSampleRecord += 8
	}
	if ea.SampleType&PERF_SAMPLE_RAW != 0 {
		// Fixed 4 bytes + variable
		ea.sizeofSampleRecord += 4
	}
	if ea.SampleType&PERF_SAMPLE_BRANCH_STACK != 0 {
		// Fixed 8 bytes + variable
		ea.sizeofSampleRecord += 8
	}

	// PERF_SAMPLE_REGS_USER
	// PERF_SAMPLE_STACK_USER
	// PERF_SAMPLE_WEIGHT
	// PERF_SAMPLE_DATA_SRC
	// PERF_SAMPLE_TRANSACTION
	// PERF_SAMPLE_REGS_INTR
}

type eventAttrBitfield uint64

func (bf *eventAttrBitfield) setBit(b bool, bit uint64) {
	if b {
		*bf |= eventAttrBitfield(bit)
	}
}

// write serializes the EventAttr as a perf_event_attr struct compatible
// with the kernel.
func (ea *EventAttr) write(buf io.Writer) error {
	// Automatically figure out ea.Size; ignore whatever is passed in.
	switch {
	case ea.AuxWatermark > 0 || ea.SampleMaxStack > 0:
		ea.Size = sizeofPerfEventAttrVer5
	case ea.SampleType&PERF_SAMPLE_REGS_INTR != 0:
		ea.Size = sizeofPerfEventAttrVer4
	case ea.UseClockID || ea.SampleType&(PERF_SAMPLE_REGS_USER|PERF_SAMPLE_STACK_USER) != 0:
		ea.Size = sizeofPerfEventAttrVer3
	case ea.SampleType&PERF_SAMPLE_BRANCH_STACK != 0:
		ea.Size = sizeofPerfEventAttrVer2
	case ea.Type == PERF_TYPE_BREAKPOINT || ea.Config2 != 0:
		ea.Size = sizeofPerfEventAttrVer1
	default:
		ea.Size = sizeofPerfEventAttrVer0
	}

	binary.Write(buf, binary.LittleEndian, ea.Type)
	binary.Write(buf, binary.LittleEndian, ea.Size)
	binary.Write(buf, binary.LittleEndian, ea.Config)

	if (ea.Freq && ea.SamplePeriod != 0) ||
		(!ea.Freq && ea.SampleFreq != 0) {
		return errors.New("Encoding error: invalid SamplePeriod/SampleFreq union")
	}

	if ea.Freq {
		binary.Write(buf, binary.LittleEndian, ea.SampleFreq)
	} else {
		binary.Write(buf, binary.LittleEndian, ea.SamplePeriod)
	}

	binary.Write(buf, binary.LittleEndian, ea.SampleType)
	binary.Write(buf, binary.LittleEndian, ea.ReadFormat)

	if ea.PreciseIP > 3 {
		return errors.New("Encoding error: PreciseIP must be < 4")
	}

	var bitfield eventAttrBitfield
	bitfield.setBit(ea.Disabled, eaDisabled)
	bitfield.setBit(ea.Inherit, eaInherit)
	bitfield.setBit(ea.Pinned, eaPinned)
	bitfield.setBit(ea.Exclusive, eaExclusive)
	bitfield.setBit(ea.ExcludeUser, eaExcludeUser)
	bitfield.setBit(ea.ExcludeKernel, eaExcludeKernel)
	bitfield.setBit(ea.ExcludeHV, eaExcludeHV)
	bitfield.setBit(ea.ExcludeIdle, eaExcludeIdle)
	bitfield.setBit(ea.Mmap, eaMmap)
	bitfield.setBit(ea.Comm, eaComm)
	bitfield.setBit(ea.Freq, eaFreq)
	bitfield.setBit(ea.InheritStat, eaInheritStat)
	bitfield.setBit(ea.EnableOnExec, eaEnableOnExec)
	bitfield.setBit(ea.Task, eaTask)
	bitfield.setBit(ea.Watermark, eaWatermark)
	bitfield.setBit(ea.PreciseIP&0x1 == 0x1, eaPreciseIP1)
	bitfield.setBit(ea.PreciseIP&0x2 == 0x2, eaPreciseIP2)
	bitfield.setBit(ea.MmapData, eaMmapData)
	bitfield.setBit(ea.SampleIDAll, eaSampleIDAll)
	bitfield.setBit(ea.ExcludeHost, eaExcludeHost)
	bitfield.setBit(ea.ExcludeGuest, eaExcludeGuest)
	bitfield.setBit(ea.ExcludeCallchainKernel, eaExcludeCallchainKernel)
	bitfield.setBit(ea.ExcludeCallchainUser, eaExcludeCallchainUser)
	bitfield.setBit(ea.Mmap2, eaMmap2)
	bitfield.setBit(ea.CommExec, eaCommExec)
	bitfield.setBit(ea.UseClockID, eaUseClockID)
	bitfield.setBit(ea.ContextSwitch, eaContextSwitch)
	binary.Write(buf, binary.LittleEndian, uint64(bitfield))

	if (ea.Watermark && ea.WakeupEvents != 0) ||
		(!ea.Watermark && ea.WakeupWatermark != 0) {
		return errors.New("Encoding error: invalid WakeupWatermark/WakeupEvents union")
	}

	if ea.Watermark {
		binary.Write(buf, binary.LittleEndian, ea.WakeupWatermark)
	} else {
		binary.Write(buf, binary.LittleEndian, ea.WakeupEvents)
	}

	binary.Write(buf, binary.LittleEndian, ea.BPType)

	switch ea.Type {
	case PERF_TYPE_BREAKPOINT:
		if ea.Config1 != 0 || ea.Config2 != 0 {
			return errors.New("Cannot set Config1/Config2 for type == PERF_TYPE_BREAKPOINT")
		}
		binary.Write(buf, binary.LittleEndian, ea.BPAddr)
		binary.Write(buf, binary.LittleEndian, ea.BPLen)
	default:
		if ea.BPAddr != 0 || ea.BPLen != 0 {
			return errors.New("Cannot set BPAddr/BPLen for type != PERF_TYPE_BREAKPOINT")
		}
		binary.Write(buf, binary.LittleEndian, ea.Config1)
		binary.Write(buf, binary.LittleEndian, ea.Config2)
	}

	binary.Write(buf, binary.LittleEndian, ea.BranchSampleType)
	binary.Write(buf, binary.LittleEndian, ea.SampleRegsUser)
	binary.Write(buf, binary.LittleEndian, ea.SampleStackUser)
	binary.Write(buf, binary.LittleEndian, ea.ClockID)
	binary.Write(buf, binary.LittleEndian, ea.SampleRegsIntr)
	binary.Write(buf, binary.LittleEndian, ea.AuxWatermark)
	binary.Write(buf, binary.LittleEndian, ea.SampleMaxStack)

	binary.Write(buf, binary.LittleEndian, uint16(0))

	ea.computeSizes()
	return nil
}

/*
   struct perf_event_mmap_page {
       __u32 version;        // version number of this structure
       __u32 compat_version; // lowest version this is compat with
       __u32 lock;           // seqlock for synchronization
       __u32 index;          // hardware counter identifier
       __s64 offset;         // add to hardware counter value
       __u64 time_enabled;   // time event active
       __u64 time_running;   // time event on CPU
       union {
           __u64   capabilities;
           struct {
               __u64 cap_usr_time / cap_usr_rdpmc / cap_bit0 : 1,
                     cap_bit0_is_deprecated : 1,
                     cap_user_rdpmc         : 1,
                     cap_user_time          : 1,
                     cap_user_time_zero     : 1,
           };
       };
       __u16 pmc_width;
       __u16 time_shift;
       __u32 time_mult;
       __u64 time_offset;
       __u64 __reserved[120];   // Pad to 1k
       __u64 data_head;         // head in the data section
       __u64 data_tail;         // user-space written tail
       __u64 data_offset;       // where the buffer starts
       __u64 data_size;         // data buffer size
       __u64 aux_head;
       __u64 aux_tail;
       __u64 aux_offset;
       __u64 aux_size;
   }
*/

type metadata struct {
	Version       uint32
	CompatVersion uint32
	Lock          uint32
	Index         uint32
	Offset        int64
	TimeEnabled   uint64
	TimeRunning   uint64
	Capabilities  uint64
	PMCWidth      uint16
	TimeWidth     uint16
	TimeMult      uint32
	TimeOffset    uint64
	_             [120]uint64
	DataHead      uint64
	DataTail      uint64
	DataOffset    uint64
	DataSize      uint64
	AuxHead       uint64
	AuxTail       uint64
	AuxOffset     uint64
	AuxSize       uint64
}

// -----------------------------------------------------------------------------

// CounterValue resepresents the read value of a counter event
type CounterValue struct {
	// Globally unique identifier for this counter event. Only
	// present if PERF_FORMAT_ID was specified.
	ID uint64

	// The counter result
	Value uint64
}

// CounterGroup represents the read values of a group of counter events
type CounterGroup struct {
	TimeEnabled uint64
	TimeRunning uint64
	Values      []CounterValue
}

func (cg *CounterGroup) read(rawData []byte, format uint64) (off int, err error) {
	// The fixed length portion of the data should have already been
	// checked before this is called. The variable length portion will be
	// checked here.

	if format&PERF_FORMAT_GROUP != 0 {
		nr := *(*uint64)(unsafe.Pointer(&rawData[0]))
		off += 8
		if format&PERF_FORMAT_TOTAL_TIME_ENABLED != 0 {
			cg.TimeEnabled = *(*uint64)(unsafe.Pointer(&rawData[off]))
			off += 8
		}
		if format&PERF_FORMAT_TOTAL_TIME_RUNNING != 0 {
			cg.TimeRunning = *(*uint64)(unsafe.Pointer(&rawData[off]))
			off += 8
		}

		cg.Values = make([]CounterValue, nr)
		if format&PERF_FORMAT_ID != 0 {
			if err = checkRawDataSize(rawData, off+(int(nr)*16)); err != nil {
				return
			}
			for i := uint64(0); i < nr; i++ {
				cg.Values[i].Value = *(*uint64)(unsafe.Pointer(&(rawData[off])))
				cg.Values[i].ID = *(*uint64)(unsafe.Pointer(&rawData[off+8]))
				off += 16
			}
		} else {
			if err = checkRawDataSize(rawData, off+(int(nr)*8)); err != nil {
				return
			}
			for i := uint64(0); i < nr; i++ {
				cg.Values[i].Value = *(*uint64)(unsafe.Pointer(&rawData[off]))
				off += 8
			}
		}
	} else {
		cg.Values = []CounterValue{
			CounterValue{
				Value: *(*uint64)(unsafe.Pointer(&rawData[0])),
			},
		}
		off += 8
		if format&PERF_FORMAT_TOTAL_TIME_ENABLED != 0 {
			cg.TimeEnabled = *(*uint64)(unsafe.Pointer(&rawData[off]))
			off += 8
		}
		if format&PERF_FORMAT_TOTAL_TIME_RUNNING != 0 {
			cg.TimeRunning = *(*uint64)(unsafe.Pointer(&rawData[off]))
			off += 8
		}
		if format&PERF_FORMAT_ID != 0 {
			cg.Values[0].ID = *(*uint64)(unsafe.Pointer(&rawData[off]))
			off += 8
		}
	}
	return
}

// BranchEntry is a translation of the Linux kernel's struct perf_branch_entry
// into Go. It may appear in SampleRecord if PERF_SAMPLE_BRANCH_STACK is set.
type BranchEntry struct {
	From      uint64
	To        uint64
	Mispred   bool
	Predicted bool
	InTx      bool
	Abort     bool
	Cycles    uint16
}

// Sample is the representation of a perf_event sample retrieved from the
// Linux kernel. It includes the header information, a translation of the
// sample data, and metadata depending on the flags set in the EventAttr
// used to enable the event that generated the sample.
type Sample struct {
	Type uint32
	Misc uint16

	SampleID

	// If Type == PERF_RECORD_LOST, this will be the count of lost records
	Lost uint64

	// If Type == PERF_RECORD_SAMPLE, the rest of this will be filled in
	// accordingly
	IP          uint64
	Addr        uint64
	Period      uint64
	V           CounterGroup
	IPs         []uint64
	RawData     []byte
	Branches    []BranchEntry
	UserABI     uint64
	UserRegs    []uint64
	StackData   []uint64
	Weight      uint64
	DataSrc     uint64
	Transaction uint64
	IntrABI     uint64
	IntrRegs    []uint64

	// TraceFormat is primarily used internally to decode RawData on-demand
	// It is exposed publicly to facilitate unit testing.
	TraceFormat TraceEventFormat
}

func (sample *Sample) readSampleRecord(rawData []byte, eventAttr *EventAttr) (err error) {
	// The fixed length portion of the data should have already been
	// checked before this is called. The variable length portion will be
	// checked here.

	var off int
	sampleType := eventAttr.SampleType

	if eventAttr.SampleType&PERF_SAMPLE_IDENTIFIER != 0 {
		sampleType &= ^PERF_SAMPLE_IDENTIFIER
		sample.ID = *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
	}

	if sampleType&PERF_SAMPLE_IP != 0 {
		sampleType &= ^PERF_SAMPLE_IP
		sample.IP = *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
	}

	if sampleType&PERF_SAMPLE_TID != 0 {
		sampleType &= ^PERF_SAMPLE_TID
		sample.PID = *(*uint32)(unsafe.Pointer(&rawData[off]))
		sample.TID = *(*uint32)(unsafe.Pointer(&rawData[off+4]))
		off += 8
	}

	if sampleType&PERF_SAMPLE_TIME != 0 {
		sampleType &= ^PERF_SAMPLE_TIME
		sample.Time = *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
	}

	if sampleType&PERF_SAMPLE_ADDR != 0 {
		sampleType &= ^PERF_SAMPLE_ADDR
		sample.Addr = *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
	}

	if sampleType&PERF_SAMPLE_ID != 0 {
		sampleType &= ^PERF_SAMPLE_ID
		sample.ID = *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
	}

	if sampleType&PERF_SAMPLE_STREAM_ID != 0 {
		sampleType &= ^PERF_SAMPLE_STREAM_ID
		sample.StreamID = *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
	}

	if sampleType&PERF_SAMPLE_CPU != 0 {
		sampleType &= ^PERF_SAMPLE_CPU
		sample.CPU = *(*uint32)(unsafe.Pointer(&rawData[off]))
		// _ = *(*uint32)(unsafe.Pointer(&rawData[off+4]))
		off += 8
	}

	if sampleType&PERF_SAMPLE_PERIOD != 0 {
		sampleType &= ^PERF_SAMPLE_PERIOD
		sample.Period = *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
	}

	if sampleType&PERF_SAMPLE_READ != 0 {
		sampleType &= ^PERF_SAMPLE_READ
		var o int
		o, err = sample.V.read(rawData[off:], eventAttr.ReadFormat)
		if err != nil {
			return
		}
		off += o
	}

	if sampleType&PERF_SAMPLE_CALLCHAIN != 0 {
		sampleType &= ^PERF_SAMPLE_CALLCHAIN
		nr := *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
		if err = checkRawDataSize(rawData, off+(int(nr)*8)); err != nil {
			return
		}
		sample.IPs = make([]uint64, nr)
		for i := uint64(0); i < nr; i++ {
			sample.IPs[i] = *(*uint64)(unsafe.Pointer(&rawData[off]))
			off += 8
		}
	}

	if sampleType&PERF_SAMPLE_RAW != 0 {
		sampleType &= ^PERF_SAMPLE_RAW
		rawDataSize := int(*(*uint32)(unsafe.Pointer(&rawData[off])))
		off += 4
		if err = checkRawDataSize(rawData, off+rawDataSize); err != nil {
			return
		}
		sample.RawData = rawData[off : off+rawDataSize]
		off += int(rawDataSize)
	}

	if sampleType&PERF_SAMPLE_BRANCH_STACK != 0 {
		sampleType &= ^PERF_SAMPLE_BRANCH_STACK
		nr := *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
		if err = checkRawDataSize(rawData, off+(int(nr)*24)); err != nil {
			return
		}
		sample.Branches = make([]BranchEntry, nr)
		for i := uint64(0); i < nr; i++ {
			flags := *(*uint64)(unsafe.Pointer(&rawData[off+16]))
			sample.Branches[i] = BranchEntry{
				From:      *(*uint64)(unsafe.Pointer(&rawData[off])),
				To:        *(*uint64)(unsafe.Pointer(&rawData[off+8])),
				Mispred:   (flags&(1<<0) != 0),
				Predicted: (flags&(1<<1) != 0),
				InTx:      (flags&(1<<2) != 0),
				Abort:     (flags&(1<<3) != 0),
				Cycles:    uint16((flags & 0xffff0) >> 4),
			}
			off += 24
		}
	}

	// PERF_SAMPLE_REGS_USER
	// PERF_SAMPLE_STACK_USER
	// PERF_SAMPLE_WEIGHT - uint64
	// PERF_SAMPLE_DATA_SRC - uint64
	// PERF_SAMPLE_TRANSACTION - uint64
	// PERF_SAMPLE_REGS_INTR

	if sampleType != 0 {
		err = fmt.Errorf("EventAttr.SampleType has unsupported bits %x set", sampleType)
	}
	return
}

/*
   struct sample_id {
       { u32 pid, tid; } // if PERF_SAMPLE_TID set
       { u64 time;     } // if PERF_SAMPLE_TIME set
       { u64 id;       } // if PERF_SAMPLE_ID set
       { u64 stream_id;} // if PERF_SAMPLE_STREAM_ID set
       { u32 cpu, res; } // if PERF_SAMPLE_CPU set
       { u64 id;       } // if PERF_SAMPLE_IDENTIFIER set
   };
*/

// SampleID is a translation of the structure used by the Linux kernel for all
// samples when SampleIDAll is set in the EventAttr used for a sample.
type SampleID struct {
	PID      uint32
	TID      uint32
	Time     uint64
	ID       uint64
	StreamID uint64
	CPU      uint32
}

func (sid *SampleID) read(rawData []byte, attr *EventAttr) {
	// Availability of the data to read should be checked before this is
	// called

	var off int
	eventSampleType := attr.SampleType
	if (eventSampleType & PERF_SAMPLE_TID) != 0 {
		sid.PID = *(*uint32)(unsafe.Pointer(&rawData[off]))
		sid.TID = *(*uint32)(unsafe.Pointer(&rawData[off+4]))
		off += 8
	}
	if (eventSampleType & PERF_SAMPLE_TIME) != 0 {
		sid.Time = *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
	}
	if (eventSampleType & PERF_SAMPLE_ID) != 0 {
		sid.ID = *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
	}
	if (eventSampleType & PERF_SAMPLE_STREAM_ID) != 0 {
		sid.StreamID = *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
	}
	if (eventSampleType & PERF_SAMPLE_CPU) != 0 {
		sid.CPU = *(*uint32)(unsafe.Pointer(&rawData[off]))
		// _ = *(*uint32)(unsafe.Pointer(&rawData[off+4]))
		off += 8
	}
	if (eventSampleType & PERF_SAMPLE_IDENTIFIER) != 0 {
		sid.ID = *(*uint64)(unsafe.Pointer(&rawData[off]))
		off += 8
	}
}

func (sample *Sample) resolveEventAttr(
	rawData []byte,
	formatMap map[uint64]*EventAttr,
) (attr *EventAttr, err error) {
	// Assumptions: All EventAttr structures in use must have following
	// set for any of this code to function properly:
	//
	//      SampleType |= PERF_SAMPLE_IDENTIFIER
	//      SampleIDAll = true
	//
	// If we're finding the eventAttr from formatMap, the sample ID will be
	// in the record where it's needed. For PERF_RECORD_SAMPLE, the ID will
	// be the first thing in the data. For everything else, the ID will be
	// the last thing in the data.

	if err = checkRawDataSize(rawData, 8); err != nil {
		return
	}

	var sampleID uint64
	if sample.Type == PERF_RECORD_SAMPLE {
		// The SampleID will be immediately following the event header
		sampleID = *(*uint64)(unsafe.Pointer(&rawData[0]))
	} else {
		// The SampleID will be the last uint64 in the sample
		sampleID = *(*uint64)(unsafe.Pointer(&rawData[len(rawData)-8]))
	}

	attr, ok := formatMap[sampleID]
	if !ok {
		err = fmt.Errorf("Unknown SampleID %d from raw sample", sampleID)
	}
	return
}

const (
	sizeofEventHeader = 8
	sizeofLostRecord  = 16
)

func (sample *Sample) read(
	rawData []byte,
	eventAttr *EventAttr,
	formatMap map[uint64]*EventAttr,
) (n int, err error) {
	if err = checkRawDataSize(rawData, sizeofEventHeader); err != nil {
		n = len(rawData)
		return
	}
	sample.Type = *(*uint32)(unsafe.Pointer(&rawData[0]))
	sample.Misc = *(*uint16)(unsafe.Pointer(&rawData[4]))
	n = int(*(*uint16)(unsafe.Pointer(&rawData[6])))

	if err = checkRawDataSize(rawData, n); err != nil {
		n = len(rawData)
		return
	}
	if n == sizeofEventHeader {
		return
	}
	rawData = rawData[sizeofEventHeader:n]

	if eventAttr == nil {
		eventAttr, err = sample.resolveEventAttr(rawData, formatMap)
		if err != nil {
			return
		}
	}

	// For all sample types that are not PERF_RECORD_SAMPLE the sample ID
	// will be at the end of the sample buffer
	if sample.Type != PERF_RECORD_SAMPLE {
		s := eventAttr.sizeofSampleID
		if err = checkRawDataSize(rawData, s); err != nil {
			return
		}
		sample.SampleID.read(rawData[len(rawData)-s:], eventAttr)
		rawData = rawData[:len(rawData)-s]
	}

	switch sample.Type {
	case PERF_RECORD_LOST:
		err = checkRawDataSize(rawData, sizeofLostRecord)
		if err == nil {
			sample.Lost = *(*uint64)(unsafe.Pointer(&rawData[8]))
		}

	case PERF_RECORD_SAMPLE:
		err = checkRawDataSize(rawData, eventAttr.sizeofSampleRecord)
		if err == nil {
			err = sample.readSampleRecord(rawData, eventAttr)
		}
	}

	return
}

// DecodeRawData decodes a sample's raw data into a dictionary of field names
// and values.
func (sample *Sample) DecodeRawData() (expression.FieldValueMap, error) {
	return sample.TraceFormat.DecodeRawData(sample.RawData)
}

// DecodeValue decodes the specified field from a sample's raw data.
func (sample *Sample) DecodeValue(name string) (interface{}, error) {
	field, found := sample.TraceFormat[name]
	if !found {
		return nil, expression.FieldNotSet{Name: name}
	}
	return field.DecodeRawData(sample.RawData)
}

//
// Implement expression.FieldValueGetter interface
//

// GetString returns the string value set for the requested field name.
func (sample *Sample) GetString(name string) (string, error) {
	if field, set := sample.TraceFormat[name]; set {
		return field.DecodeString(sample.RawData)
	}
	return "", expression.FieldNotSet{Name: name}
}

// GetSignedInt8 returns the signed 8-bit integer value set for the requested
// field name.
func (sample *Sample) GetSignedInt8(name string) (int8, error) {
	if field, set := sample.TraceFormat[name]; set {
		return field.DecodeSignedInt8(sample.RawData)
	}
	return 0, expression.FieldNotSet{Name: name}
}

// GetSignedInt16 returns the signed 16-bit integer value set for the requested
// field name.
func (sample *Sample) GetSignedInt16(name string) (int16, error) {
	if field, set := sample.TraceFormat[name]; set {
		return field.DecodeSignedInt16(sample.RawData)
	}
	return 0, expression.FieldNotSet{Name: name}
}

// GetSignedInt32 returns the signed 32-bit integer value set for the requested
// field name.
func (sample *Sample) GetSignedInt32(name string) (int32, error) {
	if field, set := sample.TraceFormat[name]; set {
		return field.DecodeSignedInt32(sample.RawData)
	}
	return 0, expression.FieldNotSet{Name: name}
}

// GetSignedInt64 returns the signed 64-bit integer value set for the requested
// field name.
func (sample *Sample) GetSignedInt64(name string) (int64, error) {
	if field, set := sample.TraceFormat[name]; set {
		return field.DecodeSignedInt64(sample.RawData)
	}
	return 0, expression.FieldNotSet{Name: name}
}

// GetUnsignedInt8 returns the unsigned 8-bit integer value set for the
// requested field name.
func (sample *Sample) GetUnsignedInt8(name string) (uint8, error) {
	if field, set := sample.TraceFormat[name]; set {
		return field.DecodeUnsignedInt8(sample.RawData)
	}
	return 0, expression.FieldNotSet{Name: name}
}

// GetUnsignedInt16 returns the unsigned 16-bit integer value set for the
// requested field name.
func (sample *Sample) GetUnsignedInt16(name string) (uint16, error) {
	if field, set := sample.TraceFormat[name]; set {
		return field.DecodeUnsignedInt16(sample.RawData)
	}
	return 0, expression.FieldNotSet{Name: name}
}

// GetUnsignedInt32 returns the unsigned 32-bit integer value set for the
// requested field name.
func (sample *Sample) GetUnsignedInt32(name string) (uint32, error) {
	if field, set := sample.TraceFormat[name]; set {
		return field.DecodeUnsignedInt32(sample.RawData)
	}
	return 0, expression.FieldNotSet{Name: name}
}

// GetUnsignedInt64 returns the unsigned 64-bit integer value set for the
// requested field name.
func (sample *Sample) GetUnsignedInt64(name string) (uint64, error) {
	if field, set := sample.TraceFormat[name]; set {
		return field.DecodeUnsignedInt64(sample.RawData)
	}
	return 0, expression.FieldNotSet{Name: name}
}

// GetBool is present solely to conform to expression.FieldValueGetter. It will
// always return an error because the kernel does not support a Boolean type.
func (sample *Sample) GetBool(name string) (bool, error) {
	if field, set := sample.TraceFormat[name]; set {
		return false, field.typeMismatch(expression.ValueTypeBool)
	}
	return false, expression.FieldNotSet{Name: name}
}

// GetDouble is present solely to conform to expression.FieldValueGetter. It
// will always return an error because the kernel does not support a floating
// point type.
func (sample *Sample) GetDouble(name string) (float64, error) {
	if field, set := sample.TraceFormat[name]; set {
		return 0, field.typeMismatch(expression.ValueTypeDouble)
	}
	return 0, expression.FieldNotSet{Name: name}
}

// GetTimestamp is present solely to conform to expression.FieldValueGetter. It
// will always return an error because the kernel does not support a timestamp
// type.
func (sample *Sample) GetTimestamp(name string) (time.Time, error) {
	if field, set := sample.TraceFormat[name]; set {
		return time.Time{}, field.typeMismatch(expression.ValueTypeTimestamp)
	}
	return time.Time{}, expression.FieldNotSet{Name: name}
}
