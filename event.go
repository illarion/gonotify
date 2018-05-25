package gonotify

// FileEvent is the wrapper around InotifyEvent with additional Eof marker. Reading from
// FileEvents from DirWatcher.C or FileWatcher.C may end with Eof when underlying inotify is closed
type FileEvent struct {
	InotifyEvent
	Eof bool
}
