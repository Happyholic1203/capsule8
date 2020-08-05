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
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unsafe"

	"github.com/Happyholic1203/capsule8/pkg/expression"

	"github.com/golang/glog"
)

// TraceEventFormat represents the format of a kernel tracing event.
type TraceEventFormat map[string]TraceEventField

// TraceEventField represents a single field in a TraceEventFormat.
type TraceEventField struct {
	// FieldName is the name of the field.
	FieldName string
	// TypeName is the name of the field's type.
	TypeName string
	// Offset is the byte offset at which the field data begins.
	Offset int
	// Size is the number of bytes that make up the entirety of the field's
	// data.
	Size int
	// IsSigned is true if the field is signed; otherwise, it is false.
	IsSigned bool
	// DataType is the data type of this field.
	DataType expression.ValueType
	// DataTypeSize is the size of the data type. For arrays, this is the
	// size of each element. For scalars, this is the same as Size.
	DataTypeSize int
	// DataLocSize is the size of data location information, if present.
	// This is used for strings and dynamically sized arrays.
	DataLocSize int
	// If non-zero, this specifies the length of a fixed-size array, with
	// data present inline rather than using DataLocSize.
	ArraySize int
}

func (field *TraceEventField) setTypeFromSizeAndSign(isArray bool, arraySize int) bool {
	if isArray {
		if arraySize == -1 {
			// If this is an array of unknown size, we have to
			// skip it, because the field size is ambiguous
			return true
		}
		field.DataTypeSize = field.Size / arraySize
	} else {
		field.DataTypeSize = field.Size
	}

	switch field.DataTypeSize {
	case 1:
		if field.IsSigned {
			field.DataType = expression.ValueTypeSignedInt8
		} else {
			field.DataType = expression.ValueTypeUnsignedInt8
		}
	case 2:
		if field.IsSigned {
			field.DataType = expression.ValueTypeSignedInt16
		} else {
			field.DataType = expression.ValueTypeUnsignedInt16
		}
	case 4:
		if field.IsSigned {
			field.DataType = expression.ValueTypeSignedInt32
		} else {
			field.DataType = expression.ValueTypeUnsignedInt32
		}
	case 8:
		if field.IsSigned {
			field.DataType = expression.ValueTypeSignedInt64
		} else {
			field.DataType = expression.ValueTypeUnsignedInt64
		}
	default:
		// We can't figure out the type from the information given to
		// us. We're here likely because of a typedef name we didn't
		// recognize that's an array of integers or something. Skip it.
		return true
	}
	return false
}

func (field *TraceEventField) parseTypeName(s string, isArray bool, arraySize int) bool {
	if strings.HasPrefix(s, "const ") {
		s = s[6:]
	}

	switch s {
	// Standard C types
	case "bool":
		// "bool" is usually 1 byte, but it could be defined otherwise?
		return field.setTypeFromSizeAndSign(isArray, arraySize)

	// These types are going to be consistent in a 64-bit kernel, and in a
	// 32-bit kernel as well, except for "long".
	case "int", "signed int", "signed", "unsigned int", "unsigned", "uint":
		// The kernel is a bit unreliable about reporting "int" with
		// different sizes and signs, so try to use size/sign whenever
		// possible. If it's not possible, assume 32-bit int
		skip := field.setTypeFromSizeAndSign(isArray, arraySize)
		if skip {
			if field.IsSigned {
				field.DataType = expression.ValueTypeSignedInt32
			} else {
				field.DataType = expression.ValueTypeUnsignedInt32
			}
			field.DataTypeSize = 4
		}
		return false
	case "char", "signed char", "unsigned char":
		if field.IsSigned {
			field.DataType = expression.ValueTypeSignedInt8
		} else {
			field.DataType = expression.ValueTypeUnsignedInt8
		}
		field.DataTypeSize = 1
		return false
	case "short", "signed short", "unsigned short":
		if field.IsSigned {
			field.DataType = expression.ValueTypeSignedInt16
		} else {
			field.DataType = expression.ValueTypeUnsignedInt16
		}
		field.DataTypeSize = 2
		return false
	case "long", "signed long", "unsigned long":
		skip := field.setTypeFromSizeAndSign(isArray, arraySize)
		if skip {
			// Assume a 64-bit kernel
			if field.IsSigned {
				field.DataType = expression.ValueTypeSignedInt64
			} else {
				field.DataType = expression.ValueTypeUnsignedInt64
			}
			field.DataTypeSize = 8
		}
		return false
	case "long long", "signed long long", "unsigned long long":
		if field.IsSigned {
			field.DataType = expression.ValueTypeSignedInt64
		} else {
			field.DataType = expression.ValueTypeUnsignedInt64
		}
		field.DataTypeSize = 8
		return false

	// Fixed-size types
	case "s8", "__s8", "int8_t", "__int8_t":
		field.DataType = expression.ValueTypeSignedInt8
		field.DataTypeSize = 1
		return false
	case "u8", "__u8", "uint8_t", "__uint8_t":
		field.DataType = expression.ValueTypeUnsignedInt8
		field.DataTypeSize = 1
		return false
	case "s16", "__s16", "int16_t", "__int16_t":
		field.DataType = expression.ValueTypeSignedInt16
		field.DataTypeSize = 2
		return false
	case "u16", "__u16", "uint16_t", "__uint16_t":
		field.DataType = expression.ValueTypeUnsignedInt16
		field.DataTypeSize = 2
		return false
	case "s32", "__s32", "int32_t", "__int32_t":
		field.DataType = expression.ValueTypeSignedInt32
		field.DataTypeSize = 4
		return false
	case "u32", "__u32", "uint32_t", "__uint32_t":
		field.DataType = expression.ValueTypeUnsignedInt32
		field.DataTypeSize = 4
		return false
	case "s64", "__s64", "int64_t", "__int64_t":
		field.DataType = expression.ValueTypeSignedInt64
		field.DataTypeSize = 8
		return false
	case "u64", "__u64", "uint64_t", "__uint64_t":
		field.DataType = expression.ValueTypeUnsignedInt64
		field.DataTypeSize = 8
		return false

		/*
			// Known kernel typedefs in 4.10
			case "clockid_t", "pid_t", "xfs_extnum_t":
				field.DataType = expression.ValueTypeSignedInt32
				field.DataTypeSize = 4
			case "dev_t", "gfp_t", "gid_t", "isolate_mode_t", "tid_t", "uid_t",
				"ext4_lblk_t",
				"xfs_agblock_t", "xfs_agino_t", "xfs_agnumber_t", "xfs_btnum_t",
				"xfs_dahash_t", "xfs_exntst_t", "xfs_extlen_t", "xfs_lookup_t",
				"xfs_nlink_t", "xlog_tid_t":
				field.DataType = expression.ValueTypeUnsignedInt32
				field.DataTypeSize = 4
			case "loff_t", "xfs_daddr_t", "xfs_fsize_t", "xfs_lsn_t", "xfs_off_t":
				field.DataType = expression.ValueTypeSignedInt64
				field.DataTypeSize = 8
			case "aio_context_t", "blkcnt_t", "cap_user_data_t",
				"cap_user_header_t", "cputime_t", "dma_addr_t", "fl_owner_t",
				"gfn_t", "gpa_t", "gva_t", "ino_t", "key_serial_t", "key_t",
				"mqd_t", "off_t", "pgdval_t", "phys_addr_t", "pmdval_t",
				"pteval_t", "pudval_t", "qid_t", "resource_size_t", "sector_t",
				"timer_t", "umode_t",
				"ext4_fsblk_t",
				"xfs_ino_t", "xfs_fileoff_t", "xfs_fsblock_t", "xfs_filblks_t":
				field.DataType = expression.ValueTypeUnsignedInt64
				field.DataTypeSize = 8

			case "xen_mc_callback_fn_t":
				// This is presumably a pointer type
				return true

			case "uuid_be", "uuid_le":
				field.DataType = expression.ValueTypeUnsignedInt8
				field.DataTypeSize = 1
				field.arraySize = 16
				return false
		*/

	default:
		// Judging by Linux kernel conventions, it would appear that
		// any type name ending in _t is an integer type. Try to figure
		// it out from other information the kernel has given us. Note
		// that pointer types also fall into this category; however, we
		// have no way to know whether the value is to be treated as an
		// integer or a pointer unless we try to parse the printf fmt
		// string that's also included in the format description (no!)
		if strings.HasSuffix(s, "_t") {
			return field.setTypeFromSizeAndSign(isArray, arraySize)
		}
		if len(s) > 0 && s[len(s)-1] == '*' {
			return field.setTypeFromSizeAndSign(isArray, arraySize)
		}
		if strings.HasPrefix(s, "struct ") {
			// Skip structs
			return true
		}
		if strings.HasPrefix(s, "union ") {
			// Skip unions
			return true
		}
		if strings.HasPrefix(s, "enum ") {
			return field.setTypeFromSizeAndSign(isArray, arraySize)
		}
		// We don't recognize the type name. It's probably a typedef
		// for an integer or array of integers or something. Try to
		// figure it out from the size and sign information, but the
		// odds are not in our favor if we're here.
		return field.setTypeFromSizeAndSign(isArray, arraySize)
	}
}

var linuxArraySizeSanityWarning = false

func (field *TraceEventField) parseTypeAndName(s string) (bool, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "__data_loc") {
		s = s[11:]
		field.DataLocSize = field.Size

		// We have to use the type name here. The size information will
		// always indicate how big the data_loc information is, which
		// is normally 4 bytes (offset uint16, length uint16)

		x := strings.LastIndexFunc(s, unicode.IsSpace)
		field.FieldName = s[x+1:]

		s = strings.TrimSpace(s[:x])
		if !strings.HasSuffix(s, "[]") {
			return true, errors.New("Expected [] suffix on __data_loc type")
		}
		s = strings.TrimSpace(s[:len(s)-2])
		field.TypeName = s

		if s == "char" {
			field.DataType = expression.ValueTypeString
			field.DataTypeSize = 1
		} else if field.parseTypeName(s, true, -1) {
			return true, nil
		}
		return false, nil
	}

	arraySize := -1
	isArray := false
	if x := strings.IndexRune(s, '['); x != -1 {
		if x+1 >= len(s) {
			return true, errors.New("Closing ] missing")
		}
		if s[x+1] == ']' {
			return true, errors.New("Unexpected __data_loc without __data_loc prefix")
		}

		// Try to parse out the array size. Most of the time this will
		// be possible, but there are some cases where macros or consts
		// are used, so it's not possible.
		value, err := strconv.Atoi(s[x+1 : len(s)-1])
		if err == nil {
			arraySize = value
		}

		s = s[:x]
		isArray = true
	}

	if x := strings.LastIndexFunc(s, unicode.IsSpace); x != -1 {
		y := x + 1
		if y < len(s) && s[y] == '*' {
			y++
			for y < len(s) && s[y] == '*' {
				y++
			}
			x = y
		}
		field.TypeName = strings.TrimSpace(s[:x])
		field.FieldName = s[y:]
	}
	if field.FieldName == "" {
		return true, errors.New("Found type name without field name")
	}

	if field.parseTypeName(field.TypeName, isArray, arraySize) {
		return true, nil
	}
	if isArray {
		if arraySize >= 0 {
			field.ArraySize = arraySize

			// Sanity check what we've determined. Various versions
			// of the Linux kernel misreport size information.
			if arraySize != field.Size/field.DataTypeSize {
				if !linuxArraySizeSanityWarning {
					linuxArraySizeSanityWarning = true
					glog.Warning("Linux kernel tracepoint format size information is incorrect; compensating")
				}
				if field.parseTypeName(field.TypeName, true, -1) {
					// I'm pretty sure this isn't actually reachable
					return true, nil
				}
				field.ArraySize = field.Size / field.DataTypeSize
			}
		} else {
			field.ArraySize = field.Size / field.DataTypeSize
		}
	}

	return false, nil
}

func parseTraceEventField(line string) (field TraceEventField, err error) {
	var fieldString string

	fields := strings.Split(strings.TrimSpace(line), ";")
	for i := 0; i < len(fields); i++ {
		if fields[i] == "" {
			continue
		}
		parts := strings.Split(fields[i], ":")
		if len(parts) != 2 {
			err = errors.New("malformed format field")
			return
		}

		switch strings.TrimSpace(parts[0]) {
		case "field":
			fieldString = parts[1]
		case "offset":
			field.Offset, err = strconv.Atoi(parts[1])
		case "size":
			field.Size, err = strconv.Atoi(parts[1])
		case "signed":
			field.IsSigned, err = strconv.ParseBool(parts[1])
		}
		if err != nil {
			return
		}
	}

	skip, err := field.parseTypeAndName(fieldString)
	if err == nil && skip {
		// If a field is marked as skip, treat it as an array of bytes
		field.DataTypeSize = 1
		field.ArraySize = field.Size
		if field.IsSigned {
			field.DataType = expression.ValueTypeSignedInt8
		} else {
			field.DataType = expression.ValueTypeUnsignedInt8
		}
	}

	return
}

func getTraceEventFormat(tracingDir, name string) (uint16, TraceEventFormat, error) {
	filename := filepath.Join(tracingDir, "events", name, "format")
	file, err := os.OpenFile(filename, os.O_RDONLY, 0)
	if err != nil {
		return 0, nil, err
	}
	defer file.Close()

	return readTraceEventFormat(name, file)
}

func readTraceEventFormat(name string, reader io.Reader) (uint16, TraceEventFormat, error) {
	var (
		eventID  uint16
		inFormat bool
	)

	fields := make(TraceEventFormat)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		rawLine := scanner.Text()
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		switch {
		case inFormat:
			if !unicode.IsSpace(rune(rawLine[0])) {
				inFormat = false
				continue
			}
			if field, err := parseTraceEventField(line); err == nil {
				fields[field.FieldName] = field
			} else {
				glog.Infof("Couldn't parse trace event format: %v", err)
				return 0, nil, err
			}
		case strings.HasPrefix(line, "format:"):
			inFormat = true
		case strings.HasPrefix(line, "ID:"):
			value := strings.TrimSpace(line[3:])
			if parsedValue, err := strconv.Atoi(value); err == nil {
				eventID = uint16(parsedValue)
			} else {
				glog.Infof("Couldn't parse trace event ID: %v", err)
				return 0, nil, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, nil, err
	}

	return eventID, fields, nil
}

func (field *TraceEventField) typeMismatch(
	expectedType expression.ValueType,
) expression.FieldTypeMismatch {
	return expression.FieldTypeMismatch{
		Name:         field.FieldName,
		ExpectedType: expectedType,
		ActualType:   field.DataType,
	}
}

func (field *TraceEventField) dataOffsetAndLength(rawData []byte) (int, int, error) {
	var dataLength, dataOffset int
	switch field.DataLocSize {
	case 4:
		dataOffset = int(*(*uint16)(unsafe.Pointer(&rawData[field.Offset])))
		dataLength = int(*(*uint16)(unsafe.Pointer(&rawData[field.Offset+2])))
	case 8:
		dataOffset = int(*(*uint32)(unsafe.Pointer(&rawData[field.Offset])))
		dataLength = int(*(*uint32)(unsafe.Pointer(&rawData[field.Offset+4])))
	default:
		return 0, 0, fmt.Errorf("__data_loc size is neither 4 nor 8 (got %d)", field.DataLocSize)
	}
	return dataOffset, dataLength, nil
}

// Much of the following 9 methods is duplicated code, which sucks, but since Go
// does not have a preprocessor or generics and since we are desperately trying
// to avoid as many tiny, transient memory allocations as possible to reduce GC
// pressure, we want to avoid using the naked interface and return explicit
// types instead.

// DecodeString decodes a string field from raw data.
func (field *TraceEventField) DecodeString(rawData []byte) (string, error) {
	if field.DataType != expression.ValueTypeString {
		return "", field.typeMismatch(expression.ValueTypeString)
	}
	dataOffset, dataLength, err := field.dataOffsetAndLength(rawData)
	if err != nil {
		return "", err
	}
	if dataLength > 0 && rawData[dataOffset+dataLength-1] == 0 {
		dataLength--
	}
	return string(rawData[dataOffset : dataOffset+dataLength]), nil
}

// DecodeSignedInt8 decodes a signed 8-bit integer field from raw data.
func (field *TraceEventField) DecodeSignedInt8(rawData []byte) (int8, error) {
	if field.DataType != expression.ValueTypeSignedInt8 {
		return 0, field.typeMismatch(expression.ValueTypeSignedInt8)
	}
	if field.ArraySize != 0 || field.DataLocSize != 0 {
		// XXX This isn't going to give us precisely what we want for
		// an error message, because the actual is the right base type;
		// it's just a vector rather than a scalar. Since the expression
		// package has no notion of an array, it's not possible to
		// represent this in the FieldTypeMismatch error.
		return 0, field.typeMismatch(expression.ValueTypeSignedInt8)
	}
	return int8(rawData[field.Offset]), nil
}

// DecodeSignedInt16 decodes a signed 16-bit integer field from raw data.
func (field *TraceEventField) DecodeSignedInt16(rawData []byte) (int16, error) {
	if field.DataType != expression.ValueTypeSignedInt16 {
		return 0, field.typeMismatch(expression.ValueTypeSignedInt16)
	}
	if field.ArraySize != 0 || field.DataLocSize != 0 {
		// XXX This isn't going to give us precisely what we want for
		// an error message, because the actual is the right base type;
		// it's just a vector rather than a scalar. Since the expression
		// package has no notion of an array, it's not possible to
		// represent this in the FieldTypeMismatch error.
		return 0, field.typeMismatch(expression.ValueTypeSignedInt16)
	}
	return *(*int16)(unsafe.Pointer(&rawData[field.Offset])), nil
}

// DecodeSignedInt32 decodes a signed 32-bit integer field from raw data.
func (field *TraceEventField) DecodeSignedInt32(rawData []byte) (int32, error) {
	if field.DataType != expression.ValueTypeSignedInt32 {
		return 0, field.typeMismatch(expression.ValueTypeSignedInt32)
	}
	if field.ArraySize != 0 || field.DataLocSize != 0 {
		// XXX This isn't going to give us precisely what we want for
		// an error message, because the actual is the right base type;
		// it's just a vector rather than a scalar. Since the expression
		// package has no notion of an array, it's not possible to
		// represent this in the FieldTypeMismatch error.
		return 0, field.typeMismatch(expression.ValueTypeSignedInt32)
	}
	return *(*int32)(unsafe.Pointer(&rawData[field.Offset])), nil
}

// DecodeSignedInt64 decodes a signed 64-bit integer field from raw data.
func (field *TraceEventField) DecodeSignedInt64(rawData []byte) (int64, error) {
	if field.DataType != expression.ValueTypeSignedInt64 {
		return 0, field.typeMismatch(expression.ValueTypeSignedInt64)
	}
	if field.ArraySize != 0 || field.DataLocSize != 0 {
		// XXX This isn't going to give us precisely what we want for
		// an error message, because the actual is the right base type;
		// it's just a vector rather than a scalar. Since the expression
		// package has no notion of an array, it's not possible to
		// represent this in the FieldTypeMismatch error.
		return 0, field.typeMismatch(expression.ValueTypeSignedInt64)
	}
	return *(*int64)(unsafe.Pointer(&rawData[field.Offset])), nil
}

// DecodeUnsignedInt8 decodes an unsigned 8-bit integer field from raw data.
func (field *TraceEventField) DecodeUnsignedInt8(rawData []byte) (uint8, error) {
	if field.DataType != expression.ValueTypeUnsignedInt8 {
		return 0, field.typeMismatch(expression.ValueTypeUnsignedInt8)
	}
	if field.ArraySize != 0 || field.DataLocSize != 0 {
		// XXX This isn't going to give us precisely what we want for
		// an error message, because the actual is the right base type;
		// it's just a vector rather than a scalar. Since the expression
		// package has no notion of an array, it's not possible to
		// represent this in the FieldTypeMismatch error.
		return 0, field.typeMismatch(expression.ValueTypeUnsignedInt8)
	}
	return uint8(rawData[field.Offset]), nil
}

// DecodeUnsignedInt16 decodes an unsigned 16-bit integer field from raw data.
func (field *TraceEventField) DecodeUnsignedInt16(rawData []byte) (uint16, error) {
	if field.DataType != expression.ValueTypeUnsignedInt16 {
		return 0, field.typeMismatch(expression.ValueTypeUnsignedInt16)
	}
	if field.ArraySize != 0 || field.DataLocSize != 0 {
		// XXX This isn't going to give us precisely what we want for
		// an error message, because the actual is the right base type;
		// it's just a vector rather than a scalar. Since the expression
		// package has no notion of an array, it's not possible to
		// represent this in the FieldTypeMismatch error.
		return 0, field.typeMismatch(expression.ValueTypeUnsignedInt16)
	}
	return *(*uint16)(unsafe.Pointer(&rawData[field.Offset])), nil
}

// DecodeUnsignedInt32 decodes an unsigned 32-bit integer field from raw data.
func (field *TraceEventField) DecodeUnsignedInt32(rawData []byte) (uint32, error) {
	if field.DataType != expression.ValueTypeUnsignedInt32 {
		return 0, field.typeMismatch(expression.ValueTypeUnsignedInt32)
	}
	if field.ArraySize != 0 || field.DataLocSize != 0 {
		// XXX This isn't going to give us precisely what we want for
		// an error message, because the actual is the right base type;
		// it's just a vector rather than a scalar. Since the expression
		// package has no notion of an array, it's not possible to
		// represent this in the FieldTypeMismatch error.
		return 0, field.typeMismatch(expression.ValueTypeUnsignedInt32)
	}
	return *(*uint32)(unsafe.Pointer(&rawData[field.Offset])), nil
}

// DecodeUnsignedInt64 decodes an unsigned 64-bit integer field from raw data.
func (field *TraceEventField) DecodeUnsignedInt64(rawData []byte) (uint64, error) {
	if field.DataType != expression.ValueTypeUnsignedInt64 {
		return 0, field.typeMismatch(expression.ValueTypeUnsignedInt64)
	}
	if field.ArraySize != 0 || field.DataLocSize != 0 {
		// XXX This isn't going to give us precisely what we want for
		// an error message, because the actual is the right base type;
		// it's just a vector rather than a scalar. Since the expression
		// package has no notion of an array, it's not possible to
		// represent this in the FieldTypeMismatch error.
		return 0, field.typeMismatch(expression.ValueTypeUnsignedInt64)
	}
	return *(*uint64)(unsafe.Pointer(&rawData[field.Offset])), nil
}

func decodeDataType(dataType expression.ValueType, rawData []byte) (interface{}, error) {
	switch dataType {
	case expression.ValueTypeString:
		return nil, errors.New("internal error; got unexpected expression.ValueTypeString")
	case expression.ValueTypeSignedInt8:
		return int8(rawData[0]), nil
	case expression.ValueTypeSignedInt16:
		return *(*int16)(unsafe.Pointer(&rawData[0])), nil
	case expression.ValueTypeSignedInt32:
		return *(*int32)(unsafe.Pointer(&rawData[0])), nil
	case expression.ValueTypeSignedInt64:
		return *(*int64)(unsafe.Pointer(&rawData[0])), nil
	case expression.ValueTypeUnsignedInt8:
		return uint8(rawData[0]), nil
	case expression.ValueTypeUnsignedInt16:
		return *(*uint16)(unsafe.Pointer(&rawData[0])), nil
	case expression.ValueTypeUnsignedInt32:
		return *(*uint32)(unsafe.Pointer(&rawData[0])), nil
	case expression.ValueTypeUnsignedInt64:
		return *(*uint64)(unsafe.Pointer(&rawData[0])), nil
	}
	return nil, errors.New("internal error; undefined dataType")
}

// DecodeRawData decodes a field from raw data.
func (field *TraceEventField) DecodeRawData(rawData []byte) (interface{}, error) {
	var arraySize, dataLength, dataOffset int
	var err error

	if field.DataLocSize > 0 {
		dataOffset, dataLength, err = field.dataOffsetAndLength(rawData)
		if err != nil {
			return nil, err
		}

		if field.DataType == expression.ValueTypeString {
			if dataLength > 0 && rawData[dataOffset+dataLength-1] == 0 {
				dataLength--
			}
			return string(rawData[dataOffset : dataOffset+dataLength]), nil
		}
		arraySize = dataLength / field.DataTypeSize
	} else if field.ArraySize == 0 {
		return decodeDataType(field.DataType, rawData[field.Offset:])
	} else {
		arraySize = field.ArraySize
		dataOffset = field.Offset
		dataLength = arraySize * field.DataTypeSize
	}

	switch field.DataType {
	case expression.ValueTypeSignedInt8:
		array := make([]int8, arraySize)
		var slice = struct {
			addr     uintptr
			len, cap int
		}{uintptr(unsafe.Pointer(&rawData[dataOffset])), arraySize, arraySize}
		copy(array, *(*[]int8)(unsafe.Pointer(&slice)))
		return array, nil
	case expression.ValueTypeUnsignedInt8:
		array := make([]uint8, arraySize)
		copy(array, rawData[dataOffset:])
		return array, nil
	default:
		array := make([]interface{}, arraySize)
		for i := 0; i < arraySize; i++ {
			array[i], err = decodeDataType(field.DataType, rawData[dataOffset:])
			if err != nil {
				return nil, err
			}
			dataOffset += field.DataTypeSize
		}
		return array, nil
	}
}

// DecodeRawData decodes a buffer of raw bytes according to the kernel defined
// format that has been parsed from the tracing filesystem.
func (f TraceEventFormat) DecodeRawData(rawData []byte) (expression.FieldValueMap, error) {
	data := make(expression.FieldValueMap)
	for _, field := range f {
		var err error
		data[field.FieldName], err = field.DecodeRawData(rawData)
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}
