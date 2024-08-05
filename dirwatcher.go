package gonotify

import (
	"context"
	"os"
	"path/filepath"
	"sync"
)

// DirWatcher recursively watches the given root folder, waiting for file events.
// Events can be masked by providing fileMask. DirWatcher does not generate events for
// folders or subfolders.
type DirWatcher struct {
	C  chan FileEvent
	wg sync.WaitGroup
}

// WaitForStop blocks until all internal resources such as goroutines have terminated.
// Call this after cancelling the context passed to NewFileWatcher to deterministically ensure teardown is complete.
func (d *DirWatcher) WaitForStop() {
	d.wg.Wait()
}

// NewDirWatcher creates DirWatcher recursively waiting for events in the given root folder and
// emitting FileEvents in channel C, that correspond to fileMask. Folder events are ignored (having IN_ISDIR set to 1)
func NewDirWatcher(ctx context.Context, fileMask uint32, root string) (*DirWatcher, error) {
	dw := &DirWatcher{
		C: make(chan FileEvent),
	}

	ctx, cancel := context.WithCancel(ctx)

	i, err := NewInotify(ctx)
	if err != nil {
		return nil, err
	}

	queue := make([]FileEvent, 0, 100)

	err = filepath.Walk(root, func(path string, f os.FileInfo, err error) error {

		if err != nil {
			return nil
		}

		if !f.IsDir() {

			//fake event for existing files
			queue = append(queue, FileEvent{
				InotifyEvent: InotifyEvent{
					Name: path,
					Mask: IN_CREATE,
				},
			})

			return nil
		}
		return i.AddWatch(path, IN_ALL_EVENTS)
	})

	if err != nil {
		cancel()
		return nil, err
	}

	events := make(chan FileEvent)

	dw.wg.Add(1)
	go func() {
		defer dw.wg.Done()

		for _, event := range queue {
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		}
		queue = nil

		for {

			raw, err := i.Read()
			if err != nil {
				close(events)
				return
			}

			for _, event := range raw {

				// Skip ignored events queued from removed watchers
				if event.Mask&IN_IGNORED == IN_IGNORED {
					continue
				}

				// Add watch for folders created in watched folders (recursion)
				if event.Mask&(IN_CREATE|IN_ISDIR) == IN_CREATE|IN_ISDIR {

					// After the watch for subfolder is added, it may be already late to detect files
					// created there right after subfolder creation, so we should generate such events
					// ourselves:
					filepath.Walk(event.Name, func(path string, f os.FileInfo, err error) error {
						if err != nil {
							return nil
						}

						if !f.IsDir() {
							// fake event, but there can be duplicates of this event provided by real watcher
							select {
							case events <- FileEvent{
								InotifyEvent: InotifyEvent{
									Name: path,
									Mask: IN_CREATE,
								},
							}:
							case <-ctx.Done():
								return nil
							}
						}

						return nil
					})

					// Wait for further files to be added
					i.AddWatch(event.Name, IN_ALL_EVENTS)

					continue
				}

				// Remove watch for deleted folders
				if event.Mask&IN_DELETE_SELF == IN_DELETE_SELF {
					i.RmWd(event.Wd)
					continue
				}

				// Skip sub-folder events
				if event.Mask&IN_ISDIR == IN_ISDIR {
					continue
				}

				select {
				case events <- FileEvent{
					InotifyEvent: event,
				}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	dw.wg.Add(1)
	go func() {
		defer dw.wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					select {
					case dw.C <- FileEvent{
						Eof: true,
					}:
						cancel()
					case <-ctx.Done():
					}
					return
				}

				// Skip events not conforming with provided mask
				if event.Mask&fileMask == 0 {
					continue
				}

				select {
				case dw.C <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return dw, nil

}
