package gonotify

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestOpenClose(t *testing.T) {
	i, err := NewInotify()
	if err != nil {
		t.Error(err)
	}

	err = i.Close()
	if err != nil {
		t.Error(err)
	}

	err = i.Close()
	if err == nil {
		t.Fail()
	}
}

func TestReadFromClosed(t *testing.T) {
	i, err := NewInotify()
	if err != nil {
		t.Error(err)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		evt, err := i.Read()

		if err == nil {
			wg.Done()
			t.Fail()
			return
		}

		if len(evt) != 0 {
			wg.Done()
			t.Fail()
			return
		}

		wg.Done()
	}()

	i.Close()
	wg.Wait()
}

func BenchmarkWatch(b *testing.B) {
	for x := 0; x < b.N; x++ {
		i, err := NewInotify()
		if err != nil {
			b.Error(err)
		}
		i.Close()
	}
}

func TestInotify(t *testing.T) {

	dir, err := ioutil.TempDir("", "TestInotify")
	if err != nil {
		t.Error(err)
	}
	defer os.RemoveAll(dir)
	defer os.Remove(dir)

	t.Run("OpenFile", func(t *testing.T) {
		i, err := NewInotify()
		if err != nil {
			t.Error(err)
		}
		defer i.Close()

		i.AddWatch(dir, IN_ALL_EVENTS)

		f, err := os.OpenFile(filepath.Join(dir, "hz"), os.O_RDWR|os.O_CREATE, 0)
		if err != nil {
			t.Error(err)
		}
		f.Close()

		events, err := i.Read()

		if err != nil {
			t.Error(err)
		}

		event := events[0]

		if event.Name != filepath.Join(dir, "hz") {
			t.Fail()
		}

		if event.Mask&IN_CREATE == 0 {
			t.Fail()
		}

		t.Logf("%#v", event)
	})

	t.Run("MultipleEvents", func(t *testing.T) {
		i, err := NewInotify()
		if err != nil {
			t.Error(err)
		}
		defer i.Close()

		i.AddWatch(dir, IN_CLOSE_WRITE)

		for x := 0; x < 10; x++ {
			fileName := fmt.Sprintf("hz-%d", x)

			f, err := os.OpenFile(filepath.Join(dir, fileName), os.O_RDWR|os.O_CREATE, 0)
			if err != nil {
				t.Error(err)
			}
			f.Close()

			events, err := i.Read()

			if err != nil {
				t.Error(err)
			}

			event := events[0]

			if event.Mask&IN_CLOSE_WRITE == 0 {
				t.Fail()
			}

			if event.Name != filepath.Join(dir, fileName) {
				t.Fail()
			}

			t.Logf("%#v", event)
		}
	})

	t.Run("SelfFolderEvent", func(t *testing.T) {
		i, err := NewInotify()
		if err != nil {
			t.Error(err)
		}
		defer i.Close()

		subdir := filepath.Join(dir, "subdir")
		err = os.Mkdir(subdir, os.ModePerm)
		if err != nil {
			t.Error(err)
		}

		i.AddWatch(subdir, IN_ALL_EVENTS)

		os.RemoveAll(subdir)

		events, err := i.Read()
		if err != nil {
			t.Error(err)
		}

		event := events[0]

		if event.Name != subdir {
			t.Fail()
		}

		if event.Mask&IN_DELETE_SELF == 0 {
			t.Fail()
		}

		t.Logf("%#v", event)
	})
}
