package gonotify

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOpenClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := NewInotify(ctx)
	if err != nil {
		t.Error(err)
	}
}

func TestReadFromClosed(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())

	i, err := NewInotify(ctx)
	if err != nil {
		t.Error(err)
	}

	exp := make(chan struct{})

	go func() {
		evt, err := i.Read()

		if err == nil {
			close(exp)
			t.Error("Expected error from closed inotify.Read")
			return
		}

		if len(evt) != 0 {
			close(exp)
			t.Error("Expected no events from closed inotify.Read")
			return
		}

		close(exp)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-exp:
		return
	case <-time.After(1 * time.Second):
		pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
		t.Error("Cancelling context did not close inotify.Read")
	}
}

func BenchmarkWatch(b *testing.B) {
	for x := 0; x < b.N; x++ {
		ctx, cancel := context.WithCancel(context.Background())
		_, err := NewInotify(ctx)
		if err != nil {
			b.Error(err)
		}
		cancel()
	}
}

func TestInotify(t *testing.T) {
	ctx := context.Background()

	dir, err := ioutil.TempDir("", "TestInotify")
	if err != nil {
		t.Error(err)
	}
	defer os.RemoveAll(dir)
	defer os.Remove(dir)

	t.Run("OpenFile", func(t *testing.T) {

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		i, err := NewInotify(ctx)
		if err != nil {
			t.Error(err)
		}

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
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		i, err := NewInotify(ctx)
		if err != nil {
			t.Error(err)
		}

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

	// This test should generate more events than the buffer passed to syscall.Read()
	// can handle. This is to test the buffer handling in the ReadDeadline() method.
	// The potential bug is that the buffer may contain patial event at the end of the buffer
	// and the next syscall.Read() will not be able to read the rest of the event.
	t.Run("MultipleEvents #2 - Reading leftover events", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		i, err := NewInotify(ctx)
		if err != nil {
			t.Error(err)
		}

		i.AddWatch(dir, IN_CLOSE_WRITE)

		// generate 2* maxEvents events with long filenames (in range from syscall.NAME_MAX-10 to syscall.NAME_MAX)
		for x := 0; x < 2*maxEvents; x++ {
			fileNameLen := syscall.NAME_MAX - 10 + x%10
			// make a filename with len = fileNameLen
			fileName := fmt.Sprintf("%s-%d", "hz", x)
			fileName = fmt.Sprintf("%s%s", fileName, strings.Repeat("a", fileNameLen-len(fileName)+1))

			f, err := os.OpenFile(filepath.Join(dir, fileName), os.O_RDWR|os.O_CREATE, 0)
			if err != nil {
				t.Error(err)
			}
			f.Close()
		}

		// read all events
		events, err := i.Read()
		if err != nil {
			t.Error(err)
		}

		events2, err := i.Read()
		if err != nil {
			t.Error(err)
		}

		// check if all events were read
		if len(events)+len(events2) != 2*maxEvents {
			t.Errorf("Expected %d events, but got %d", 2*maxEvents, len(events))
		}

	})

	t.Run("SelfFolderEvent", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		i, err := NewInotify(ctx)
		if err != nil {
			t.Error(err)
		}

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

	t.Run("Bug #2 Inotify.Read() discards solo events", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		i, err := NewInotify(ctx)

		if err != nil {
			t.Error(err)
		}

		subdir := filepath.Join(dir, "subdir#2_1")
		err = os.Mkdir(subdir, os.ModePerm)
		if err != nil {
			t.Error(err)
		}

		i.AddWatch(subdir, IN_CREATE)

		fileName := "single-file.txt"

		f, err := os.OpenFile(filepath.Join(subdir, fileName), os.O_RDWR|os.O_CREATE, 0)
		if err != nil {
			t.Error(err)
		}
		f.Close()

		events, err := i.Read()

		if err != nil {
			t.Error(err)
		}

		expected := 1
		if len(events) != expected {
			t.Errorf("Length of read events is %d, but extected %d", len(events), expected)
		}

	})

	t.Run("Bug #2 Inotify.Read() discards solo events (case 2)", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		i, err := NewInotify(ctx)

		if err != nil {
			t.Error(err)
		}

		subdir := filepath.Join(dir, "subdir#2_2")
		err = os.Mkdir(subdir, os.ModePerm)
		if err != nil {
			t.Error(err)
		}

		fileName := "single-file.txt"
		fullPath := filepath.Join(subdir, fileName)

		f, err := os.OpenFile(fullPath, os.O_RDWR|os.O_CREATE, 0666)
		if err != nil {
			t.Error(err)
		}

		i.AddWatch(fullPath, IN_MODIFY)

		f.Write([]byte("Hello world\n"))
		f.Close()

		events, err := i.Read()

		if err != nil {
			t.Error(err)
		}

		expected := 1
		if len(events) != expected {

			for _, event := range events {
				fmt.Printf("Event %#v\n", event)
			}

			t.Errorf("Length of read events is %d, but extected %d", len(events), expected)
		}

	})
}

func TestValidateInteger(t *testing.T) {
	t.Run("Overflows", func(t *testing.T) {
		verr := ValidateVsMaximumAllowedUint32Size(5294967295)
		if !errors.Is(verr, UnsignedIntegerOverflowError) {
			t.Error(verr)
		}
	})

	t.Run("Ok", func(t *testing.T) {
		verr := ValidateVsMaximumAllowedUint32Size(22949)
		if errors.Is(verr, UnsignedIntegerOverflowError) {
			t.Error(verr)
		}
	})
}
