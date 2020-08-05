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

package procfs

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/Happyholic1203/capsule8/pkg/sys"
	"github.com/Happyholic1203/capsule8/pkg/sys/proc"

	"github.com/golang/glog"
)

// SelfTGID returns the TGID of the calling task.
func (fs *FileSystem) SelfTGID() int {
	fs.selfOnce.Do(func() {
		filename := fmt.Sprintf("%s/self/stat", fs.MountPoint)
		dataBytes, err := ioutil.ReadFile(filename)
		if err != nil {
			glog.Fatalf("Cannot read %s: %v", filename, err)
		}
		f := strings.Fields(string(dataBytes))
		v, err := strconv.ParseUint(f[0], 10, 32)
		if err != nil {
			glog.Fatalf("Cannot parse %q: %v", f[0], err)
		}
		fs.selfTGID = int(v)
	})
	return fs.selfTGID
}

// ProcessContainerID returns the container ID running the specified process.
// If the process is not running inside of a container, the return will be the
// empty string.
func (fs *FileSystem) ProcessContainerID(pid int) (string, error) {
	cgroups, err := fs.TaskControlGroups(pid, pid)
	if err != nil {
		return "", err
	}

	for _, cg := range cgroups {
		if id := sys.ContainerID(cg.Path); id != "" {
			return id, nil
		}
	}

	return "", nil
}

// ProcessExecutable returns the name of the executable from exe file
func (fs *FileSystem) ProcessExecutable(pid int) (string, error) {
	exeFile := fmt.Sprintf("%s/%d/exe", fs.MountPoint, pid)
	exePath, err := os.Readlink(exeFile)
	if err != nil {
		return "", err
	}

	path := string(exePath)
	return path, nil
}

// ProcessCommandLine returns the full command-line arguments for the process
// indicated by the given PID.
func (fs *FileSystem) ProcessCommandLine(pid int) ([]string, error) {
	filename := fmt.Sprintf("%d/cmdline", pid)
	cmdline, err := fs.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var commandLine []string
	reader := bufio.NewReader(bytes.NewReader(cmdline[:]))
	for {
		var s string
		s, err = reader.ReadString(0)
		if err != nil || len(s) <= 1 {
			break
		}

		if len(s) > 1 {
			commandLine = append(commandLine, s[:len(s)-1])
		}
	}

	return commandLine, nil
}

// ProcessMappings returns the memory mappings a process currently has
func (fs *FileSystem) ProcessMappings(pid int) ([]proc.MemoryMapping, error) {
	filename := fmt.Sprintf("%d/maps", pid)
	mapsFile, err := fs.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var maps []proc.MemoryMapping
	reader := bufio.NewReader(bytes.NewReader(mapsFile[:]))
	for {
		var s string
		var start, end uint64

		s, err = reader.ReadString('\n')
		if err != nil {
			break
		}

		// Limit to 6 fields, the last being the path which is optional.
		// We use SplitN instead of Fields here so that the path doesn't
		// get broken up if it contains spaces.
		fields := strings.SplitN(s, " ", 6)
		if len(fields) < 5 {
			glog.Warningf("Couldn't parse maps line: %s", s)
			continue
		}

		addrs := strings.Split(fields[0], "-")
		if len(addrs) != 2 {
			glog.Warningf("Couldn't parse addrs: %s", fields[0])
			continue
		}

		start, err = strconv.ParseUint(addrs[0], 16, 64)
		if err != nil {
			glog.Warningf("Couldn't parse addr: %s", addrs[0])
			continue
		}

		end, err = strconv.ParseUint(addrs[1], 16, 64)
		if err != nil {
			glog.Warningf("Couldn't parse addr: %s", addrs[1])
			continue
		}

		path := strings.TrimSpace(fields[5])

		maps = append(maps, proc.MemoryMapping{
			Start: start,
			End:   end,
			Path:  path,
		})
	}

	return maps, nil
}

// TaskControlGroups returns the cgroup membership of the specified task.
func (fs *FileSystem) TaskControlGroups(tgid, pid int) ([]proc.ControlGroup, error) {
	filename := fmt.Sprintf("%d/task/%d/cgroup", tgid, pid)
	data, err := fs.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var cgroups []proc.ControlGroup

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		t := scanner.Text()
		parts := strings.Split(t, ":")
		var ID int
		ID, err = strconv.Atoi(parts[0])
		if err != nil {
			glog.Warningf("Couldn't parse cgroup line: %s", t)
			continue
		}

		c := proc.ControlGroup{
			ID:          ID,
			Controllers: strings.Split(parts[1], ","),
			Path:        parts[2],
		}

		cgroups = append(cgroups, c)
	}

	return cgroups, nil
}

// TaskCWD returns the current working directory for the specified task.
func (fs *FileSystem) TaskCWD(tgid, pid int) (string, error) {
	return os.Readlink(fmt.Sprintf("%s/%d/task/%d/cwd",
		fs.MountPoint, tgid, pid))
}

// TaskStartTime returns the time at which the specified task started.
func (fs *FileSystem) TaskStartTime(tgid, pid int) (int64, error) {
	filename := fmt.Sprintf("%d/task/%d/stat", tgid, pid)
	b, err := fs.ReadFile(filename)
	if err != nil {
		return 0, err
	}
	data := string(b)

	// This requires special care because the command can contain white space
	// and / or punctuation. Fortunately, we are guaranteed that the command
	// will always be between the first '(' and the last ')'.
	firstLParen := strings.IndexByte(data, '(')
	lastRParen := strings.LastIndexByte(data, ')')
	if firstLParen < 0 || lastRParen < 0 || lastRParen < firstLParen {
		return 0, nil
	}
	//command := stat[firstLParen+1 : lastRParen]

	//statFields := strings.Fields(stat[:firstLParen])
	//statFields = append(statFields, command)
	//statFields = append(statFields, strings.Fields(stat[lastRParen+1:])...)

	// It looks like kernel versions older than 3.4 is index position 18
	// for the process start time, while 3.4 and newer ones use index
	// position 19. Luckily, when 19 is used 18 is always 0. Since the
	// start time should never be 0, this will tell us whether we've got an
	// older kernel or a newer one.
	fields := strings.Fields(data[lastRParen+1:])
	i, err := strconv.ParseInt(fields[18], 0, 64)
	if err != nil {
		return 0, err
	}
	if i == 0 {
		i, err = strconv.ParseInt(fields[19], 0, 64)
		if err != nil {
			return 0, err
		}
	}
	return i, nil
}

// TaskUniqueID returns a unique task ID for a PID.
func (fs *FileSystem) TaskUniqueID(tgid, pid int, startTime int64) string {
	// Do not use TGID here, because the TGID for a PID can change. The
	// argument is included for consistency in naming conventions.

	// Hash the bootID, PID, and start time to create a unique process
	// identifier that can also be calculated from perf records and trace
	// events
	return fmt.Sprintf("%s-%d-%d", fs.BootID(), pid, startTime)
}

// WalkTasks calls the specified function for each task present in the proc
// FileSystem. If the walk function returns false, the walk will be aborted.
func (fs *FileSystem) WalkTasks(walkFunc proc.TaskWalkFunc) error {
	glog.V(1).Infof("Scanning %s for existing tasks", fs.MountPoint)
	d, err := os.Open(fs.MountPoint)
	if err != nil {
		return fmt.Errorf("Cannot open %q: %v", fs.MountPoint, err)
	}
	procNames, err := d.Readdirnames(0)
	if err != nil {
		d.Close()
		return fmt.Errorf("Cannot read directory names from %q: %v",
			fs.MountPoint, err)
	}
	d.Close()

	for _, procName := range procNames {
		var i int64
		i, err = strconv.ParseInt(procName, 10, 32)
		if err != nil {
			continue
		}
		tgid := int(i)

		taskPath := filepath.Join(fs.MountPoint, procName, "task")
		d, err = os.Open(taskPath)
		if err != nil {
			// This is not fatal; the process may have gone away
			continue
		}
		var taskNames []string
		taskNames, err = d.Readdirnames(0)
		if err != nil {
			// This is not fatal; the process may have gone away
			d.Close()
			continue
		}
		d.Close()

		for _, taskName := range taskNames {
			i, err = strconv.ParseInt(taskName, 10, 32)
			if err != nil {
				continue
			}
			pid := int(i)

			if !walkFunc(tgid, pid) {
				return nil
			}
		}
	}

	return nil
}

// ReadTaskStatus reads the status of a task, storing the information into the
// supplied struct. The supplied struct must be a pointer.
func (fs *FileSystem) ReadTaskStatus(tgid, pid int, i interface{}) error {
	filename := fmt.Sprintf("%d/task/%d/status", tgid, pid)
	f, err := os.Open(filepath.Join(fs.MountPoint, filename))
	if err != nil {
		return err
	}
	defer f.Close()

	v := reflect.ValueOf(i)
	if v.Kind() != reflect.Ptr {
		return errors.New("Destination must be a pointer to struct")
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return errors.New("Destination pointer must be to a struct")
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			field := findFieldByTag(v, parts[0])
			if !field.IsValid() {
				continue
			}
			err = setValueFromString(field, parts[0],
				strings.TrimSpace(parts[1]))
			if err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func findFieldByTag(v reflect.Value, name string) reflect.Value {
	t := v.Type()
	for i := t.NumField() - 1; i >= 0; i-- {
		f := t.Field(i)
		if f.Tag == reflect.StructTag(name) {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

func setValueFromString(v reflect.Value, name, s string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if x, err := strconv.ParseInt(s, 0, 64); err == nil {
			v.SetInt(x)
		} else {
			return err
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64:
		if x, err := strconv.ParseUint(s, 0, 64); err == nil {
			v.SetUint(x)
		} else {
			return err
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Slice {
			return fmt.Errorf("Nested arrays are unsupported (%s)", name)
		}
		l := strings.Fields(s)
		a := reflect.MakeSlice(v.Type(), len(l), len(l))
		for i, x := range l {
			n := fmt.Sprintf("%s[%d]", name, i)
			if err := setValueFromString(a.Index(i), n, x); err != nil {
				return err
			}
		}
		v.Set(a)
	default:
		return fmt.Errorf("Cannot set field %s", name)
	}

	return nil
}
