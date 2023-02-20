package gonotify

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
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
			t.Fail()
		}

		if e.Name != filepath.Join(dir, "subfolder", "f1") {
			t.Fail()
		}

		// there should be no duplicate
		select {
		case e := <-dw.C:
			if !e.Eof {
				t.Fail()
			}
		default:
		}

	})
}
