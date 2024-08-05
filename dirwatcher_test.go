package gonotify

import (
	"context"
	"go.uber.org/goleak"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestDirWatcher(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx := context.Background()

	dir, err := ioutil.TempDir("", "TestDirWatcher")
	if err != nil {
		t.Error(err)
	}
	defer os.RemoveAll(dir)
	defer os.Remove(dir)

	t.Run("ExistingFile", func(t *testing.T) {

		ctx, cancel := context.WithCancel(ctx)
		dw, err := NewDirWatcher(ctx, IN_CREATE, dir)
		if err != nil {
			t.Error(err)
		}
		defer func() {
			cancel()
			dw.WaitForStop()
		}()

		f, err := os.OpenFile(filepath.Join(dir, "f1"), os.O_CREATE, os.ModePerm)
		f.Close()

		defer os.Remove(filepath.Join(dir, "f1"))

		e := <-dw.C

		if e.Name != filepath.Join(dir, "f1") {
			t.Fail()
		}

	})

	t.Run("FileInSubdir", func(t *testing.T) {

		ctx, cancel := context.WithCancel(ctx)
		dw, err := NewDirWatcher(ctx, IN_CREATE, dir)
		if err != nil {
			t.Error(err)
		}
		defer func() {
			cancel()
			dw.WaitForStop()
		}()

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
}
