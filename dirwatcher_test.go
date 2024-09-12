package gonotify

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
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
			t.Fail()
		}
	})
}
