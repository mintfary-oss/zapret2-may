//go:build !windows

package windivert

import (
	"net"
	"testing"
)

func TestOpen_Stub(t *testing.T) {
	h, err := Open()
	if err == nil {
		t.Error("Open() on non-Windows should return an error")
	}
	if h != nil {
		t.Error("Open() on non-Windows should return nil handle")
	}
}

func TestClose_Stub(t *testing.T) {
	// A nil Handle.Close() should not panic.
	var h Handle
	h.Close()
}

func TestAvailable_Stub(t *testing.T) {
	if Available() {
		t.Error("Available() should return false on non-Windows")
	}
}

func TestIsLoopback_Stub(t *testing.T) {
	if IsLoopback([]byte{1, 2, 3, 4}) {
		t.Error("IsLoopback() should return false on non-Windows")
	}
	if IsLoopback(nil) {
		t.Error("IsLoopback(nil) should return false on non-Windows")
	}
}

func TestSrcIP_Stub(t *testing.T) {
	if ip := SrcIP([]byte{1, 2, 3, 4}); ip != nil {
		t.Errorf("SrcIP() should return nil on non-Windows, got %v", ip)
	}
}

func TestDstIP_Stub(t *testing.T) {
	if ip := DstIP([]byte{1, 2, 3, 4}); ip != nil {
		t.Errorf("DstIP() should return nil on non-Windows, got %v", ip)
	}
}

func TestRunIntercept_Stub(t *testing.T) {
	var h Handle
	// Should not panic.
	h.RunIntercept("any-addr")
}

func TestErrNotAvailable(t *testing.T) {
	if ErrNotAvailable == nil {
		t.Error("ErrNotAvailable should not be nil")
	}
}

// Compile-time check that SrcIP/DstIP return net.IP.
var _ net.IP = SrcIP(nil)
var _ net.IP = DstIP(nil)
