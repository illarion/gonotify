package gonotify

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestFileWatcher(t *testing.T) {

	ctx := context.Background()

	dir, err := ioutil.TempDir("", "TestFileWatcher")
	if err != nil {
		t.Error(err)
	}
	defer os.RemoveAll(dir)
	defer os.Remove(dir)

	t.Run("Simple", func(t *testing.T) {

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		f1 := filepath.Join(dir, "/dir1/foo")
		f2 := filepath.Join(dir, "/dir2/bar")

		os.MkdirAll(filepath.Dir(f1), os.ModePerm)
		os.MkdirAll(filepath.Dir(f2), os.ModePerm)

		fw, err := NewFileWatcher(ctx, IN_ALL_EVENTS, f1, f2)
		if err != nil {
			t.Error(err)
		}

		{
			f, err := os.OpenFile(f1, os.O_RDWR|os.O_CREATE, 0)
			if err != nil {
				t.Error(err)
			}
			f.Close()
		}

		{
			f, err := os.OpenFile(f2, os.O_RDWR|os.O_CREATE, 0)
			if err != nil {
				t.Error(err)
			}
			f.Close()
		}

		// 3 events per file
		//TODO checks
		for x := 0; x < 6; x++ {
			e := <-fw.C
			t.Logf("%#v\n", e)
			//fmt.Printf("%#v\n", e)
		}

		cancel()

		select {
		case <-fw.Done():
		case <-time.After(5 * time.Second):

			// output traces of all goroutines of the current program
			buf := make([]byte, 1<<16)
			n := runtime.Stack(buf, true)
			t.Logf("=== BEGIN goroutine stack dump ===\n%s\n=== END goroutine stack dump ===\n", buf[:n])

			t.Error("FileWatcher did not close")
		}

	})

	t.Run("ClosedFileWatcherWithNotConsumedEvents", func(t *testing.T) {

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		f1 := filepath.Join(dir, "/dir1/foo1")
		f2 := filepath.Join(dir, "/dir2/bar2")

		os.MkdirAll(filepath.Dir(f1), os.ModePerm)
		os.MkdirAll(filepath.Dir(f2), os.ModePerm)

		fw, err := NewFileWatcher(ctx, IN_ALL_EVENTS, f1, f2)
		if err != nil {
			t.Error(err)
		}

		{
			f, err := os.OpenFile(f1, os.O_RDWR|os.O_CREATE, 0)
			if err != nil {
				t.Error(err)
			}
			f.Close()
		}

		{
			f, err := os.OpenFile(f2, os.O_RDWR|os.O_CREATE, 0)
			if err != nil {
				t.Error(err)
			}
			f.Close()
		}

		cancel() // close the FileWatcher, but not consume all events
		// the FileWatcher should close and the events should be discarded,
		// so that fw.Done() should be closed very soon

		select {
		case <-fw.Done():
		case <-time.After(5 * time.Second):

			// output traces of all goroutines of the current program
			buf := make([]byte, 1<<16)
			n := runtime.Stack(buf, true)
			t.Logf("=== BEGIN goroutine stack dump ===\n%s\n=== END goroutine stack dump ===\n", buf[:n])

			t.Error("FileWatcher did not close")
		}

	})

	t.Run("ClosedFileWatcherHasClosedChannel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		fw, err := NewFileWatcher(ctx, IN_ALL_EVENTS, filepath.Join(dir, "foo"))
		if err != nil {
			t.Error(err)
		}

		go func() {
			for e := range fw.C {
				t.Logf("Event received: %v", e)
			}
		}()

		cancel()

		select {
		case <-fw.Done():
		case <-time.After(5 * time.Second):

			// output traces of all goroutines of the current program
			buf := make([]byte, 1<<16)
			n := runtime.Stack(buf, true)
			t.Logf("=== BEGIN goroutine stack dump ===\n%s\n=== END goroutine stack dump ===\n", buf[:n])

			t.Error("FileWatcher did not close")
		}

	})

}
