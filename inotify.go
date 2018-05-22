package gonotify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	IN_ACCESS        = uint32(syscall.IN_ACCESS)        // File was accessed
	IN_ATTRIB        = uint32(syscall.IN_ATTRIB)        // Metadata changed
	IN_CLOSE_WRITE   = uint32(syscall.IN_CLOSE_WRITE)   // File opened for writing was closed.
	IN_CLOSE_NOWRITE = uint32(syscall.IN_CLOSE_NOWRITE) // File or directory not opened for writing was closed.
	IN_CREATE        = uint32(syscall.IN_CREATE)        // File/directory created in watched directory
	IN_DELETE        = uint32(syscall.IN_DELETE)        // File/directory deleted from watched directory.
	IN_DELETE_SELF   = uint32(syscall.IN_DELETE_SELF)   // Watched file/directory was itself deleted.
	IN_MODIFY        = uint32(syscall.IN_MODIFY)        // File was modified
	IN_MOVE_SELF     = uint32(syscall.IN_MOVE_SELF)     // Watched file/directory was itself moved.
	IN_MOVED_FROM    = uint32(syscall.IN_MOVED_FROM)    // Generated for the directory containing the old filename when a file is renamed.
	IN_MOVED_TO      = uint32(syscall.IN_MOVED_TO)      // Generated for the directory containing the new filename when a file is renamed.
	IN_OPEN          = uint32(syscall.IN_OPEN)          // File or directory was opened.

	IN_ALL_EVENTS = uint32(syscall.IN_ALL_EVENTS) // bit mask of all of the above events.
	IN_MOVE       = uint32(syscall.IN_MOVE)       // Equates to IN_MOVED_FROM | IN_MOVED_TO.
	IN_CLOSE      = uint32(syscall.IN_CLOSE)      // Equates to IN_CLOSE_WRITE | IN_CLOSE_NOWRITE.

	// The following further bits can be specified in mask when calling Inotify.AddWatch()
	IN_DONT_FOLLOW = uint32(syscall.IN_DONT_FOLLOW) // Don't dereference pathname if it is a symbolic link.
	IN_EXCL_UNLINK = uint32(syscall.IN_EXCL_UNLINK) // Don't generate events for children if they have been unlinked from the directory.
	IN_MASK_ADD    = uint32(syscall.IN_MASK_ADD)    // Add (OR) the events in mask to the watch mask
	IN_ONESHOT     = uint32(syscall.IN_ONESHOT)     // Monitor the filesystem object corresponding to pathname for one event, then remove from watch list.
	IN_ONLYDIR     = uint32(syscall.IN_ONLYDIR)     // Watch pathname only if it is a directory.

	// The following bits may be set in the mask field returned by Inotify.Read()
	IN_IGNORED    = uint32(syscall.IN_IGNORED)    // Watch was removed explicitly or automatically
	IN_ISDIR      = uint32(syscall.IN_ISDIR)      // Subject of this event is a directory.
	IN_Q_OVERFLOW = uint32(syscall.IN_Q_OVERFLOW) // Event queue overflowed (wd is -1 for this event).

	IN_UNMOUNT = uint32(syscall.IN_UNMOUNT) // Filesystem containing watched object was unmounted.
)

type InotifyEvent struct {
	Wd     uint32 // watch descriptor
	Name   string // file or directory name
	Mask   uint32 // contains bits that describe the event that occurred
	Cookie uint32 // usually 0, but if events (like IN_MOVED_FROM and IN_MOVED_TO) are linked then they will have equal cookie
}

/**
 * Inotify is the low level wrapper around inotify_init(), inotify_add_watch() and inotify_rm_watch()
 */
type Inotify struct {
	m        sync.Mutex
	fd       int
	f        *os.File
	watches  map[string]uint32
	rwatches map[uint32]string
}

func NewInotify() (*Inotify, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC)

	if err != nil {
		return nil, err
	}

	return &Inotify{
		fd:       fd,
		f:        os.NewFile(uintptr(fd), ""),
		watches:  make(map[string]uint32),
		rwatches: make(map[uint32]string),
	}, nil
}

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

func (i *Inotify) Read() ([]InotifyEvent, error) {
	events := make([]InotifyEvent, 0, 1024)
	buf := make([]byte, 1024*(syscall.SizeofInotifyEvent+16))

	n, err := i.f.Read(buf)

	if err != nil {
		return events, err
	}

	if n < syscall.SizeofInotifyEvent {
		return events, fmt.Errorf("Short inotify read")
	}

	offset := 0

	for offset+syscall.SizeofInotifyEvent < n {

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

func (i *Inotify) Close() error {
	i.m.Lock()
	defer i.m.Unlock()

	for _, w := range i.watches {
		_, err := syscall.InotifyRmWatch(i.fd, w)
		if err != nil {
			return err
		}
	}
	return i.f.Close()
}
