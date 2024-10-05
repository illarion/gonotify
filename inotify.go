//go:build linux
// +build linux

package gonotify

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// max number of events to read at once
const maxEvents = 1024
const maxUint32 = int(^uint32(0))

var TimeoutError = errors.New("Inotify timeout")
var UnsignedIntegerOverflowError = errors.New("unsigned integer overflow")

type addWatchRequest struct {
	pathName string
	mask     uint32
	result   chan error
}

type readResponse struct {
	events []InotifyEvent
	err    error
}

type readRequest struct {
	deadline time.Time
	result   chan readResponse
}

// Inotify is the low level wrapper around inotify_init(), inotify_add_watch() and inotify_rm_watch()
type Inotify struct {
	ctx        context.Context
	done       chan struct{}
	addWatchIn chan addWatchRequest
	rmByWdIn   chan uint32
	rmByPathIn chan string
	readIn     chan readRequest
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
		readIn:     make(chan readRequest),
	}

	go func() {
		defer close(inotify.done)
		defer syscall.Close(fd)

		watches := make(map[string]uint32)
		paths := make(map[uint32]string)

	main:
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
					case req := <-inotify.readIn:
						// Send error to read requests
						select {
						case req.result <- readResponse{err: errors.New("Inotify instance closed")}:
						default:
						}
					case <-time.After(200 * time.Millisecond):
						draining = false
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
				verr := ValidateInteger(wd)
				if err == nil && verr != nil {
					wdu32 := uint32(wd)
					watches[req.pathName] = wdu32
					paths[wdu32] = req.pathName
				}
				if err == nil && verr != nil {
					err = verr
				}
				select {
				case req.result <- err:
				case <-ctx.Done():
				}
			case req := <-inotify.readIn:
				{
					deadline := req.deadline
					response := readResponse{}

					events := make([]InotifyEvent, 0, maxEvents)
					buf := make([]byte, maxEvents*(syscall.SizeofInotifyEvent+syscall.NAME_MAX+1))

					var n int
				read:
					for {
						now := time.Now()
						if now.After(deadline) {
							response.err = TimeoutError
							response.events = events
							select {
							case req.result <- response:
							case <-ctx.Done():
							}
							continue main
						}

						n, err = syscall.Read(fd, buf)
						if err != nil {
							if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {

								// wait for a little bit while
								select {
								case <-time.After(time.Millisecond * 200):
								case <-ctx.Done():
									continue main
								}

								continue read
							}
							response.err = err
							response.events = events
							select {
							case req.result <- response:
							case <-ctx.Done():
							}
							continue main
						}

						if n > 0 {
							break read
						}
					}

					if n < syscall.SizeofInotifyEvent {
						response.err = fmt.Errorf("short inotify read, expected at least one SizeofInotifyEvent %d, got %d", syscall.SizeofInotifyEvent, n)
						response.events = events
						select {
						case req.result <- response:
						case <-ctx.Done():
						}
						continue main
					}

					offset := 0
					for offset+syscall.SizeofInotifyEvent <= n {
						event := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[offset]))
						verr := ValidateInteger(int(event.Wd))
						if verr != nil {
							response.err = UnsignedIntegerOverflowError
							response.events = events
							select {
							case req.result <- response:
							case <-ctx.Done():
							}
							continue main
						}

						var name string
						{
							nameStart := offset + syscall.SizeofInotifyEvent
							nameEnd := offset + syscall.SizeofInotifyEvent + int(event.Len)

							if nameEnd > n {
								response.err = fmt.Errorf("corrupted inotify event length %d", event.Len)
								response.events = events
								select {
								case req.result <- response:
								case <-ctx.Done():
								}
								continue main
							}

							name = strings.TrimRight(string(buf[nameStart:nameEnd]), "\x00")
							offset = nameEnd
						}

						watchName, ok := paths[uint32(event.Wd)]
						if !ok {
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

					response.err = nil
					response.events = events
					select {
					case req.result <- response:
					case <-ctx.Done():
					}
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
			}
		}
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
	for {
		evts, err := i.ReadDeadline(time.Now().Add(time.Millisecond * 300))
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
	req := readRequest{
		deadline: deadline,
		result:   make(chan readResponse),
	}

	select {
	case <-i.ctx.Done():
		return nil, i.ctx.Err()
	case i.readIn <- req:
		select {
		case <-i.ctx.Done():
			return nil, i.ctx.Err()
		case resp := <-req.result:
			return resp.events, resp.err
		}
	}
}

func ValidateInteger(wd int) error {
	if wd > 0 && wd > maxUint32 {
		return UnsignedIntegerOverflowError
	}
	return nil
}
