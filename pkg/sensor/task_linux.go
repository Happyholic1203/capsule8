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

package sensor

import (
	"runtime"
	"sync"
	"syscall"

	"github.com/Happyholic1203/capsule8/pkg/sys/proc"
)

// Return the kernel's notion of pid, which is the current task id
func getpid() int32 {
	r1, _, errno := syscall.Syscall(syscall.SYS_GETTID, 0, 0, 0)
	if errno != 0 {
		return -1
	}
	return int32(r1)
}

// Return the kernel's notion of tgid, which is the current task's thread group
// leader's id. This is equivalent to os.Getpid(), but do it ourselves directly
// with the kernel so that we avoid any potential Go shenanigans, though I
// don't think there are any. Just to be safe for all time ...
func gettgid() int32 {
	r1, _, errno := syscall.Syscall(syscall.SYS_GETPID, 0, 0, 0)
	if errno != 0 {
		return -1
	}
	return int32(r1)
}

// Report pid, tgid, and startTime for a task, ensuring that the task has
// differing values for pid and tgid so that they can be independently
// verified. Go plays all sorts of games under the covers to implement its
// own scheduler, which is great and all, but it makes this a little trickier.
// Luckily the runtime provides functions to lock goroutines (G) to kernel
// tasks (M), and when the Go scheduler runs out of tasks (M) to run goroutines
// (G), it creates more tasks (M). So we just keep forcing new goroutines to be
// created while locking them to a task until we get a goroutine that has
// different pid and tgid, at which point we use proc.FileSystem.TaskStartTime
// to get the task's start time, which also triggers the kprobe that has been
// installed to get the same information from task struct using the offsets
// that we are trying to verify.
//
// This code is clearly abusive, but it saves us the need to execute an
// external program written in C that does the whole thing much more easily.
// Unfortunately doing that introduces whole bunches of security issues and
// other complications.
func reportTaskStructFields(
	fs proc.FileSystem,
) (pid int32, tgid int32, startTime int64) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Don't let this little game get too far out of hand. Limit the number
	// of goroutines we'll try to spawn to get what we want. This usually
	// works on the first attempt, so if it doesn't go after a few tries,
	// it isn't going to.
	const maxAttempts = 4

	var (
		attempt int
		f       func(func())
		wg      sync.WaitGroup
	)

	f = func(ff func()) {
		defer ff()
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		tgid = gettgid()
		pid = getpid()
		if pid != tgid {
			startTime, _ = fs.TaskStartTime(int(tgid), int(pid))
		} else {
			attempt++
			if attempt < maxAttempts {
				var w sync.WaitGroup
				w.Add(1)
				go f(w.Done)
				w.Wait()
			}
		}
	}

	wg.Add(1)
	go f(wg.Done)
	wg.Wait()

	return
}
