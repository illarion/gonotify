package gonotify

import (
	"strings"
	"testing"
)

// TestInotifyEvent_Is tests the Is function of InotifyEvent
func TestInotifyEvent_Is(t *testing.T) {
	var i InotifyEvent
	i.Mask = IN_ACCESS
	if !i.Is(IN_ACCESS) {
		t.Fail()
	}
}

// TestInotifyEvent_IsAny tests the IsAny function of InotifyEvent
func TestInotifyEvent_IsAny(t *testing.T) {
	var i InotifyEvent
	i.Mask = IN_ACCESS
	if !i.IsAny(IN_ACCESS, IN_ATTRIB) {
		t.Fail()
	}
}

// TestInotifyEvent_IsAll tests the IsAll function of InotifyEvent
func TestInotifyEvent_IsAll(t *testing.T) {
	var i InotifyEvent
	i.Mask = IN_ACCESS | IN_ATTRIB
	if !i.IsAll(IN_ACCESS, IN_ATTRIB) {
		t.Fail()
	}
}

// TestInotifyEvent_String tests the String function of InotifyEvent
func TestInotifyEvent_String(t *testing.T) {
	var i InotifyEvent
	i.Mask = IN_ACCESS | IN_ATTRIB | IN_CLOSE_WRITE
	if !strings.Contains(i.String(), "IN_ACCESS") {
		t.Fail()
	}
	if !strings.Contains(i.String(), "IN_ATTRIB") {
		t.Fail()
	}
	if !strings.Contains(i.String(), "IN_CLOSE_WRITE") {
		t.Fail()
	}
}
