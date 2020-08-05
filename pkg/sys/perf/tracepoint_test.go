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
	"fmt"
	"reflect"
	"testing"

	"github.com/Happyholic1203/capsule8/pkg/expression"

	"github.com/stretchr/testify/assert"
)

func TestParseTypeAndName(t *testing.T) {
	type testCase struct {
		s        string
		size     int
		isSigned bool

		dataType     expression.ValueType
		dataTypeSize int
		dataLocSize  int
		arraySize    int
	}

	// These tests should return false, nil
	validTests := []testCase{
		testCase{"__data_loc char[] name", 4, true, expression.ValueTypeString, 1, 4, 0},
		testCase{"__data_loc long[] name", 4, true, expression.ValueTypeSignedInt64, 8, 4, 0},

		testCase{"short iii[4]", 8, true, expression.ValueTypeSignedInt16, 2, 0, 4},
		testCase{"short iii[NUM_PIDS]", 8, true, expression.ValueTypeSignedInt16, 2, 0, 4},
		testCase{"unknown_type x[8]", 32, false, expression.ValueTypeUnsignedInt32, 4, 0, 8},

		testCase{"bool yesno", 1, false, expression.ValueTypeUnsignedInt8, 1, 0, 0},
		testCase{"char x", 1, true, expression.ValueTypeSignedInt8, 1, 0, 0},
		testCase{"short x", 2, true, expression.ValueTypeSignedInt16, 2, 0, 0},
		testCase{"int x", 4, true, expression.ValueTypeSignedInt32, 4, 0, 0},
		testCase{"long x", 8, true, expression.ValueTypeSignedInt64, 8, 0, 0},
		testCase{"long long x", 8, true, expression.ValueTypeSignedInt64, 8, 0, 0},
		testCase{"unsigned char x", 1, false, expression.ValueTypeUnsignedInt8, 1, 0, 0},
		testCase{"unsigned short x", 2, false, expression.ValueTypeUnsignedInt16, 2, 0, 0},
		testCase{"unsigned int x", 4, false, expression.ValueTypeUnsignedInt32, 4, 0, 0},
		testCase{"unsigned long x", 8, false, expression.ValueTypeUnsignedInt64, 8, 0, 0},
		testCase{"unsigned long long x", 8, false, expression.ValueTypeUnsignedInt64, 8, 0, 0},

		testCase{"s8 x", 1, true, expression.ValueTypeSignedInt8, 1, 0, 0},
		testCase{"s16 x", 2, true, expression.ValueTypeSignedInt16, 2, 0, 0},
		testCase{"s32 x", 4, true, expression.ValueTypeSignedInt32, 4, 0, 0},
		testCase{"s64 x", 8, true, expression.ValueTypeSignedInt64, 8, 0, 0},
		testCase{"u8 x", 1, true, expression.ValueTypeUnsignedInt8, 1, 0, 0},
		testCase{"u16 x", 2, true, expression.ValueTypeUnsignedInt16, 2, 0, 0},
		testCase{"u32 x", 4, true, expression.ValueTypeUnsignedInt32, 4, 0, 0},
		testCase{"u64 x", 8, true, expression.ValueTypeUnsignedInt64, 8, 0, 0},

		testCase{"pid_t pid", 4, true, expression.ValueTypeSignedInt32, 4, 0, 0},
		testCase{"const char *pointer", 8, true, expression.ValueTypeSignedInt64, 8, 0, 0},
		testCase{"const char **pointer", 8, true, expression.ValueTypeSignedInt64, 8, 0, 0},
		testCase{"enum color c", 4, false, expression.ValueTypeUnsignedInt32, 4, 0, 0},

		testCase{"unknown_type x", 1, true, expression.ValueTypeSignedInt8, 1, 0, 0},
		testCase{"unknown_type x", 2, true, expression.ValueTypeSignedInt16, 2, 0, 0},
		testCase{"unknown_type x", 4, true, expression.ValueTypeSignedInt32, 4, 0, 0},
		testCase{"unknown_type x", 8, true, expression.ValueTypeSignedInt64, 8, 0, 0},
		testCase{"unknown_type x", 1, false, expression.ValueTypeUnsignedInt8, 1, 0, 0},
		testCase{"unknown_type x", 2, false, expression.ValueTypeUnsignedInt16, 2, 0, 0},
		testCase{"unknown_type x", 4, false, expression.ValueTypeUnsignedInt32, 4, 0, 0},
		testCase{"unknown_type x", 8, false, expression.ValueTypeUnsignedInt64, 8, 0, 0},

		// For ints with kernel misrepresented sizes
		testCase{"int x", 16, true, expression.ValueTypeSignedInt32, 4, 0, 0},
		testCase{"unsigned int x", 16, false, expression.ValueTypeUnsignedInt32, 4, 0, 0},
		testCase{"long x", 16, true, expression.ValueTypeSignedInt64, 8, 0, 0},
		testCase{"unsigned long x", 16, false, expression.ValueTypeUnsignedInt64, 8, 0, 0},

		// Misrepresented array sizes
		testCase{"u64 mismatch[6]", 24, false, expression.ValueTypeUnsignedInt64, 8, 0, 3},
	}
	for _, tc := range validTests {
		field := TraceEventField{
			Size:     tc.size,
			IsSigned: tc.isSigned,
		}
		skip, err := field.parseTypeAndName(tc.s)
		assert.NoError(t, err)
		assert.False(t, skip)

		assert.Equalf(t, tc.dataType, field.DataType, "bad dataType for %s (got %d)", tc.s, field.DataType)
		assert.Equalf(t, tc.dataTypeSize, field.DataTypeSize, "bad dataTypeSize for %s (got %d)", tc.s, field.DataTypeSize)
		assert.Equalf(t, tc.dataLocSize, field.DataLocSize, "bad dataLocSize for %s (got %d)", tc.s, field.DataLocSize)
		assert.Equalf(t, tc.arraySize, field.ArraySize, "bad arraySize for %s (got %d)", tc.s, field.ArraySize)
	}

	// These tests should return true, nil
	skipTests := []testCase{
		testCase{"__data_loc struct foo[] bar", 32, false, 0, 0, 0, 0},
		testCase{"struct foo bar", 32, false, 0, 0, 0, 0},
		testCase{"union foo bar", 8, false, 0, 0, 0, 0},
		testCase{"gid_t groups[NGROUP]", 64, true, 0, 0, 0, 0},
	}
	for _, tc := range skipTests {
		field := TraceEventField{
			Size:     tc.size,
			IsSigned: tc.isSigned,
		}
		skip, err := field.parseTypeAndName(tc.s)
		assert.NoError(t, err)
		assert.True(t, skip)
	}

	// These tests should return an error
	invalidTests := []testCase{
		testCase{"__data_loc char name", 1, true, 0, 0, 0, 0},
		testCase{"char[] name", 1, true, 0, 0, 0, 0},
		testCase{"char [", 1, true, 0, 0, 0, 0},
		testCase{"bool", 1, false, expression.ValueTypeUnsignedInt8, 1, 0, 0},
	}
	for _, tc := range invalidTests {
		field := TraceEventField{
			Size:     tc.size,
			IsSigned: tc.isSigned,
		}
		_, err := field.parseTypeAndName(tc.s)
		assert.Error(t, err)
	}
}

func TestGetTraceEventFormat(t *testing.T) {
	var err error

	_, _, err = getTraceEventFormat("testdata", "nonexistent")
	assert.Error(t, err)

	invalidTests := []string{
		"id", "field", "offset", "size", "signed", "type",
	}
	for _, s := range invalidTests {
		name := fmt.Sprintf("invalid/invalid_%s", s)
		_, _, err = getTraceEventFormat("testdata", name)
		assert.Error(t, err)
	}

	expectedFormat := TraceEventFormat{
		"common_type": TraceEventField{
			FieldName:    "common_type",
			TypeName:     "unsigned short",
			Offset:       0,
			Size:         2,
			IsSigned:     false,
			DataType:     expression.ValueTypeUnsignedInt16,
			DataTypeSize: 2,
		},
		"common_flags": TraceEventField{
			FieldName:    "common_flags",
			TypeName:     "unsigned char",
			Offset:       2,
			Size:         1,
			IsSigned:     false,
			DataType:     expression.ValueTypeUnsignedInt8,
			DataTypeSize: 1,
		},
		"common_preempt_count": TraceEventField{
			FieldName:    "common_preempt_count",
			TypeName:     "unsigned char",
			Offset:       3,
			Size:         1,
			IsSigned:     false,
			DataType:     expression.ValueTypeUnsignedInt8,
			DataTypeSize: 1,
		},
		"common_pid": TraceEventField{
			FieldName:    "common_pid",
			TypeName:     "int",
			Offset:       4,
			Size:         4,
			IsSigned:     true,
			DataType:     expression.ValueTypeSignedInt32,
			DataTypeSize: 4,
		},
		"name": TraceEventField{
			FieldName:    "name",
			TypeName:     "char",
			Offset:       8,
			Size:         4,
			IsSigned:     true,
			DataType:     expression.ValueTypeString,
			DataTypeSize: 1,
			DataLocSize:  4,
		},
		"longs": TraceEventField{
			FieldName:    "longs",
			TypeName:     "long",
			Offset:       12,
			Size:         4,
			IsSigned:     true,
			DataType:     expression.ValueTypeSignedInt64,
			DataTypeSize: 8,
			DataLocSize:  4,
		},
		// Skips are turned into arrays of bytes
		"signed_skip": TraceEventField{
			FieldName:    "signed_skip",
			TypeName:     "gid_t",
			Offset:       16,
			Size:         64,
			IsSigned:     true,
			DataType:     expression.ValueTypeSignedInt8,
			DataTypeSize: 1,
			ArraySize:    64,
		},
		"unsigned_skip": TraceEventField{
			FieldName:    "unsigned_skip",
			TypeName:     "gid_t",
			Offset:       80,
			Size:         64,
			IsSigned:     false,
			DataType:     expression.ValueTypeUnsignedInt8,
			DataTypeSize: 1,
			ArraySize:    64,
		},
	}

	id, actualFormat, err := getTraceEventFormat("testdata", "valid/valid")
	assert.NoError(t, err)
	assert.Equal(t, uint16(31337), id)
	assert.Equal(t, expectedFormat, actualFormat)
}

var allValueTypes = []expression.ValueType{
	expression.ValueTypeString,
	expression.ValueTypeSignedInt8,
	expression.ValueTypeSignedInt16,
	expression.ValueTypeSignedInt32,
	expression.ValueTypeSignedInt64,
	expression.ValueTypeUnsignedInt8,
	expression.ValueTypeUnsignedInt16,
	expression.ValueTypeUnsignedInt32,
	expression.ValueTypeUnsignedInt64,
}

func valueTypesExcept(valueType expression.ValueType) []expression.ValueType {
	result := make([]expression.ValueType, 0, len(allValueTypes)-1)
	for _, t := range allValueTypes {
		if t != valueType {
			result = append(result, t)
		}
	}
	return result
}

func TestTraceEventFieldTypeMismatch(t *testing.T) {
	for _, o := range allValueTypes {
		for _, i := range valueTypesExcept(o) {
			field := TraceEventField{
				FieldName: "foo",
				DataType:  o,
			}
			err := field.typeMismatch(i)
			if assert.NotNil(t, err) {
				assert.Equal(t, field.FieldName, err.Name)
				assert.Equal(t, i, err.ExpectedType)
				assert.Equal(t, o, err.ActualType)
			}
		}
	}
}

func TestTraceEventFieldDataOffsetAndLength(t *testing.T) {
	testCases := []struct {
		dataLocSize    int
		rawData        []byte
		expectError    bool
		expectedOffset int
		expectedLength int
	}{
		{
			dataLocSize: 2,
			rawData:     []byte{0x11, 0x22},
			expectError: true,
		},
		{
			dataLocSize:    4,
			rawData:        []byte{0x11, 0x22, 0x33, 0x44},
			expectedOffset: 0x2211,
			expectedLength: 0x4433,
		},
		{
			dataLocSize:    8,
			rawData:        []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88},
			expectedOffset: 0x44332211,
			expectedLength: 0x88776655,
		},
	}

	for i, tc := range testCases {
		field := TraceEventField{DataLocSize: tc.dataLocSize}
		o, l, err := field.dataOffsetAndLength(tc.rawData)
		if tc.expectError {
			assert.Errorf(t, err, "index %d", i)
		} else if assert.NoErrorf(t, err, "index %d", i) {
			assert.Equalf(t, tc.expectedOffset, o, "index %d", i)
			assert.Equalf(t, tc.expectedLength, l, "index %d", i)
		}
	}
}

func TestTraceEventFieldDecodeString(t *testing.T) {
	rawData := []byte{4, 0, 9, 0, 'c', 'a', 'p', 's', 'u', 'l', 'e', '8', 0}
	for _, valueType := range valueTypesExcept(expression.ValueTypeString) {
		field := TraceEventField{
			DataType: valueType,
		}
		_, err := field.DecodeString(rawData)
		assert.Error(t, err, expression.ValueTypeStrings[valueType])
	}

	field := TraceEventField{
		DataType:    expression.ValueTypeString,
		DataLocSize: 2,
	}
	_, err := field.DecodeString(rawData)
	assert.Error(t, err)

	field = TraceEventField{
		DataType:    expression.ValueTypeString,
		DataLocSize: 4,
	}
	s, err := field.DecodeString(rawData)
	assert.NoError(t, err)
	assert.Equal(t, "capsule8", s)
}

func TestTraceEventFieldDecodeIntegers(t *testing.T) {
	testCases := []struct {
		method   string
		dataType expression.ValueType
		rawData  []byte
		expected interface{}
	}{
		{
			method:   "DecodeSignedInt8",
			dataType: expression.ValueTypeSignedInt8,
			rawData:  []byte{0x8},
			expected: int8(0x8),
		},
		{
			method:   "DecodeSignedInt16",
			dataType: expression.ValueTypeSignedInt16,
			rawData:  []byte{0x8, 0x8},
			expected: int16(0x0808),
		},
		{
			method:   "DecodeSignedInt32",
			dataType: expression.ValueTypeSignedInt32,
			rawData:  []byte{0x8, 0x8, 0x8, 0x8},
			expected: int32(0x08080808),
		},
		{
			method:   "DecodeSignedInt64",
			dataType: expression.ValueTypeSignedInt64,
			rawData:  []byte{0x8, 0x8, 0x8, 0x8, 0x8, 0x8, 0x8, 0x8},
			expected: int64(0x0808080808080808),
		},
		{
			method:   "DecodeUnsignedInt8",
			dataType: expression.ValueTypeUnsignedInt8,
			rawData:  []byte{0x88},
			expected: uint8(0x88),
		},
		{
			method:   "DecodeUnsignedInt16",
			dataType: expression.ValueTypeUnsignedInt16,
			rawData:  []byte{0x88, 0x88},
			expected: uint16(0x8888),
		},
		{
			method:   "DecodeUnsignedInt32",
			dataType: expression.ValueTypeUnsignedInt32,
			rawData:  []byte{0x88, 0x88, 0x88, 0x88},
			expected: uint32(0x88888888),
		},
		{
			method:   "DecodeUnsignedInt64",
			dataType: expression.ValueTypeUnsignedInt64,
			rawData:  []byte{0x88, 0x88, 0x88, 0x88, 0x88, 0x88, 0x88, 0x88},
			expected: uint64(0x8888888888888888),
		},
	}

	for i, tc := range testCases {
		field := &TraceEventField{}
		fieldValue := reflect.ValueOf(field)
		method := fieldValue.MethodByName(tc.method)
		for _, valueType := range valueTypesExcept(tc.dataType) {
			field.DataType = valueType
			r := method.Call([]reflect.Value{reflect.ValueOf(tc.rawData)})
			if !r[1].IsNil() {
				err := r[1].Interface().(error)
				assert.Errorf(t, err, "index %d", i)
			} else {
				assert.Errorf(t, nil, "index %d", i)
			}
		}

		// Set the correct data type. Ensure that arrays fail as they
		// should.
		field.DataType = tc.dataType

		field.ArraySize = 234
		r := method.Call([]reflect.Value{reflect.ValueOf(tc.rawData)})
		if !r[1].IsNil() {
			err := r[1].Interface().(error)
			assert.Errorf(t, err, "index %d", i)
		} else {
			assert.Errorf(t, nil, "index %d", i)
		}
		field.ArraySize = 0

		field.DataLocSize = 4
		r = method.Call([]reflect.Value{reflect.ValueOf(tc.rawData)})
		if !r[1].IsNil() {
			err := r[1].Interface().(error)
			assert.Errorf(t, err, "index %d", i)
		} else {
			assert.Errorf(t, nil, "index %d", i)
		}
		field.DataLocSize = 0

		// This should succeed and return the expected value.
		r = method.Call([]reflect.Value{reflect.ValueOf(tc.rawData)})
		if r[1].IsNil() {
			assert.Equalf(t, tc.expected, r[0].Interface(), "index %d", i)
		} else {
			err := r[1].Interface().(error)
			assert.NoErrorf(t, err, "index %d", i)
		}
	}
}

func TestDecodeDataType(t *testing.T) {
	type testCase struct {
		dataType      expression.ValueType
		rawData       []byte
		expectedValue interface{}
		expectedErr   bool
	}
	testCases := []testCase{
		testCase{
			dataType:      expression.ValueTypeString,
			rawData:       nil,
			expectedValue: nil,
			expectedErr:   true,
		},
		testCase{
			dataType:      expression.ValueTypeSignedInt8,
			rawData:       []byte{8},
			expectedValue: int8(8),
			expectedErr:   false,
		},
		testCase{
			dataType:      expression.ValueTypeSignedInt16,
			rawData:       []byte{0x34, 0x12},
			expectedValue: int16(0x1234),
			expectedErr:   false,
		},
		testCase{
			dataType:      expression.ValueTypeSignedInt32,
			rawData:       []byte{0x11, 0x22, 0x33, 0x44},
			expectedValue: int32(0x44332211),
			expectedErr:   false,
		},
		testCase{
			dataType:      expression.ValueTypeSignedInt64,
			rawData:       []byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11},
			expectedValue: int64(0x1122334455667788),
			expectedErr:   false,
		},
		testCase{
			dataType:      expression.ValueTypeUnsignedInt8,
			rawData:       []byte{0x56},
			expectedValue: uint8(0x56),
			expectedErr:   false,
		},
		testCase{
			dataType:      expression.ValueTypeUnsignedInt16,
			rawData:       []byte{0x34, 0x12},
			expectedValue: uint16(0x1234),
			expectedErr:   false,
		},
		testCase{
			dataType:      expression.ValueTypeUnsignedInt32,
			rawData:       []byte{0x11, 0x22, 0x33, 0x44},
			expectedValue: uint32(0x44332211),
			expectedErr:   false,
		},
		testCase{
			dataType:      expression.ValueTypeUnsignedInt64,
			rawData:       []byte{0x12, 0x23, 0x34, 0x45, 0x56, 0x67, 0x78, 0x89},
			expectedValue: uint64(0x8978675645342312),
			expectedErr:   false,
		},
		testCase{
			dataType:      29384756,
			rawData:       nil,
			expectedValue: nil,
			expectedErr:   true,
		},
	}
	for _, tc := range testCases {
		actualValue, err := decodeDataType(tc.dataType, tc.rawData)
		if tc.expectedErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		assert.Equal(t, tc.expectedValue, actualValue)
	}
}

func TestDecodeRawData(t *testing.T) {
	rawData := []byte{
		0x20, 0x00, 0x06, 0x00, // name4
		0x26, 0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00, // name8
		0x11, 0x22, 0x33, 0x44, // pid
		0x01, 0x01, 0x01, 0x01, 0x02, 0x02, 0x02, 0x02, // args
		0x2c, 0x00, 0x04, 0x00, // foo
		0x30, 0x00, 0x04, 0x00, // bar

		'N', 'A', 'M', 'E', '4', 0,
		'N', 'A', 'M', 'E', '8', 0,
		0x11, 0x22, 0x33, 0x44,
		0x12, 0x34, 0x56, 0x78,
	}

	format := TraceEventFormat{
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
		"bar": TraceEventField{
			FieldName:    "bar",
			Offset:       28,
			DataType:     expression.ValueTypeSignedInt8,
			DataTypeSize: 1,
			DataLocSize:  4,
		},
	}

	e := expression.FieldValueMap{
		"name4": "NAME4",
		"name8": "NAME8",
		"pid":   int32(0x44332211),
		"args":  []interface{}{uint32(0x01010101), uint32(0x02020202)},
		"foo":   []uint8{0x11, 0x22, 0x33, 0x44},
		"bar":   []int8{0x12, 0x34, 0x56, 0x78},
	}

	data, err := format.DecodeRawData(rawData)
	assert.NoError(t, err)
	assert.Equal(t, e, data)

	format = TraceEventFormat{
		"error": TraceEventField{
			FieldName:   "error",
			DataLocSize: 16,
		},
	}
	_, err = format.DecodeRawData(rawData)
	assert.Error(t, err)

	format = TraceEventFormat{
		"error": TraceEventField{
			FieldName: "error",
			DataType:  expression.ValueTypeString,
		},
	}
	_, err = format.DecodeRawData(rawData)
	assert.Error(t, err)

	format = TraceEventFormat{
		"error": TraceEventField{
			FieldName: "error",
			DataType:  expression.ValueTypeString,
			ArraySize: 4,
		},
	}
	_, err = format.DecodeRawData(rawData)
	assert.Error(t, err)
}
