//go:build linux
// +build linux

package gonotify

import (
	"context"
	"errors"
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

	// defaultReadTimeout is the default timeout for Read() method. Should be > readRetryDelay
	defaultReadTimeout = time.Millisecond * 50

	// readRetryDelay is the delay between read attempts
	readRetryDelay = time.Millisecond * 20
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

	readMutex         sync.Mutex
	readDeadlineMutex sync.Mutex
}

// NewInotify creates new inotify instance
func NewInotify(ctx context.Context) (*Inotify, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}

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
	// read events goroutine. Only this goroutine can read or close the inotify file descriptor
	go func() {

		defer wg.Done()
		defer syscall.Close(fd)
		defer close(inotify.eventsOut)

		// reusable buffers for reading inotify events. Make sure they're not
		// leaked into other goroutines, as they're not thread safe
		events := make([]InotifyEvent, 0, maxEvents)
		buf := make([]byte, maxEvents*(syscall.SizeofInotifyEvent+syscall.NAME_MAX+1))

		for {

			events = events[:0]

			var n int

			for {
				n, err = syscall.Read(fd, buf)
				if err != nil {
					if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {

						// wait for a little bit while
						select {
						case <-ctx.Done():
							return
						case <-time.After(readRetryDelay):
						}

						continue
					}

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

				events = append(events, InotifyEvent{
					Wd:     uint32(event.Wd),
					Name:   name,
					Mask:   event.Mask,
					Cookie: event.Cookie,
				})
			}

			// send events to the channel
			for _, e := range events {
				select {
				case <-ctx.Done():
					return
				case inotify.eventsOut <- eventItem{
					InotifyEvent: e,
					err:          nil,
				}:
				}
			}

		}

	}()

	wg.Add(1)
	// main goroutine (handle channels)
	go func() {
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
	for {
		evts, err := i.ReadDeadline(time.Now().Add(defaultReadTimeout))
		if err != nil {
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
	i.readDeadlineMutex.Lock()
	defer i.readDeadlineMutex.Unlock()

	events := make([]InotifyEvent, 0, maxEvents)

	for {
		select {
		case <-i.ctx.Done():
			return nil, i.ctx.Err()
		case <-i.Done():
			return nil, errors.New("Inotify closed")
		case <-time.After(time.Until(deadline)):
			return events, nil
		case evt, ok := <-i.eventsOut:
			if !ok {
				return nil, errors.New("Inotify closed")
			}
			if evt.err != nil {
				return nil, evt.err
			}

			events = append(events, evt.InotifyEvent)

			if len(events) >= maxEvents {
				return events, nil
			}

		}
	}

}
