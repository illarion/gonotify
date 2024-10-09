// Copyright 2009 The Go Authors.
// based on golang's syscall.InotifyRmWatch

//go:build linux && arm

package syscallf

import "syscall"

func InotifyRmWatch(fd int, watchdesc int) (success int, err error) {
	r0, _, e1 := syscall.RawSyscall(syscall.SYS_INOTIFY_RM_WATCH, uintptr(fd), uintptr(watchdesc), 0)
	success = int(r0)
	if e1 != 0 {
		err = errnoErr(e1)
	}
	return
}
