//go:build linux
// +build linux

package gonotify

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var TimeoutError = errors.New("Inotify timeout")

// Inotify is the low level wrapper around inotify_init(), inotify_add_watch() and inotify_rm_watch()
type Inotify struct {
	ctx      context.Context
	m        sync.Mutex
	fd       int
	watches  map[string]uint32
	rwatches map[uint32]string
}

// NewInotify creates new inotify instance
func NewInotify(ctx context.Context) (*Inotify, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)

	if err != nil {
		return nil, err
	}

	inotify := &Inotify{
		ctx:      ctx,
		fd:       fd,
		watches:  make(map[string]uint32),
		rwatches: make(map[uint32]string),
	}

	go func() {
		<-ctx.Done()
		inotify.close()
	}()

	return inotify, nil
}

// AddWatch adds given path to list of watched files / folders
func (i *Inotify) AddWatch(pathName string, mask uint32) error {
	w, err := syscall.InotifyAddWatch(i.fd, pathName, mask)

	if err != nil {
		return err
	}

	i.m.Lock()
	i.watches[pathName] = uint32(w)
	i.rwatches[uint32(w)] = pathName
	i.m.Unlock()
	return nil
}

// RmWd removes watch by watch descriptor
func (i *Inotify) RmWd(wd uint32) error {
	i.m.Lock()
	defer i.m.Unlock()

	pathName, ok := i.rwatches[wd]
	if !ok {
		return nil
	}

	_, err := syscall.InotifyRmWatch(i.fd, wd)
	if err != nil {
		return err
	}

	delete(i.watches, pathName)
	delete(i.rwatches, wd)
	return nil
}

// RmWatch removes watch by pathName
func (i *Inotify) RmWatch(pathName string) error {
	i.m.Lock()
	defer i.m.Unlock()

	wd, ok := i.watches[pathName]
	if !ok {
		return nil
	}

	_, err := syscall.InotifyRmWatch(i.fd, wd)
	if err != nil {
		return err
	}

	delete(i.watches, pathName)
	delete(i.rwatches, wd)
	return nil
}

// Read reads portion of InotifyEvents and may fail with an error. If no events are available, it will
// wait forever, until context is cancelled.
func (i *Inotify) Read() ([]InotifyEvent, error) {
	for {
		evts, err := i.ReadDeadline(time.Now().Add(time.Millisecond * 200))
		if err != nil {
			if err == TimeoutError {
				continue
			}
			return evts, err
		}
		if len(evts) > 0 {
			return evts, nil
		}
	}
}

// ReadDeadline waits for InotifyEvents until deadline is reached, or context is cancelled. If
// deadline is reached, TimeoutError is returned.
func (i *Inotify) ReadDeadline(deadline time.Time) ([]InotifyEvent, error) {
	events := make([]InotifyEvent, 0, 1024)
	buf := make([]byte, 1024*(syscall.SizeofInotifyEvent+16))

	var n int
	var err error

	fdset := &syscall.FdSet{}
	fdset.Bits[0] = 1 << uint(i.fd)

	for {
		if i.ctx.Err() != nil {
			return events, i.ctx.Err()
		}

		now := time.Now()

		if now.After(deadline) {
			return events, TimeoutError
		}

		diff := deadline.Sub(now)

		timeout := syscall.NsecToTimeval(diff.Nanoseconds())

		_, err = syscall.Select(i.fd+1, fdset, nil, nil, &timeout)

		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return events, err
		}

		n, err = syscall.Read(i.fd, buf)
		if err != nil {
			if err == syscall.EAGAIN {
				continue
			}
			return events, err
		}

		if n > 0 {
			break
		}
	}

	if n < syscall.SizeofInotifyEvent {
		return events, fmt.Errorf("Short inotify read")
	}

	offset := 0

	for offset+syscall.SizeofInotifyEvent <= n {

		event := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[offset]))
		namebuf := buf[offset+syscall.SizeofInotifyEvent : offset+syscall.SizeofInotifyEvent+int(event.Len)]

		offset += syscall.SizeofInotifyEvent + int(event.Len)

		name := strings.TrimRight(string(namebuf), "\x00")
		name = filepath.Join(i.rwatches[uint32(event.Wd)], name)
		events = append(events, InotifyEvent{
			Wd:     uint32(event.Wd),
			Name:   name,
			Mask:   event.Mask,
			Cookie: event.Cookie,
		})
	}

	return events, nil
}

func (i *Inotify) close() {
	i.m.Lock()
	defer i.m.Unlock()

	for _, w := range i.watches {
		_, err := syscall.InotifyRmWatch(i.fd, w)
		if err != nil {
			continue
		}
	}
	syscall.Close(i.fd)
}
