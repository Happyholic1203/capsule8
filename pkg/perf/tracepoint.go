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

	"github.com/capsule8/reactive8/pkg/config"
	"github.com/golang/glog"
)

const (
	dtString int = iota
	dtS8
	dtS16
	dtS32
	dtS64
	dtU8
	dtU16
	dtU32
	dtU64
)

type TraceEventField struct {
	FieldName string
	TypeName  string
	Offset    int
	Size      int
	IsSigned  bool

	dataType     int // data type constant from above
	dataTypeSize int
	dataLocSize  int
	arraySize    int // 0 == [] array, >0 == # elements
}

func getTraceFs() string {
	return config.Sensor.TraceFs
}

func AddKprobe(name string, address string, onReturn bool, output string) error {
	filename := filepath.Join(getTraceFs(), "kprobe_events")
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer file.Close()

	var cmd string
	if onReturn {
		cmd = fmt.Sprintf("r:%s %s %s", name, address, output)
	} else {
		cmd = fmt.Sprintf("p:%s %s %s", name, address, output)
	}
	_, err = file.Write([]byte(cmd))
	if err != nil {
		return err
	}

	return nil
}

func RemoveKprobe(name string) error {
	filename := filepath.Join(getTraceFs(), "kprobe_events")
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer file.Close()

	cmd := fmt.Sprintf("-:%s", name)
	_, err = file.Write([]byte(cmd))
	if err != nil {
		return err
	}

	return nil
}

func GetAvailableTraceEvents() ([]string, error) {
	var events []string

	filename := filepath.Join(getTraceFs(), "available_events")
	file, err := os.OpenFile(filename, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		events = append(events, scanner.Text())
	}
	err = scanner.Err()
	if err != nil {
		return nil, err
	}

	return events, nil
}

func GetTraceEventID(name string) (uint16, error) {
	filename := filepath.Join(getTraceFs(), "events", name, "id")
	file, err := os.OpenFile(filename, os.O_RDONLY, 0)
	if err != nil {
		glog.Infof("Couldn't open trace event %s: %v",
			filename, err)
		return 0, err
	}
	defer file.Close()

	return ReadTraceEventID(name, file)
}

func ReadTraceEventID(name string, reader io.Reader) (uint16, error) {
	//
	// The tracepoint id is a uint16, so we can assume it'll be
	// no longer than 5 characters plus a newline.
	//
	var buf [6]byte
	_, err := reader.Read(buf[:])
	if err != nil {
		glog.Infof("Couldn't read trace event id: %v", err)
		return 0, err
	}

	idStr := strings.TrimRight(string(buf[:]), "\n\x00")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		glog.Infof("Couldn't parse trace event id %s: %v",
			string(buf[:]), err)
		return 0, err
	}

	return uint16(id), nil
}

func (field *TraceEventField) setTypeFromSizeAndSign(isArray bool, arraySize int) (bool, error) {
	if isArray  {
		if arraySize == -1 {
			// If this is an array of unknown size, we have to
			// skip it, because the field size is ambiguous
			return true, nil
		}
		field.dataTypeSize = field.Size / arraySize
	} else {
		field.dataTypeSize = field.Size
	}

	switch field.dataTypeSize {
	case 1:
		if field.IsSigned {
			field.dataType = dtS8
		} else {
			field.dataType = dtU8
		}
	case 2:
		if field.IsSigned {
			field.dataType = dtS16
		} else {
			field.dataType = dtU16
		}
	case 4:
		if field.IsSigned {
			field.dataType = dtS32
		} else {
			field.dataType = dtU32
		}
	case 8:
		if field.IsSigned {
			field.dataType = dtS64
		} else {
			field.dataType = dtU64
		}
	default:
		// We can't figure out the type from the information given to
		// us. We're here likely because of a typedef name we didn't
		// recognize that's an array of integers or something. Skip it.
		return true, nil
	}
	return false, nil
}

func (field *TraceEventField) parseTypeName(s string, isArray bool, arraySize int) (bool, error) {
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
		if field.IsSigned {
			field.dataType = dtS32
		} else {
			field.dataType = dtU32
		}
		field.dataTypeSize = 4
		return false, nil
	case "char", "signed char", "unsigned char":
		if field.IsSigned {
			field.dataType = dtS8
		} else {
			field.dataType = dtU8
		}
		field.dataTypeSize = 1
		return false, nil
	case "short", "signed short", "unsigned short":
		if field.IsSigned {
			field.dataType = dtS16
		} else {
			field.dataType = dtU16
		}
		field.dataTypeSize = 2
		return false, nil
	case "long", "signed long", "unsigned long":
		skip, err := field.setTypeFromSizeAndSign(isArray, arraySize)
		if skip && err == nil {
			// Assume a 64-bit kernel
			if field.IsSigned {
				field.dataType = dtS64
			} else {
				field.dataType = dtU64
			}
			field.dataTypeSize = 8
			return false, nil
		}
		return skip, err
	case "long long", "signed long long", "unsigned long long":
		if field.IsSigned {
			field.dataType = dtS64
		} else {
			field.dataType = dtU64
		}
		field.dataTypeSize = 8
		return false, nil

	// Fixed-size types
	case "s8", "__s8", "int8_t", "__int8_t":
		field.dataType = dtS8
		field.dataTypeSize = 1
		return false, nil
	case "u8", "__u8", "uint8_t", "__uint8_t":
		field.dataType = dtS16
		field.dataTypeSize = 1
		return false, nil
	case "s16", "__s16", "int16_t", "__int16_t":
		field.dataType = dtS16
		field.dataTypeSize = 2
		return false, nil
	case "u16", "__u16", "uint16_t", "__uint16_t":
		field.dataType = dtU16
		field.dataTypeSize = 2
		return false, nil
	case "s32", "__s32", "int32_t", "__int32_t":
		field.dataType = dtS32
		field.dataTypeSize = 4
		return false, nil
	case "u32", "__u32", "uint32_t", "__uint32_t":
		field.dataType = dtU32
		field.dataTypeSize = 4
		return false, nil
	case "s64", "__s64", "int64_t", "__int64_t":
		field.dataType = dtS64
		field.dataTypeSize = 8
		return false, nil
	case "u64", "__u64", "uint64_t", "__uint64_t":
		field.dataType = dtU64
		field.dataTypeSize = 8
		return false, nil

/*
	// Known kernel typedefs in 4.10
	case "clockid_t", "pid_t", "xfs_extnum_t":
		field.dataType = dtS32
		field.dataTypeSize = 4
	case "dev_t", "gfp_t", "gid_t", "isolate_mode_t", "tid_t", "uid_t",
		"ext4_lblk_t",
		"xfs_agblock_t", "xfs_agino_t", "xfs_agnumber_t", "xfs_btnum_t",
		"xfs_dahash_t", "xfs_exntst_t", "xfs_extlen_t", "xfs_lookup_t",
		"xfs_nlink_t", "xlog_tid_t":
		field.dataType = dtU32
		field.dataTypeSize = 4
	case "loff_t", "xfs_daddr_t", "xfs_fsize_t", "xfs_lsn_t", "xfs_off_t":
		field.dataType = dtS64
		field.dataTypeSize = 8
	case "aio_context_t", "blkcnt_t", "cap_user_data_t",
		"cap_user_header_t", "cputime_t", "dma_addr_t", "fl_owner_t",
		"gfn_t", "gpa_t", "gva_t", "ino_t", "key_serial_t", "key_t",
		"mqd_t", "off_t", "pgdval_t", "phys_addr_t", "pmdval_t",
		"pteval_t", "pudval_t", "qid_t", "resource_size_t", "sector_t",
		"timer_t", "umode_t",
		"ext4_fsblk_t",
		"xfs_ino_t", "xfs_fileoff_t", "xfs_fsblock_t", "xfs_filblks_t":
		field.dataType = dtU64
		field.dataTypeSize = 8

	case "xen_mc_callback_fn_t":
		// This is presumably a pointer type
		return true, nil

	case "uuid_be", "uuid_le":
		field.dataType = dtU8
		field.dataTypeSize = 1
		field.arraySize = 16
		return false, nil
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
			return true, nil
		}
		if strings.HasPrefix(s, "union ") {
			// Skip unions
			return true, nil
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

func (field *TraceEventField) parseTypeAndName(s string) (bool, error) {
	if strings.HasPrefix(s, "__data_loc") {
		s = s[11:]
		field.dataLocSize = field.Size

		// We have to use the type name here. The size information will
		// always indicate how big the data_loc information is, which
		// is normally 4 bytes (offset uint16, length uint16)

		x := strings.LastIndexFunc(s, unicode.IsSpace)
		field.FieldName = string(strings.TrimSpace(s[x+1:]))

		s = s[:x]
		if !strings.HasSuffix(s, "[]") {
			return true, errors.New("Expected [] suffix on __data_loc type")
		}
		s = s[:len(s)-2]
		field.TypeName = string(s)

		if s == "char" {
			field.dataType = dtString
			field.dataTypeSize = 1
		} else {
			skip, err := field.parseTypeName(s, true, -1)
			if err != nil {
				return true, err
			}
			if skip {
				return true, nil
			}
		}
		return false, nil
	}

	arraySize := -1
	isArray := false
	x := strings.IndexRune(s, '[')
	if x != -1 {
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

	x = strings.LastIndexFunc(s, unicode.IsSpace)
	field.TypeName = string(s[:x])
	field.FieldName = string(strings.TrimSpace(s[x+1:]))

	skip, err := field.parseTypeName(field.TypeName, isArray, arraySize)
	if err != nil {
		return true, err
	}
	if skip {
		return true, nil
	}
	if isArray {
		if arraySize >= 0 {
			field.arraySize = arraySize
		} else {
			field.arraySize = field.Size / field.dataTypeSize
		}
	}

	return false, nil
}

func parseTraceEventField(line string) (*TraceEventField, error) {
	var err error
	var fieldString string

	field := &TraceEventField{}
	fields := strings.Split(strings.TrimSpace(line), ";")
	for i := 0; i < len(fields); i++ {
		if fields[i] == "" {
			continue
		}
		parts := strings.Split(fields[i], ":")
		if len(parts) != 2 {
			return nil, errors.New("malformed format field")
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
			return nil, err
		}
	}

	skip, err := field.parseTypeAndName(fieldString)
	if err != nil {
		return nil, err
	}
	if skip {
		// If a field is marked as skip, treat it as an array of bytes
		field.dataTypeSize = 1
		field.arraySize = field.Size
		if field.IsSigned {
			field.dataType = dtS8
		} else {
			field.dataType = dtU8
		}
	}
	return field, nil
}

func GetTraceEventFormat(name string) (uint16, map[string]TraceEventField, error) {
	filename := filepath.Join(getTraceFs(), "events", name, "format")
	file, err := os.OpenFile(filename, os.O_RDONLY, 0)
	if err != nil {
		glog.Infof("Couldn't open trace event %s: %v",
			filename, err)
		return 0, nil, err
	}
	defer file.Close()

	return ReadTraceEventFormat(name, file)
}

func ReadTraceEventFormat(name string, reader io.Reader) (uint16, map[string]TraceEventField, error) {
	var eventID uint16

	inFormat := false
	fields := make(map[string]TraceEventField)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		rawLine := scanner.Text()
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		if inFormat {
			if !unicode.IsSpace(rune(rawLine[0])) {
				inFormat = false
				continue
			}
			field, err := parseTraceEventField(line)
			if err != nil {
				glog.Infof("Couldn't parse trace event format: %v", err)
				return 0, nil, err
			}
			if field != nil {
				fields[field.FieldName] = *field
			}
		} else if strings.HasPrefix(line, "format:") {
			inFormat = true
		} else if strings.HasPrefix(line, "ID:") {
			value := strings.TrimSpace(line[3:])
			parsedValue, err := strconv.Atoi(value)
			if err != nil {
				glog.Infof("Couldn't parse trace event ID: %v", err)
				return 0, nil, err
			}
			eventID = uint16(parsedValue)
		}
	}
	err := scanner.Err()
	if err != nil {
		return 0, nil, err
	}

	return eventID, fields, err
}
