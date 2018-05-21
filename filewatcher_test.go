package gonotify

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestFileWatcher(t *testing.T) {

	dir, err := ioutil.TempDir("", "TestFileWatcher")
	if err != nil {
		t.Error(err)
	}
	defer os.RemoveAll(dir)

	t.Run("Simple", func(t *testing.T) {

		f1 := filepath.Join(dir, "/dir1/foo")
		f2 := filepath.Join(dir, "/dir2/bar")

		os.MkdirAll(filepath.Dir(f1), os.ModePerm)
		os.MkdirAll(filepath.Dir(f2), os.ModePerm)

		fw, err := NewFileWatcher(IN_ALL_EVENTS, f1, f2)
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
			fmt.Printf("%#v\n", e)
		}

		fw.Close()

	})

}
