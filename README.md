## Gonotify 

Simple Golang inotify wrapper.

[![Build Status](https://travis-ci.org/illarion/gonotify.svg?branch=master)](https://travis-ci.org/illarion/gonotify)

### Provides following primitives:

* `Inotify` - low level wrapper around [inotify(7)](http://man7.org/linux/man-pages/man7/inotify.7.html)
* `InotifyEvent` - generated file/folder event. Contains `Name` (full path), watch descriptior and `Mask` that describes the event.

* `FileWatcher` - higher level utility, helps to watch the list of files for changes, creation or removal
* `DirWatcher` - higher level utility, recursively watches given root folder for added, removed or changed files.
** `FileEvent` - embeds `InotifyEvent` and keeps additional field `Eof` to notify user that there will be no more events. 

You can use `FileWathcer` and `DirWatcher` as an example and build your own utilities based on `Inotify`.

## License
MIT. See LICENSE file for more details.

