package gonotify

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestDirWatcher(t *testing.T) {
	ctx := context.Background()

	dir, err := ioutil.TempDir("", "TestDirWatcher")
	if err != nil {
		t.Error(err)
	}
	defer os.RemoveAll(dir)
	defer os.Remove(dir)

	t.Run("ExistingFile", func(t *testing.T) {

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		f, err := os.OpenFile(filepath.Join(dir, "f1"), os.O_CREATE, os.ModePerm)
		f.Close()

		defer os.Remove(filepath.Join(dir, "f1"))

		dw, err := NewDirWatcher(ctx, IN_CREATE, dir)
		if err != nil {
			t.Error(err)
		}

		e := <-dw.C

		if e.Name != filepath.Join(dir, "f1") {
			t.Fail()
		}

		cancel()

		select {
		case <-dw.Done():
		case <-time.After(5 * time.Second):
			// output traces of all goroutines of the current program
			buf := make([]byte, 1<<16)
			n := runtime.Stack(buf, true)
			t.Logf("=== BEGIN goroutine stack dump ===\n%s\n=== END goroutine stack dump ===\n", buf[:n])

			t.Error("DirWatcher did not close")
		}

	})

	t.Run("FileInSubdir", func(t *testing.T) {

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		dw, err := NewDirWatcher(ctx, IN_CREATE, dir)
		if err != nil {
			t.Error(err)
		}

		err = os.Mkdir(filepath.Join(dir, "subfolder"), os.ModePerm)
		if err != nil {
			t.Error(err)
		}

		f, err := os.OpenFile(filepath.Join(dir, "subfolder", "f1"), os.O_CREATE, os.ModePerm)
		f.Close()

		e := <-dw.C

		if e.Eof {
			t.Errorf("EOF event received: %v", e)
		}

		if e.Name != filepath.Join(dir, "subfolder", "f1") {
			t.Errorf("Wrong event received: %v", e)
		}

		select {
		case e := <-dw.C:
			if !e.Eof {
				t.Fail()
			}
		default:
		}

		cancel()

		select {
		case <-dw.Done():
		case <-time.After(5 * time.Second):

			// output traces of all goroutines of the current program
			buf := make([]byte, 1<<16)
			n := runtime.Stack(buf, true)
			t.Logf("=== BEGIN goroutine stack dump ===\n%s\n=== END goroutine stack dump ===\n", buf[:n])

			t.Error("DirWatcher did not close")
		}

	})

	t.Run("ClosedDirwatcherWithNotConsumedEvents", func(t *testing.T) {

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		dw, err := NewDirWatcher(ctx, IN_CREATE, dir)
		if err != nil {
			t.Error(err)
		}

		err = os.Mkdir(filepath.Join(dir, "subfolder2"), os.ModePerm)
		if err != nil {
			t.Error(err)
		}

		f, err := os.OpenFile(filepath.Join(dir, "subfolder2", "f1"), os.O_CREATE, os.ModePerm)
		f.Close()

		cancel() // close the DirWatcher, but do not consume the events
		// the DirWatcher should close and the events should be discarded,
		// so that dw.Done() should be closed very soon

		select {
		case <-dw.Done():
		case <-time.After(5 * time.Second):

			// output traces of all goroutines of the current program
			buf := make([]byte, 1<<16)
			n := runtime.Stack(buf, true)
			t.Logf("=== BEGIN goroutine stack dump ===\n%s\n=== END goroutine stack dump ===\n", buf[:n])

			t.Error("DirWatcher did not close")
		}

	})

	t.Run("ClosedDirwatcherBecomesDone", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		dw, err := NewDirWatcher(ctx, IN_CREATE, dir)
		if err != nil {
			t.Error(err)
		}

		var wg sync.WaitGroup
		defer wg.Wait()

		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range dw.C {
				t.Logf("Event received: %v", e)
			}
		}()

		cancel()

		select {
		case <-dw.Done():
		case <-time.After(5 * time.Second):

			// output traces of all goroutines of the current program
			buf := make([]byte, 1<<16)
			n := runtime.Stack(buf, true)
			t.Logf("=== BEGIN goroutine stack dump ===\n%s\n=== END goroutine stack dump ===\n", buf[:n])

			t.Error("DirWatcher did not close")
		}
	})
}
