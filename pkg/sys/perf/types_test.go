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
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/Happyholic1203/capsule8/pkg/expression"

	"github.com/stretchr/testify/assert"
)

func TestCheckRawDataSize(t *testing.T) {
	rawData := make([]byte, 8)

	// have 8, want 16 -> error
	err := checkRawDataSize(rawData, 16)
	assert.Error(t, err)

	// have 8, want 8 -> ok
	err = checkRawDataSize(rawData, 8)
	assert.NoError(t, err)

	// have 8, want 4 -> ok
	err = checkRawDataSize(rawData, 4)
	assert.NoError(t, err)
}

func TestComputeSizes(t *testing.T) {
	testCases := []struct {
		sampleType         uint64
		readFormat         uint64
		sizeofSampleID     int
		sizeofSampleRecord int
	}{
		{PERF_SAMPLE_TID, 0, 8, 8},
		{PERF_SAMPLE_TIME, 0, 8, 8},
		{PERF_SAMPLE_ID, 0, 8, 8},
		{PERF_SAMPLE_STREAM_ID, 0, 8, 8},
		{PERF_SAMPLE_CPU, 0, 8, 8},
		{PERF_SAMPLE_IDENTIFIER, 0, 8, 8},
		{PERF_SAMPLE_TID | PERF_SAMPLE_TIME, 0, 16, 16},
		{PERF_SAMPLE_ID | PERF_SAMPLE_CPU, 0, 16, 16},
		{PERF_SAMPLE_TID | PERF_SAMPLE_TIME | PERF_SAMPLE_ID, 0, 24, 24},
		{PERF_SAMPLE_IDENTIFIER | PERF_SAMPLE_TID | PERF_SAMPLE_STREAM_ID, 0, 24, 24},
		{PERF_SAMPLE_TID | PERF_SAMPLE_TIME | PERF_SAMPLE_ID | PERF_SAMPLE_CPU, 0, 32, 32},
		{PERF_SAMPLE_TID | PERF_SAMPLE_TIME | PERF_SAMPLE_ID |
			PERF_SAMPLE_STREAM_ID | PERF_SAMPLE_CPU |
			PERF_SAMPLE_IDENTIFIER, 0, 48, 48},

		{PERF_SAMPLE_IP, 0, 0, 8},
		{PERF_SAMPLE_ADDR, 0, 0, 8},
		{PERF_SAMPLE_PERIOD, 0, 0, 8},

		{PERF_SAMPLE_READ, 0, 0, 8},
		{PERF_SAMPLE_READ, PERF_FORMAT_ID, 0, 16},
		{PERF_SAMPLE_READ, PERF_FORMAT_TOTAL_TIME_ENABLED | PERF_FORMAT_TOTAL_TIME_RUNNING, 0, 24},
		{PERF_SAMPLE_READ, PERF_FORMAT_GROUP, 0, 8},
		{PERF_SAMPLE_READ, PERF_FORMAT_GROUP | PERF_FORMAT_TOTAL_TIME_ENABLED | PERF_FORMAT_TOTAL_TIME_RUNNING, 0, 24},

		{PERF_SAMPLE_CALLCHAIN, 0, 0, 8},
		{PERF_SAMPLE_RAW, 0, 0, 4},
		{PERF_SAMPLE_BRANCH_STACK, 0, 0, 8},
	}
	for _, tc := range testCases {
		ea := EventAttr{
			SampleType: tc.sampleType,
			ReadFormat: tc.readFormat,
		}
		ea.computeSizes()
		assert.Equal(t, tc.sizeofSampleID, ea.sizeofSampleID)
		assert.Equal(t, tc.sizeofSampleRecord, ea.sizeofSampleRecord)
	}
}

func (bf *eventAttrBitfield) testBit(bit uint64) bool {
	return *bf&eventAttrBitfield(bit) == eventAttrBitfield(bit)
}

type readError struct {
	error
}

func readOrPanic(buf io.Reader, i interface{}) {
	if err := binary.Read(buf, binary.LittleEndian, i); err != nil {
		panic(readError{err})
	}
}

func (ea *EventAttr) read(buf io.Reader) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(readError); ok {
				err = e.error
			} else {
				panic(r)
			}
		}
	}()

	*ea = EventAttr{} // zero everything out

	readOrPanic(buf, &ea.Type)

	readOrPanic(buf, &ea.Size)
	switch ea.Size {
	case sizeofPerfEventAttrVer0,
		sizeofPerfEventAttrVer1,
		sizeofPerfEventAttrVer2,
		sizeofPerfEventAttrVer3,
		sizeofPerfEventAttrVer4,
		sizeofPerfEventAttrVer5:
		// pass
	default:
		return fmt.Errorf("Illegal size %d while reading EventAttr", ea.Size)
	}

	readOrPanic(buf, &ea.Config)

	// Don't know which to use, SamplePeriod or SampleFreq until flags are
	// read after this. Read now and assign later.
	var samplePeriodOrFreq uint64
	readOrPanic(buf, &samplePeriodOrFreq)

	readOrPanic(buf, &ea.SampleType)
	readOrPanic(buf, &ea.ReadFormat)

	var uint64Value uint64
	readOrPanic(buf, &uint64Value)
	bitfield := eventAttrBitfield(uint64Value)
	ea.Disabled = bitfield.testBit(eaDisabled)
	ea.Inherit = bitfield.testBit(eaInherit)
	ea.Pinned = bitfield.testBit(eaPinned)
	ea.Exclusive = bitfield.testBit(eaExclusive)
	ea.ExcludeUser = bitfield.testBit(eaExcludeUser)
	ea.ExcludeKernel = bitfield.testBit(eaExcludeKernel)
	ea.ExcludeHV = bitfield.testBit(eaExcludeHV)
	ea.ExcludeIdle = bitfield.testBit(eaExcludeIdle)
	ea.Mmap = bitfield.testBit(eaMmap)
	ea.Comm = bitfield.testBit(eaComm)
	ea.Freq = bitfield.testBit(eaFreq)
	ea.InheritStat = bitfield.testBit(eaInheritStat)
	ea.EnableOnExec = bitfield.testBit(eaEnableOnExec)
	ea.Task = bitfield.testBit(eaTask)
	ea.Watermark = bitfield.testBit(eaWatermark)
	if bitfield.testBit(eaPreciseIP1) {
		ea.PreciseIP |= 0x1
	}
	if bitfield.testBit(eaPreciseIP2) {
		ea.PreciseIP |= 0x2
	}
	ea.MmapData = bitfield.testBit(eaMmapData)
	ea.SampleIDAll = bitfield.testBit(eaSampleIDAll)
	ea.ExcludeHost = bitfield.testBit(eaExcludeHost)
	ea.ExcludeGuest = bitfield.testBit(eaExcludeGuest)
	ea.ExcludeCallchainKernel = bitfield.testBit(eaExcludeCallchainKernel)
	ea.ExcludeCallchainUser = bitfield.testBit(eaExcludeCallchainUser)
	ea.Mmap2 = bitfield.testBit(eaMmap2)
	ea.CommExec = bitfield.testBit(eaCommExec)
	ea.UseClockID = bitfield.testBit(eaUseClockID)
	ea.ContextSwitch = bitfield.testBit(eaContextSwitch)

	if ea.Freq {
		ea.SampleFreq = samplePeriodOrFreq
	} else {
		ea.SamplePeriod = samplePeriodOrFreq
	}

	if ea.Watermark {
		readOrPanic(buf, &ea.WakeupWatermark)
	} else {
		readOrPanic(buf, &ea.WakeupEvents)
	}

	readOrPanic(buf, &ea.BPType)

	// Things start getting funky from here on out due to sizes and field
	// use changes, etc.

	if ea.Size < sizeofPerfEventAttrVer1 {
		if ea.Type == PERF_TYPE_BREAKPOINT {
			return errors.New("EventAttr struct too small for PERF_TYPE_BREAKPOINT")
		}
		return nil
	}

	if ea.Type == PERF_TYPE_BREAKPOINT {
		readOrPanic(buf, &ea.BPAddr)
		readOrPanic(buf, &ea.BPLen)
	} else {
		readOrPanic(buf, &ea.Config1)
		readOrPanic(buf, &ea.Config2)
	}

	if ea.Size < sizeofPerfEventAttrVer2 {
		if ea.SampleType&PERF_SAMPLE_BRANCH_STACK != 0 {
			return errors.New("EventAttr struct too small for PERF_SAMPLE_BRANCH_STACK")
		}
		return nil
	}

	readOrPanic(buf, &ea.BranchSampleType)

	if ea.Size < sizeofPerfEventAttrVer3 {
		if ea.SampleType&PERF_SAMPLE_REGS_USER != 0 {
			return errors.New("EventAttr struct too small for PERF_SAMPLE_REGS_USER")
		}
		if ea.SampleType&PERF_SAMPLE_STACK_USER != 0 {
			return errors.New("EventAttr struct too small for PERF_SAMPLE_STACK_USER")
		}
		if ea.UseClockID {
			return errors.New("EventAttr struct too small for UseClockID == true")
		}
		return nil
	}

	readOrPanic(buf, &ea.SampleRegsUser)
	readOrPanic(buf, &ea.SampleStackUser)
	readOrPanic(buf, &ea.ClockID)

	if ea.Size < sizeofPerfEventAttrVer4 {
		if ea.SampleType&PERF_SAMPLE_REGS_INTR != 0 {
			return errors.New("EventAttr struct too small for PERF_SAMPLE_REGS_INTR")
		}
		return nil
	}

	readOrPanic(buf, &ea.SampleRegsIntr)

	if ea.Size < sizeofPerfEventAttrVer5 {
		return nil
	}

	readOrPanic(buf, &ea.AuxWatermark)
	readOrPanic(buf, &ea.SampleMaxStack)

	var reserved uint16
	readOrPanic(buf, &reserved)

	return
}

func TestEventAttrWriteSizes(t *testing.T) {
	// The first tests check to ensure that sizes are chosen correctly.
	// Sizes are chosen based on fields in use. New fields have been added
	// in different kernel versions.
	type sizeTestCase struct {
		size uint32
		attr EventAttr
	}
	sizeTestCases := []sizeTestCase{
		// First published struct
		sizeTestCase{
			sizeofPerfEventAttrVer0,
			EventAttr{},
		},

		// add: config2
		sizeTestCase{
			sizeofPerfEventAttrVer1,
			EventAttr{
				Config2: 8,
			},
		},

		// add: branch_sample_type
		// -- ignored if PERF_SAMPLE_BRANCH_STACK not set in SampleType
		sizeTestCase{
			sizeofPerfEventAttrVer2,
			EventAttr{
				SampleType: PERF_SAMPLE_BRANCH_STACK,
			},
		},

		// add: sample_regs_user, sample_stack_user
		// -- ignored if neither PERF_SAMPLE_REGS_USER nor
		//    PERF_SAMPLE_STACK_USER are set in SampleType
		// -- UseClockID enables use of reserved field added here,
		//    later renamed to clockid in 4.1
		sizeTestCase{
			sizeofPerfEventAttrVer3,
			EventAttr{
				SampleType: PERF_SAMPLE_REGS_USER,
			},
		},
		sizeTestCase{
			sizeofPerfEventAttrVer3,
			EventAttr{
				SampleType: PERF_SAMPLE_STACK_USER,
			},
		},
		sizeTestCase{
			sizeofPerfEventAttrVer3,
			EventAttr{
				UseClockID: true,
			},
		},

		// add: sample_regs_intr
		// -- ignored if PERF_SAMPLE_REGS_INTR not set in SampleType
		sizeTestCase{
			sizeofPerfEventAttrVer4,
			EventAttr{
				SampleType: PERF_SAMPLE_REGS_INTR,
			},
		},

		// add: aux_watermark
		// -- sample_max_stack added later, but uses a reserved field
		//    included with the addition of aux_watermark, so both use
		//    the same size
		sizeTestCase{
			sizeofPerfEventAttrVer5,
			EventAttr{
				AuxWatermark: 468237,
			},
		},
		sizeTestCase{
			sizeofPerfEventAttrVer5,
			EventAttr{
				SampleMaxStack: 32768,
			},
		},
	}
	for _, tc := range sizeTestCases {
		writeBuffer := &bytes.Buffer{}
		err := tc.attr.write(writeBuffer)
		assert.NoError(t, err)

		var actualSize, actualType uint32
		readBuffer := bytes.NewBuffer(writeBuffer.Bytes())
		binary.Read(readBuffer, binary.LittleEndian, &actualType)
		binary.Read(readBuffer, binary.LittleEndian, &actualSize)

		assert.Equal(t, tc.size, actualSize)

		// Don't check the exact size of the write buffer here since
		// all fields are always written. But the length should be at
		// least as long as the size.
		assert.Truef(t, actualSize <= uint32(writeBuffer.Len()),
			"bytes written does not match attr.Size (expected %d; got %d)",
			actualSize, writeBuffer.Len())
	}
}

func TestEventAttrWriteFailures(t *testing.T) {
	// These failure cases are not exhaustive.
	failAttrs := []EventAttr{
		EventAttr{
			Freq:       false,
			SampleFreq: 1,
		},
		EventAttr{
			Freq:         true,
			SamplePeriod: 1,
		},
		EventAttr{
			Watermark:       false,
			WakeupWatermark: 1,
		},
		EventAttr{
			Watermark:    true,
			WakeupEvents: 1,
		},
		EventAttr{
			Type:    PERF_TYPE_BREAKPOINT,
			Config1: 1,
		},
		EventAttr{
			Type:    PERF_TYPE_BREAKPOINT,
			Config2: 1,
		},
		EventAttr{
			Type:   PERF_TYPE_TRACEPOINT,
			BPAddr: 1,
		},
		EventAttr{
			Type:  PERF_TYPE_HARDWARE,
			BPLen: 1,
		},
		EventAttr{
			PreciseIP: 4,
		},
	}
	for _, attr := range failAttrs {
		writeBuffer := &bytes.Buffer{}
		err := attr.write(writeBuffer)
		assert.Error(t, err)
	}
}

func TestEventAttrWriteContent(t *testing.T) {
	testCases := []EventAttr{
		// Ensure the Freq flag does what it's supposed to
		EventAttr{
			Freq:       true,
			SampleFreq: 827634,
		},
		EventAttr{
			Freq:         false,
			SamplePeriod: 298347,
		},
		// Quick checks for all possible PreciseIP values
		EventAttr{PreciseIP: 1},
		EventAttr{PreciseIP: 2},
		EventAttr{PreciseIP: 3},
		// Ensure the Watermark flag does what it's supposed to
		EventAttr{
			Watermark:       true,
			WakeupWatermark: 293478,
		},
		EventAttr{
			Watermark:    false,
			WakeupEvents: 7834,
		},
		// Ensure proper handling for PERF_TYPE_BREAKPOINT
		EventAttr{
			Type:   PERF_TYPE_BREAKPOINT,
			BPAddr: 249378,
			BPLen:  23467,
		},
		EventAttr{
			Type:    PERF_TYPE_RAW,
			Config1: 2367,
			Config2: 945367,
		},
		// Various checks for Type, Config, SampleType, ReadFormat
		EventAttr{
			Type:             PERF_TYPE_HW_CACHE,
			Config:           942783,
			SampleType:       71253,
			ReadFormat:       57698,
			BPType:           32467,
			BranchSampleType: 236745,
			SampleRegsUser:   3247,
			SampleStackUser:  34785,
			ClockID:          498,
			SampleRegsIntr:   91823,
			AuxWatermark:     923,
			SampleMaxStack:   9685,
		},
	}

	for _, expectedAttr := range testCases {
		writeBuffer := &bytes.Buffer{}
		err := expectedAttr.write(writeBuffer)
		assert.NoError(t, err)

		var actualAttr EventAttr
		readBuffer := bytes.NewBuffer(writeBuffer.Bytes())
		err = actualAttr.read(readBuffer)
		if assert.NoError(t, err) {
			actualAttr.computeSizes()
			assert.Equal(t, expectedAttr, actualAttr)
		}
	}

	bitfieldNames := []string{
		"Disabled", "Inherit", "Pinned", "Exclusive", "ExcludeUser",
		"ExcludeKernel", "ExcludeHV", "ExcludeIdle", "Mmap", "Comm",
		"Freq", "InheritStat", "EnableOnExec", "Task", "Watermark",
		"MmapData", "SampleIDAll", "ExcludeHost", "ExcludeGuest",
		"ExcludeCallchainKernel", "ExcludeCallchainUser", "Mmap2",
		"CommExec", "UseClockID", "ContextSwitch",
	}
	for _, name := range bitfieldNames {
		// Must be a pointer here to be able to use reflection to set
		// fields
		ea := &EventAttr{}
		reflect.ValueOf(ea).Elem().FieldByName(name).SetBool(true)
		writeBuffer := &bytes.Buffer{}
		err := ea.write(writeBuffer)
		assert.NoError(t, err)

		var actualAttr EventAttr
		readBuffer := bytes.NewBuffer(writeBuffer.Bytes())
		err = actualAttr.read(readBuffer)
		assert.NoError(t, err)

		assert.Equal(t, *ea, actualAttr)
	}
}

func (cg *CounterGroup) write(t *testing.T, writeBuffer io.Writer, format uint64) {
	if format&PERF_FORMAT_GROUP != 0 {
		err := binary.Write(writeBuffer, binary.LittleEndian, uint64(len(cg.Values)))
		assert.NoError(t, err)
		if format&PERF_FORMAT_TOTAL_TIME_ENABLED != 0 {
			err = binary.Write(writeBuffer, binary.LittleEndian, cg.TimeEnabled)
			assert.NoError(t, err)
		}
		if format&PERF_FORMAT_TOTAL_TIME_RUNNING != 0 {
			err = binary.Write(writeBuffer, binary.LittleEndian, cg.TimeRunning)
			assert.NoError(t, err)
		}
		for _, v := range cg.Values {
			err = binary.Write(writeBuffer, binary.LittleEndian, v.Value)
			assert.NoError(t, err)
			if format&PERF_FORMAT_ID != 0 {
				err = binary.Write(writeBuffer, binary.LittleEndian, v.ID)
				assert.NoError(t, err)
			}
		}
	} else {
		assert.Len(t, cg.Values, 1) // ensure the test is correct

		err := binary.Write(writeBuffer, binary.LittleEndian, cg.Values[0].Value)
		assert.NoError(t, err)
		if format&PERF_FORMAT_TOTAL_TIME_ENABLED != 0 {
			err = binary.Write(writeBuffer, binary.LittleEndian, cg.TimeEnabled)
			assert.NoError(t, err)
		}
		if format&PERF_FORMAT_TOTAL_TIME_RUNNING != 0 {
			err = binary.Write(writeBuffer, binary.LittleEndian, cg.TimeRunning)
			assert.NoError(t, err)
		}
		if format&PERF_FORMAT_ID != 0 {
			err = binary.Write(writeBuffer, binary.LittleEndian, cg.Values[0].ID)
			assert.NoError(t, err)
		}
	}
}

func TestCounterGroupRead(t *testing.T) {
	expectedGroup := CounterGroup{
		TimeEnabled: 98237645,
		TimeRunning: 20938745,
		Values: []CounterValue{
			CounterValue{
				ID:    92836457,
				Value: 923478,
			},
		},
	}

	expectedZeroFormatGroup := CounterGroup{
		Values: []CounterValue{
			CounterValue{
				Value: 879649587,
			},
		},
	}

	type testCase struct {
		expected CounterGroup
		format   uint64
		offset   int
	}

	testCases := []testCase{
		testCase{
			expected: expectedGroup,
			format:   PERF_FORMAT_GROUP | PERF_FORMAT_TOTAL_TIME_ENABLED | PERF_FORMAT_TOTAL_TIME_RUNNING | PERF_FORMAT_ID,
			offset:   40,
		},
		testCase{
			expected: expectedGroup,
			format:   PERF_FORMAT_TOTAL_TIME_ENABLED | PERF_FORMAT_TOTAL_TIME_RUNNING | PERF_FORMAT_ID,
			offset:   32,
		},
		testCase{
			expected: expectedZeroFormatGroup,
			format:   PERF_FORMAT_GROUP,
			offset:   16,
		},
		testCase{
			expected: expectedZeroFormatGroup,
			format:   0,
			offset:   8,
		},
	}
	for _, tc := range testCases {
		writeBuffer := &bytes.Buffer{}
		tc.expected.write(t, writeBuffer, tc.format)

		var actualGroup CounterGroup
		o, err := actualGroup.read(writeBuffer.Bytes(), tc.format)
		if assert.NoError(t, err) {
			assert.Equal(t, tc.offset, o)
			assert.Equal(t, tc.expected, actualGroup)
		}
	}

	// Test for handling of truncated data
	badFormats := []uint64{
		PERF_FORMAT_GROUP,
		PERF_FORMAT_GROUP | PERF_FORMAT_ID,
	}
	for _, format := range badFormats {
		writeBuffer := &bytes.Buffer{}
		expectedGroup.write(t, writeBuffer, format)

		var actualGroup CounterGroup
		b := writeBuffer.Bytes()
		_, err := actualGroup.read(b[:len(b)-2], format)
		assert.Error(t, err)
	}
}

func (s *Sample) write(t *testing.T, w io.Writer, attr EventAttr) {
	if attr.SampleType&PERF_SAMPLE_IDENTIFIER != 0 {
		err := binary.Write(w, binary.LittleEndian, s.ID)
		assert.NoError(t, err)
	}
	if attr.SampleType&PERF_SAMPLE_IP != 0 {
		err := binary.Write(w, binary.LittleEndian, s.IP)
		assert.NoError(t, err)
	}
	if attr.SampleType&PERF_SAMPLE_TID != 0 {
		err := binary.Write(w, binary.LittleEndian, s.PID)
		assert.NoError(t, err)
		err = binary.Write(w, binary.LittleEndian, s.TID)
	}
	if attr.SampleType&PERF_SAMPLE_TIME != 0 {
		err := binary.Write(w, binary.LittleEndian, s.Time)
		assert.NoError(t, err)
	}
	if attr.SampleType&PERF_SAMPLE_ADDR != 0 {
		err := binary.Write(w, binary.LittleEndian, s.Addr)
		assert.NoError(t, err)
	}
	if attr.SampleType&PERF_SAMPLE_ID != 0 {
		err := binary.Write(w, binary.LittleEndian, s.ID)
		assert.NoError(t, err)
	}
	if attr.SampleType&PERF_SAMPLE_STREAM_ID != 0 {
		err := binary.Write(w, binary.LittleEndian, s.StreamID)
		assert.NoError(t, err)
	}
	if attr.SampleType&PERF_SAMPLE_CPU != 0 {
		err := binary.Write(w, binary.LittleEndian, s.CPU)
		assert.NoError(t, err)
		err = binary.Write(w, binary.LittleEndian, uint32(0))
		assert.NoError(t, err)
	}
	if attr.SampleType&PERF_SAMPLE_PERIOD != 0 {
		err := binary.Write(w, binary.LittleEndian, s.Period)
		assert.NoError(t, err)
	}
	if attr.SampleType&PERF_SAMPLE_READ != 0 {
		s.V.write(t, w, attr.ReadFormat)
	}
	if attr.SampleType&PERF_SAMPLE_CALLCHAIN != 0 {
		err := binary.Write(w, binary.LittleEndian, uint64(len(s.IPs)))
		assert.NoError(t, err)
		for _, ip := range s.IPs {
			err = binary.Write(w, binary.LittleEndian, ip)
			assert.NoError(t, err)
		}
	}
	if attr.SampleType&PERF_SAMPLE_RAW != 0 {
		err := binary.Write(w, binary.LittleEndian, uint32(len(s.RawData)))
		assert.NoError(t, err)
		_, err = w.Write(s.RawData)
		assert.NoError(t, err)
	}
	if attr.SampleType&PERF_SAMPLE_BRANCH_STACK != 0 {
		err := binary.Write(w, binary.LittleEndian, uint64(len(s.Branches)))
		assert.NoError(t, err)
		for _, b := range s.Branches {
			err = binary.Write(w, binary.LittleEndian, b.From)
			assert.NoError(t, err)
			err = binary.Write(w, binary.LittleEndian, b.To)
			assert.NoError(t, err)

			var flags uint64
			if b.Mispred {
				flags |= 1 << 0
			}
			if b.Predicted {
				flags |= 1 << 1
			}
			if b.InTx {
				flags |= 1 << 2
			}
			if b.Abort {
				flags |= 1 << 3
			}
			flags |= uint64(b.Cycles) << 4
			err = binary.Write(w, binary.LittleEndian, flags)
			assert.NoError(t, err)
		}
	}
}

func TestSampleReadSampleRecord(t *testing.T) {
	expectedRecord := Sample{
		SampleID: SampleID{
			ID:       423,      // PERF_SAMPLE_ID
			StreamID: 3598,     // PERF_SAMPLE_STREAM_ID
			CPU:      2,        // PERF_SAMPLE_CPU
			Time:     23948576, // PERF_SAMPLE_TIME
			PID:      237846,   // PERF_SAMPLE_TID
			TID:      287346,
		},
		IP:     98276345, // PERF_SAMPLE_IP
		Addr:   9467805,  // PERF_SAMPLE_ADDR
		Period: 876,      // PERF_SAMPLE_PERIOD

		// PERF_SAMPLE_READ
		V: CounterGroup{
			TimeEnabled: 297843,
			TimeRunning: 102983457,
			Values: []CounterValue{
				CounterValue{97854, 42837},
			},
		},

		// PERF_SAMPLE_CALLCHAIN
		IPs: []uint64{123, 456, 789},

		// PERF_SAMPLE_RAW
		RawData: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14},

		// PERF_SAMPLE_BRANCH_STACK
		Branches: []BranchEntry{
			BranchEntry{
				From:      243,
				To:        2341,
				Mispred:   false,
				Predicted: true,
				InTx:      false,
				Abort:     true,
				Cycles:    123,
			},
		},

		// Not Implemented:
		// PERF_SAMPLE_REGS_USER
		// PERF_SAMPLE_STACK_USER
		// PERF_SAMPLE_WEIGHT
		// PERF_SAMPLE_DATA_SRC
		// PERF_SAMPLE_TRANSACTION
		// PERF_SAMPLE_REGS_INTR
	}

	attr := EventAttr{}
	attr.SampleType = PERF_SAMPLE_IP | PERF_SAMPLE_TID | PERF_SAMPLE_TIME |
		PERF_SAMPLE_ADDR | PERF_SAMPLE_ID | PERF_SAMPLE_STREAM_ID |
		PERF_SAMPLE_CPU | PERF_SAMPLE_PERIOD |
		PERF_SAMPLE_READ | PERF_SAMPLE_CALLCHAIN | PERF_SAMPLE_RAW |
		PERF_SAMPLE_BRANCH_STACK
	attr.ReadFormat = PERF_FORMAT_GROUP | PERF_FORMAT_ID |
		PERF_FORMAT_TOTAL_TIME_ENABLED | PERF_FORMAT_TOTAL_TIME_RUNNING

	writeBuffer := &bytes.Buffer{}
	expectedRecord.write(t, writeBuffer, attr)

	var actualRecord Sample
	err := actualRecord.readSampleRecord(writeBuffer.Bytes(), &attr)
	if assert.NoError(t, err) {
		assert.Equal(t, expectedRecord, actualRecord)
	}

	// Ensure that variable length sample types propagate errors up when
	// the data is not available.
	varSampleTypes := []uint64{
		PERF_SAMPLE_READ,
		PERF_SAMPLE_CALLCHAIN,
		PERF_SAMPLE_RAW,
		PERF_SAMPLE_BRANCH_STACK,
	}
	for _, sampleType := range varSampleTypes {
		attr.SampleType = sampleType
		writeBuffer = &bytes.Buffer{}
		expectedRecord.write(t, writeBuffer, attr)
		b := writeBuffer.Bytes()
		err = actualRecord.readSampleRecord(b[:len(b)-2], &attr)
		assert.Error(t, err)
	}

	// Ensure unimplemented sample types result in an error
	badSampleTypes := []uint64{
		PERF_SAMPLE_REGS_USER,
		PERF_SAMPLE_STACK_USER,
		PERF_SAMPLE_WEIGHT,
		PERF_SAMPLE_DATA_SRC,
		PERF_SAMPLE_TRANSACTION,
		PERF_SAMPLE_REGS_INTR,
	}
	for _, sampleType := range badSampleTypes {
		attr.SampleType = sampleType
		writeBuffer = &bytes.Buffer{}
		expectedRecord.write(t, writeBuffer, attr)
		err = actualRecord.readSampleRecord(writeBuffer.Bytes(), &attr)
		assert.Error(t, err)
	}
}

func (sid *SampleID) write(t *testing.T, w io.Writer, sampleType uint64) {
	if sampleType&PERF_SAMPLE_TID != 0 {
		err := binary.Write(w, binary.LittleEndian, sid.PID)
		assert.NoError(t, err)
		err = binary.Write(w, binary.LittleEndian, sid.TID)
		assert.NoError(t, err)
	}
	if sampleType&PERF_SAMPLE_TIME != 0 {
		err := binary.Write(w, binary.LittleEndian, sid.Time)
		assert.NoError(t, err)
	}
	if sampleType&PERF_SAMPLE_ID != 0 {
		err := binary.Write(w, binary.LittleEndian, sid.ID)
		assert.NoError(t, err)
	}
	if sampleType&PERF_SAMPLE_STREAM_ID != 0 {
		err := binary.Write(w, binary.LittleEndian, sid.StreamID)
		assert.NoError(t, err)
	}
	if sampleType&PERF_SAMPLE_CPU != 0 {
		err := binary.Write(w, binary.LittleEndian, sid.CPU)
		assert.NoError(t, err)
		err = binary.Write(w, binary.LittleEndian, uint32(0))
		assert.NoError(t, err)
	}
	if sampleType&PERF_SAMPLE_IDENTIFIER != 0 {
		err := binary.Write(w, binary.LittleEndian, sid.ID)
		assert.NoError(t, err)
	}
}

func TestSampleIDRead(t *testing.T) {
	expectedSampleID := SampleID{
		PID:      987243,
		TID:      92387,
		Time:     23965,
		ID:       123,
		StreamID: 87456,
		CPU:      1,
	}
	sampleType := PERF_SAMPLE_TID | PERF_SAMPLE_TIME | PERF_SAMPLE_ID |
		PERF_SAMPLE_STREAM_ID | PERF_SAMPLE_CPU | PERF_SAMPLE_IDENTIFIER

	writeBuffer := &bytes.Buffer{}
	expectedSampleID.write(t, writeBuffer, sampleType)

	var actualSampleID SampleID
	actualSampleID.read(writeBuffer.Bytes(),
		&EventAttr{SampleType: sampleType})
	assert.Equal(t, expectedSampleID, actualSampleID)
}

func TestSampleResolveEventAttr(t *testing.T) {
	expectedEventAttr := EventAttr{
		SampleType:  PERF_SAMPLE_IDENTIFIER, // must be set
		SampleIDAll: true,                   // must be set
	}
	formatMap := map[uint64]*EventAttr{
		768435: &expectedEventAttr,
	}
	expectedSampleID := uint64(768435)

	// 1. sampleID is in formatMap for PERF_RECORD_SAMPLE
	sample := Sample{}
	sample.Type = PERF_RECORD_SAMPLE
	writeBuffer := &bytes.Buffer{}
	err := binary.Write(writeBuffer, binary.LittleEndian, expectedSampleID)
	assert.NoError(t, err)

	actualEventAttr, err := sample.resolveEventAttr(writeBuffer.Bytes(), formatMap)
	assert.NoError(t, err)
	assert.Equal(t, expectedEventAttr, *actualEventAttr)

	// 2. sampleID is not in formatMap for PERF_RECORD_SAMPLE
	sample = Sample{}
	sample.Type = PERF_RECORD_SAMPLE
	writeBuffer = &bytes.Buffer{}
	err = binary.Write(writeBuffer, binary.LittleEndian, ^expectedSampleID)
	assert.NoError(t, err)

	_, err = sample.resolveEventAttr(writeBuffer.Bytes(), formatMap)
	assert.Error(t, err)

	// 3. sampleID is in formatMap for !PERF_RECORD_SAMPLE
	sample = Sample{}
	sample.Type = PERF_RECORD_LOST
	writeBuffer = &bytes.Buffer{}
	err = binary.Write(writeBuffer, binary.LittleEndian, uint64(237456))
	assert.NoError(t, err)
	err = binary.Write(writeBuffer, binary.LittleEndian, uint64(238476))
	assert.NoError(t, err)
	err = binary.Write(writeBuffer, binary.LittleEndian, expectedSampleID)
	assert.NoError(t, err)

	actualEventAttr, err = sample.resolveEventAttr(writeBuffer.Bytes(), formatMap)
	assert.NoError(t, err)
	assert.Equal(t, expectedEventAttr, *actualEventAttr)

	// 4. sampleID is not in formatMap for !PERF_RECORD_SAMPLE
	sample = Sample{}
	sample.Type = PERF_RECORD_LOST
	writeBuffer = &bytes.Buffer{}
	err = binary.Write(writeBuffer, binary.LittleEndian, uint64(237456))
	assert.NoError(t, err)
	err = binary.Write(writeBuffer, binary.LittleEndian, uint64(238476))
	assert.NoError(t, err)
	err = binary.Write(writeBuffer, binary.LittleEndian, ^expectedSampleID)
	assert.NoError(t, err)

	_, err = sample.resolveEventAttr(writeBuffer.Bytes(), formatMap)
	assert.Error(t, err)

	// 5. Not enough data is present to resolve EventAttr
	shortData := [4]byte{0x11, 0x22, 0x33, 0x44}
	_, err = sample.resolveEventAttr(shortData[:], formatMap)
	assert.Error(t, err)
}

func TestSampleRead(t *testing.T) {
	attr := EventAttr{
		SampleType:  PERF_SAMPLE_IDENTIFIER | PERF_SAMPLE_RAW,
		SampleIDAll: true,
	}
	attr.computeSizes()
	formatMap := map[uint64]*EventAttr{
		123: &attr,
	}

	testCases := []struct {
		rawData        []byte
		eventAttr      *EventAttr
		formatMap      map[uint64]*EventAttr
		expectError    bool
		expectedN      int
		expectedSample Sample
	}{
		// Error: not enough eventHeader data (0)
		{
			rawData: []byte{
				0x11, 0x22, 0x33, 0x44, // eventHeader
			},
			expectError: true,
		},
		// Error: not enough total data (1)
		{
			rawData: []byte{
				0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, // eventHeader
			},
			expectError: true,
		},
		// No error: eventHeader.Size == sizeofEventHeader (2)
		{
			rawData: []byte{
				0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x08, 0x00, // eventHeader
			},
			expectedN: 8,
			expectedSample: Sample{
				Type: 0x44332211,
				Misc: 0x6655,
			},
		},
		// Error: cannot resolve eventAttr (3)
		{
			rawData: []byte{
				0x09, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0A, 0x00, // eventHeader
				0x00, 0x00, // PERF_SAMPLE_IDENTIFIER
			},
			expectError: true,
			formatMap:   formatMap,
		},
		// Error: not PERF_RECORD_SAMPLE, not enough data for SampleID (4)
		{
			rawData: []byte{
				0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0A, 0x00, // eventHeader
				0x00, 0x00, // PERF_SAMPLE_IDENTIFIER
			},
			eventAttr:   &attr,
			expectError: true,
		},
		// Error: PERF_RECORD_LOST incomplete LostRecord (5)
		{
			rawData: []byte{
				0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x18, 0x00, // eventHeader
				0x7B, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // LostRecord.Id
				0x7B, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // PERF_SAMPLE_IDENTIFIER
			},
			eventAttr:   &attr,
			expectError: true,
		},
		// No error: PERF_RECORD_LOST (6)
		{
			rawData: []byte{
				0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, // eventHeader
				0x7B, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // LostRecord.Id
				0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, // LostRecord.Lost
				0x7B, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // PERF_SAMPLE_IDENTIFIER
			},
			eventAttr: &attr,
			expectedN: 32,
			expectedSample: Sample{
				Type: PERF_RECORD_LOST,
				SampleID: SampleID{
					ID: 123,
				},
				Lost: 0x8877665544332211,
			},
		},
		// Error: PERF_RECORD_SAMPLE incomplete SampleRecord (7)
		{
			rawData: []byte{
				0x09, 0x00, 0x00, 0x00, 0x00, 0x00, 0x14, 0x00, // eventHeader
				0x7B, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // PERF_SAMPLE_IDENTIFIER
				0x20, 0x00, 0x00, 0x00, // PERF_SAMPLE_RAW
			},
			eventAttr:   &attr,
			expectError: true,
		},
		// No error: PERF_SAMPLE_SAMPLE (8)
		{
			rawData: []byte{
				0x09, 0x00, 0x00, 0x00, 0x00, 0x00, 0x18, 0x00, // eventHeader
				0x7B, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // PERF_SAMPLE_IDENTIFIER
				0x04, 0x00, 0x00, 0x00, 0x11, 0x22, 0x33, 0x44, // PERF_SAMPLE_RAW
			},
			eventAttr: &attr,
			expectedN: 24,
			expectedSample: Sample{
				Type: PERF_RECORD_SAMPLE,
				SampleID: SampleID{
					ID: 123,
				},
				RawData: []byte{0x11, 0x22, 0x33, 0x44},
			},
		},
	}

	for i, tc := range testCases {
		var actualSample Sample

		n, err := actualSample.read(tc.rawData, tc.eventAttr, tc.formatMap)
		if tc.expectError {
			assert.Errorf(t, err, "test case %d", i)
		} else if assert.NoErrorf(t, err, "test case %d", i) {
			assert.Equalf(t, tc.expectedN, n, "test case %d", i)
			assert.Equalf(t, tc.expectedSample, actualSample, "test case %d", i)
		}
	}
}

func TestSampleDecodeRawData(t *testing.T) {
	sample := Sample{
		RawData: []byte{
			0x1c, 0x00, 0x06, 0x00, // name4
			0x22, 0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00, // name8
			0x11, 0x22, 0x33, 0x44, // pid
			0x01, 0x01, 0x01, 0x01, 0x02, 0x02, 0x02, 0x02, // args
			0x28, 0x00, 0x04, 0x00, // foo

			'N', 'A', 'M', 'E', '4', 0,
			'N', 'A', 'M', 'E', '8', 0,
			0x11, 0x22, 0x33, 0x44,
		},
		TraceFormat: TraceEventFormat{
			"name4": TraceEventField{
				FieldName:    "name4",
				Offset:       0,
				DataType:     expression.ValueTypeString,
				DataTypeSize: 1,
				DataLocSize:  4,
			},
			"name8": TraceEventField{
				FieldName:    "name8",
				Offset:       4,
				DataType:     expression.ValueTypeString,
				DataTypeSize: 1,
				DataLocSize:  8,
			},
			"pid": TraceEventField{
				FieldName: "pid",
				Offset:    12,
				DataType:  expression.ValueTypeSignedInt32,
			},
			"args": TraceEventField{
				FieldName:    "args",
				Offset:       16,
				DataType:     expression.ValueTypeUnsignedInt32,
				DataTypeSize: 4,
				ArraySize:    2,
			},
			"foo": TraceEventField{
				FieldName:    "foo",
				Offset:       24,
				DataType:     expression.ValueTypeUnsignedInt8,
				DataTypeSize: 1,
				DataLocSize:  4,
			},
		},
	}

	e := expression.FieldValueMap{
		"name4": "NAME4",
		"name8": "NAME8",
		"pid":   int32(0x44332211),
		"args":  []interface{}{uint32(0x01010101), uint32(0x02020202)},
		"foo":   []uint8{0x11, 0x22, 0x33, 0x44},
	}

	data, err := sample.DecodeRawData()
	assert.NoError(t, err)
	assert.Equal(t, e, data)

	sample.TraceFormat = TraceEventFormat{
		"error": TraceEventField{
			FieldName:   "error",
			DataLocSize: 16,
		},
	}
	_, err = sample.DecodeRawData()
	assert.Error(t, err)

	sample.TraceFormat = TraceEventFormat{
		"error": TraceEventField{
			FieldName: "error",
			DataType:  expression.ValueTypeString,
		},
	}
	_, err = sample.DecodeRawData()
	assert.Error(t, err)

	sample.TraceFormat = TraceEventFormat{
		"error": TraceEventField{
			FieldName: "error",
			DataType:  expression.ValueTypeString,
			ArraySize: 4,
		},
	}
	_, err = sample.DecodeRawData()
	assert.Error(t, err)

}

func TestSampleDecodeValue(t *testing.T) {
	sample := &Sample{
		RawData: []byte{0x04, 0x00, 0x04, 0x00, 0x12, 0x34, 0x56, 0x78},
		TraceFormat: TraceEventFormat{
			"foo": TraceEventField{
				FieldName:    "foo",
				Size:         4,
				IsSigned:     true,
				DataType:     expression.ValueTypeSignedInt8,
				DataTypeSize: 1,
				DataLocSize:  4,
			},
		},
	}

	expectedValue := []int8{0x12, 0x34, 0x56, 0x78}
	gotValue, err := sample.DecodeValue("foo")
	assert.NoError(t, err)
	assert.Equal(t, expectedValue, gotValue)

	_, err = sample.DecodeValue("bar")
	if assert.Error(t, err) {
		assert.IsType(t, expression.FieldNotSet{}, err)
	}
}

func TestSampleFieldValueGetterImplementation(t *testing.T) {
	testCases := []struct {
		method      string
		dataType    expression.ValueType
		dataLocSize int
		rawData     []byte
		alwaysFail  bool
		expected    interface{}
	}{
		{
			method:      "GetString",
			dataType:    expression.ValueTypeString,
			dataLocSize: 4,
			rawData:     []byte{4, 0, 9, 0, 'c', 'a', 'p', 's', 'u', 'l', 'e', '8', 0},
			expected:    "capsule8",
		},
		{
			method:   "GetSignedInt8",
			dataType: expression.ValueTypeSignedInt8,
			rawData:  []byte{0x8},
			expected: int8(0x8),
		},
		{
			method:   "GetSignedInt16",
			dataType: expression.ValueTypeSignedInt16,
			rawData:  []byte{0x8, 0x8},
			expected: int16(0x0808),
		},
		{
			method:   "GetSignedInt32",
			dataType: expression.ValueTypeSignedInt32,
			rawData:  []byte{0x8, 0x8, 0x8, 0x8},
			expected: int32(0x08080808),
		},
		{
			method:   "GetSignedInt64",
			dataType: expression.ValueTypeSignedInt64,
			rawData:  []byte{0x8, 0x8, 0x8, 0x8, 0x8, 0x8, 0x8, 0x8},
			expected: int64(0x0808080808080808),
		},
		{
			method:   "GetUnsignedInt8",
			dataType: expression.ValueTypeUnsignedInt8,
			rawData:  []byte{0x88},
			expected: uint8(0x88),
		},
		{
			method:   "GetUnsignedInt16",
			dataType: expression.ValueTypeUnsignedInt16,
			rawData:  []byte{0x88, 0x88},
			expected: uint16(0x8888),
		},
		{
			method:   "GetUnsignedInt32",
			dataType: expression.ValueTypeUnsignedInt32,
			rawData:  []byte{0x88, 0x88, 0x88, 0x88},
			expected: uint32(0x88888888),
		},
		{
			method:   "GetUnsignedInt64",
			dataType: expression.ValueTypeUnsignedInt64,
			rawData:  []byte{0x88, 0x88, 0x88, 0x88, 0x88, 0x88, 0x88, 0x88},
			expected: uint64(0x8888888888888888),
		},
		{
			method:     "GetBool",
			dataType:   expression.ValueTypeBool,
			alwaysFail: true,
		},
		{
			method:     "GetDouble",
			dataType:   expression.ValueTypeDouble,
			alwaysFail: true,
		},
		{
			method:     "GetTimestamp",
			dataType:   expression.ValueTypeTimestamp,
			alwaysFail: true,
		},
	}

	sample := &Sample{
		TraceFormat: TraceEventFormat{},
	}
	for i, tc := range testCases {
		sample.RawData = tc.rawData
		sample.TraceFormat["foo"] = TraceEventField{
			FieldName:   "foo",
			DataType:    tc.dataType,
			DataLocSize: tc.dataLocSize,
		}
		method := reflect.ValueOf(sample).MethodByName(tc.method)

		// Error: expression.FieldNotSet
		r := method.Call([]reflect.Value{reflect.ValueOf("bar")})
		if !r[1].IsNil() {
			err := r[1].Interface().(error)
			assert.Errorf(t, err, "index %d", i)
		} else {
			assert.Errorf(t, nil, "index %d", i)
		}

		if tc.alwaysFail {
			r = method.Call([]reflect.Value{reflect.ValueOf("foo")})
			if !r[1].IsNil() {
				err := r[1].Interface().(error)
				assert.Errorf(t, err, "index %d", i)
			} else {
				assert.Errorf(t, nil, "index %d", i)
			}
		} else {
			// NoError
			r = method.Call([]reflect.Value{reflect.ValueOf("foo")})
			if r[1].IsNil() {
				assert.Equalf(t, tc.expected, r[0].Interface(), "index %d", i)
			} else {
				err := r[1].Interface().(error)
				assert.NoErrorf(t, err, "index %d", i)
			}
		}
	}
}
