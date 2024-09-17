//go:build linux
// +build linux

package gonotify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	// maxEvents is the maximum number of events to read in one syscall
	maxEvents = 1024
)

type addWatchRequest struct {
	pathName string
	mask     uint32
	result   chan error
}

type eventItem struct {
	InotifyEvent
	err error
}

// Inotify is the low level wrapper around inotify_init(), inotify_add_watch() and inotify_rm_watch()
type Inotify struct {
	ctx        context.Context
	done       chan struct{}
	addWatchIn chan addWatchRequest
	rmByWdIn   chan uint32
	rmByPathIn chan string
	eventsOut  chan eventItem

	readMutex sync.Mutex
}

// NewInotify creates new inotify instance
func NewInotify(ctx context.Context) (*Inotify, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}

	file := os.NewFile(uintptr(fd), "inotify")

	//ctx, cancel := context.WithCancel(ctx)

	inotify := &Inotify{
		ctx:        ctx,
		done:       make(chan struct{}),
		addWatchIn: make(chan addWatchRequest),
		rmByWdIn:   make(chan uint32),
		rmByPathIn: make(chan string),
		eventsOut:  make(chan eventItem, maxEvents),
	}

	type getPathRequest struct {
		wd     uint32
		result chan string
	}

	getPathIn := make(chan getPathRequest)

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		//defer cancel()
		<-ctx.Done()
		file.Close()
		wg.Done()
	}()

	wg.Add(1)
	// read events goroutine. Only this goroutine can read or close the inotify file descriptor
	go func() {
		//defer cancel()
		defer wg.Done()
		//defer file.Close()
		defer close(inotify.eventsOut)

		// reusable buffers for reading inotify events. Make sure they're not
		// leaked into other goroutines, as they're not thread safe
		buf := make([]byte, maxEvents*(syscall.SizeofInotifyEvent+syscall.NAME_MAX+1))

		for {

			select {
			case <-ctx.Done():
				return
			default:
			}

			var n int

			for {

				select {
				case <-ctx.Done():
					return
				default:
				}

				n, err = file.Read(buf)
				if err != nil {

					// if we got an error, we should return
					select {
					case inotify.eventsOut <- eventItem{
						InotifyEvent: InotifyEvent{},
						err:          err,
					}:
					default:
					}

					return
				}

				if n > 0 {
					break
				}
			}

			if n < syscall.SizeofInotifyEvent {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}

			offset := 0
			for offset+syscall.SizeofInotifyEvent <= n {
				event := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[offset]))
				var name string
				{
					nameStart := offset + syscall.SizeofInotifyEvent
					nameEnd := offset + syscall.SizeofInotifyEvent + int(event.Len)

					if nameEnd > n {
						continue
					}

					name = strings.TrimRight(string(buf[nameStart:nameEnd]), "\x00")
					offset = nameEnd
				}

				req := getPathRequest{wd: uint32(event.Wd), result: make(chan string)}
				var watchName string

				select {
				case <-ctx.Done():
					return
				case getPathIn <- req:

					select {
					case <-ctx.Done():
						return
					case watchName = <-req.result:
					}

				}

				if watchName == "" {
					continue
				}

				name = filepath.Join(watchName, name)

				inotifyEvent := InotifyEvent{
					Wd:     uint32(event.Wd),
					Name:   name,
					Mask:   event.Mask,
					Cookie: event.Cookie,
				}

				// watch was removed explicitly or automatically
				if inotifyEvent.Is(IN_IGNORED) {

					// remove watch

					select {
					case <-ctx.Done():
						return
					case inotify.rmByWdIn <- inotifyEvent.Wd:
					case <-time.After(1 * time.Second):
					}

					continue

				}

				select {
				case <-ctx.Done():
					return
				case inotify.eventsOut <- eventItem{
					InotifyEvent: inotifyEvent,
					err:          nil,
				}:
				}

			}

		}

	}()

	wg.Add(1)
	// main goroutine (handle channels)
	go func() {
		//defer cancel()
		defer wg.Done()

		watches := make(map[string]uint32)
		paths := make(map[uint32]string)

		for {
			select {
			case <-ctx.Done():

				// Handle pending requests
				draining := true

				for draining {
					select {
					case req := <-inotify.addWatchIn:
						// Send error to addWatch requests
						select {
						case req.result <- errors.New("Inotify instance closed"):
						default:
						}
					case <-inotify.rmByWdIn:
					case <-inotify.addWatchIn:
					case <-inotify.rmByPathIn:
					case <-getPathIn:

					default:
						draining = false
					}
				}

				for _, w := range watches {
					_, err := syscall.InotifyRmWatch(fd, w)
					if err != nil {
						continue
					}
				}

				return
			case req := <-inotify.addWatchIn:
				wd, err := syscall.InotifyAddWatch(fd, req.pathName, req.mask)
				if err == nil {
					wdu32 := uint32(wd)
					watches[req.pathName] = wdu32
					paths[wdu32] = req.pathName
				}
				select {
				case req.result <- err:
				case <-ctx.Done():
				}
			case wd := <-inotify.rmByWdIn:
				pathName, ok := paths[wd]
				if !ok {
					continue
				}
				delete(watches, pathName)
				delete(paths, wd)
			case pathName := <-inotify.rmByPathIn:
				wd, ok := watches[pathName]
				if !ok {
					continue
				}
				delete(watches, pathName)
				delete(paths, wd)
			case req := <-getPathIn:
				wd := paths[req.wd]
				select {
				case req.result <- wd:
				case <-ctx.Done():
				}
			}
		}
	}()

	go func() {
		//defer cancel()
		wg.Wait()
		close(inotify.done)
	}()

	return inotify, nil
}

// Done returns a channel that is closed when Inotify is done
func (i *Inotify) Done() <-chan struct{} {
	return i.done
}

// AddWatch adds given path to list of watched files / folders
func (i *Inotify) AddWatch(pathName string, mask uint32) error {

	req := addWatchRequest{
		pathName: pathName,
		mask:     mask,
		result:   make(chan error),
	}

	select {
	case <-i.ctx.Done():
		return i.ctx.Err()
	case i.addWatchIn <- req:

		select {
		case <-i.ctx.Done():
			return i.ctx.Err()
		case err := <-req.result:
			return err
		}
	}
}

// RmWd removes watch by watch descriptor
func (i *Inotify) RmWd(wd uint32) error {
	select {
	case <-i.ctx.Done():
		return i.ctx.Err()
	case i.rmByWdIn <- wd:
		return nil
	}
}

// RmWatch removes watch by pathName
func (i *Inotify) RmWatch(pathName string) error {
	select {
	case <-i.ctx.Done():
		return i.ctx.Err()
	case i.rmByPathIn <- pathName:
		return nil
	}
}

// Read reads portion of InotifyEvents and may fail with an error. If no events are available, it will
// wait forever, until context is cancelled.
func (i *Inotify) Read() ([]InotifyEvent, error) {
	i.readMutex.Lock()
	defer i.readMutex.Unlock()

	events := make([]InotifyEvent, 0, maxEvents)

	select {
	case <-i.ctx.Done():
		return events, i.ctx.Err()
	case <-i.Done():
		return events, errors.New("inotify closed")
	case evt, ok := <-i.eventsOut:

		if !ok {
			return events, errors.New("inotify closed")
		}
		if evt.err != nil {
			return events, evt.err
		}

		if evt.InotifyEvent.Wd != 0 {
			// append first event
			events = append(events, evt.InotifyEvent)
		}

		if len(events) >= maxEvents {
			return events, nil
		}

		// read all available events
	read:
		for {

			select {
			case <-i.ctx.Done():
				return events, i.ctx.Err()
			case <-i.Done():
				return events, errors.New("inotify closed")
			case evt, ok := <-i.eventsOut:
				if !ok {
					return events, errors.New("inotify closed")
				}
				if evt.err != nil {
					return events, evt.err
				}

				if evt.InotifyEvent.Wd != 0 {
					// append event
					events = append(events, evt.InotifyEvent)
				}

				if len(events) >= maxEvents {
					return events, nil
				}

			default:
				break read
			}

		}

	}

	return events, nil
}

// ReadDeadline waits for InotifyEvents until deadline is reached, or context is cancelled. If
// deadline is reached, TimeoutError is returned.
func (i *Inotify) ReadDeadline(deadline time.Time) ([]InotifyEvent, error) {
	i.readMutex.Lock()
	defer i.readMutex.Unlock()

	events := make([]InotifyEvent, 0, maxEvents)

	for {
		select {
		case <-i.ctx.Done():
			return events, i.ctx.Err()
		case <-i.Done():
			return events, errors.New("Inotify closed")
		case <-time.After(time.Until(deadline)):
			return events, nil
		case evt, ok := <-i.eventsOut:
			if !ok {
				return events, errors.New("Inotify closed")
			}
			if evt.err != nil {
				return events, evt.err
			}

			events = append(events, evt.InotifyEvent)

			if len(events) >= maxEvents {
				return events, nil
			}

		}
	}

}
