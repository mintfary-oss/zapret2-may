package logs

import (
	"fmt"
	"testing"
	"time"
)

func TestRing_WriteAndRecent(t *testing.T) {
	r := NewRing(5)

	for i := 0; i < 3; i++ {
		if _, err := fmt.Fprintf(r, "line %d\n", i); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	entries := r.Recent(10)
	if len(entries) != 3 {
		t.Fatalf("Recent(10) = %d entries, want 3", len(entries))
	}
	for i, e := range entries {
		want := fmt.Sprintf("line %d", i)
		if e.Message != want {
			t.Errorf("entries[%d].Message = %q, want %q", i, e.Message, want)
		}
	}
}

func TestRing_Capacity(t *testing.T) {
	// Fill a ring of capacity 3 with 5 entries; only the last 3 must survive.
	r := NewRing(3)
	for i := 0; i < 5; i++ {
		fmt.Fprintf(r, "msg %d\n", i) //nolint:errcheck
	}

	entries := r.Recent(10)
	if len(entries) != 3 {
		t.Fatalf("len(Recent) = %d, want 3 (capacity)", len(entries))
	}
	// Last 3 written should be: msg 2, msg 3, msg 4.
	for i, e := range entries {
		want := fmt.Sprintf("msg %d", i+2)
		if e.Message != want {
			t.Errorf("entries[%d] = %q, want %q", i, e.Message, want)
		}
	}
}

func TestRing_RecentN(t *testing.T) {
	r := NewRing(10)
	for i := 0; i < 6; i++ {
		fmt.Fprintf(r, "entry %d\n", i) //nolint:errcheck
	}

	got := r.Recent(3)
	if len(got) != 3 {
		t.Fatalf("Recent(3) = %d, want 3", len(got))
	}
	// Should be the last 3: entry 3, entry 4, entry 5.
	expected := []string{"entry 3", "entry 4", "entry 5"}
	for i, e := range got {
		if e.Message != expected[i] {
			t.Errorf("Recent(3)[%d] = %q, want %q", i, e.Message, expected[i])
		}
	}
}

func TestRing_EmptyRecent(t *testing.T) {
	r := NewRing(10)
	entries := r.Recent(5)
	if len(entries) != 0 {
		t.Errorf("Recent on empty ring = %d, want 0", len(entries))
	}
}

func TestRing_Size(t *testing.T) {
	r := NewRing(4)
	if r.Size() != 0 {
		t.Errorf("fresh ring size = %d, want 0", r.Size())
	}
	fmt.Fprintf(r, "a\n") //nolint:errcheck
	if r.Size() != 1 {
		t.Errorf("size after 1 write = %d, want 1", r.Size())
	}
}

func TestRing_StripNewline(t *testing.T) {
	r := NewRing(5)
	r.Write([]byte("hello\n")) //nolint:errcheck

	entries := r.Recent(1)
	if len(entries) == 0 {
		t.Fatal("no entries after Write")
	}
	if entries[0].Message != "hello" {
		t.Errorf("Message = %q, want %q (no trailing newline)", entries[0].Message, "hello")
	}
}

func TestRing_EmptyWrite(t *testing.T) {
	r := NewRing(5)
	r.Write([]byte("")) //nolint:errcheck
	r.Write([]byte("\n"))
	if r.Size() != 0 {
		t.Errorf("empty/newline-only writes should not add entries; size = %d", r.Size())
	}
}

func TestRing_Timestamp(t *testing.T) {
	before := time.Now()
	r := NewRing(5)
	r.Write([]byte("ts-test\n")) //nolint:errcheck
	after := time.Now()

	entries := r.Recent(1)
	if len(entries) == 0 {
		t.Fatal("no entries")
	}
	ts := entries[0].Time
	if ts.Before(before) || ts.After(after) {
		t.Errorf("entry time %v not in [%v, %v]", ts, before, after)
	}
}

func TestRing_Subscribe(t *testing.T) {
	r := NewRing(10)
	ch := r.Subscribe()
	defer r.Unsubscribe(ch)

	r.Write([]byte("broadcast\n")) //nolint:errcheck

	select {
	case e := <-ch:
		if e.Message != "broadcast" {
			t.Errorf("subscriber got %q, want %q", e.Message, "broadcast")
		}
	case <-time.After(time.Second):
		t.Error("subscriber did not receive entry within 1s")
	}
}

func TestRing_UnsubscribeCloses(t *testing.T) {
	r := NewRing(5)
	ch := r.Subscribe()
	r.Unsubscribe(ch)

	// Channel must be closed after Unsubscribe.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed after Unsubscribe")
		}
	default:
		t.Error("channel should be readable (closed) after Unsubscribe")
	}
}

func TestRing_MultipleSubscribers(t *testing.T) {
	r := NewRing(10)
	ch1 := r.Subscribe()
	ch2 := r.Subscribe()
	defer r.Unsubscribe(ch1)
	defer r.Unsubscribe(ch2)

	r.Write([]byte("multi\n")) //nolint:errcheck

	for i, ch := range []chan Entry{ch1, ch2} {
		select {
		case e := <-ch:
			if e.Message != "multi" {
				t.Errorf("subscriber %d got %q, want %q", i+1, e.Message, "multi")
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d did not receive entry within 1s", i+1)
		}
	}
}
