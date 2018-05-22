package gonotify

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestDirWatcher(t *testing.T) {

	dir, err := ioutil.TempDir("", "TestDirWatcher")
	if err != nil {
		t.Error(err)
	}
	defer os.RemoveAll(dir)
	defer os.Remove(dir)

	t.Run("ExistingFile", func(t *testing.T) {

		f, err := os.OpenFile(filepath.Join(dir, "f1"), os.O_CREATE, os.ModePerm)
		f.Close()

		defer os.Remove(filepath.Join(dir, "f1"))

		dw, err := NewDirWatcher(IN_CREATE, dir)
		if err != nil {
			t.Error(err)
		}
		defer dw.Close()

		e := <-dw.C

		if e.Name != filepath.Join(dir, "f1") {
			t.Fail()
		}

	})

	t.Run("FileInSubdir", func(t *testing.T) {

		dw, err := NewDirWatcher(IN_CREATE, dir)
		if err != nil {
			t.Error(err)
		}
		defer dw.Close()

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
		case <-dw.C:
			t.Fail()
		default:
		}

	})
}
