package bypass

import (
	"strings"
	"testing"
)

func TestHostlist_Contains(t *testing.T) {
	hl := NewHostlist()
	if err := hl.load(strings.NewReader("youtube.com\n# comment\nvk.com\n")); err != nil {
		t.Fatalf("load: %v", err)
	}

	cases := []struct {
		domain string
		want   bool
	}{
		{"youtube.com", true},
		{"www.youtube.com", true}, // parent match
		{"api.vk.com", true},      // parent match
		{"vk.com", true},
		{"google.com", false},
		{"notinyoutube.com", false}, // no partial suffix match
		{"", false},
	}

	for _, tc := range cases {
		got := hl.Contains(tc.domain)
		if got != tc.want {
			t.Errorf("Contains(%q) = %v, want %v", tc.domain, got, tc.want)
		}
	}
}

func TestHostlist_ShouldBypass_Disabled(t *testing.T) {
	hl := NewHostlist()
	_ = hl.load(strings.NewReader("youtube.com\n"))
	hl.Enable(false) // disabled → bypass everything

	// When filtering is off, all domains should be bypassed.
	if !hl.ShouldBypass("google.com") {
		t.Error("ShouldBypass should return true when filtering is disabled")
	}
}

func TestHostlist_ShouldBypass_Enabled(t *testing.T) {
	hl := NewHostlist()
	_ = hl.load(strings.NewReader("youtube.com\n"))
	hl.Enable(true)

	if !hl.ShouldBypass("youtube.com") {
		t.Error("ShouldBypass should return true for listed domain")
	}
	if hl.ShouldBypass("github.com") {
		t.Error("ShouldBypass should return false for unlisted domain")
	}
}

func TestHostlist_Size(t *testing.T) {
	hl := NewHostlist()
	if hl.Size() != 0 {
		t.Errorf("fresh hostlist size = %d, want 0", hl.Size())
	}
	_ = hl.load(strings.NewReader("a.com\nb.com\nc.com\n"))
	if hl.Size() != 3 {
		t.Errorf("size = %d, want 3", hl.Size())
	}
}

func TestHostlist_CaseInsensitive(t *testing.T) {
	hl := NewHostlist()
	_ = hl.load(strings.NewReader("YouTube.com\n"))

	if !hl.Contains("youtube.com") {
		t.Error("Contains should be case-insensitive")
	}
	if !hl.Contains("YOUTUBE.COM") {
		t.Error("Contains should be case-insensitive for query too")
	}
}

func TestHostlist_TrailingDot(t *testing.T) {
	hl := NewHostlist()
	_ = hl.load(strings.NewReader("example.com\n"))

	// DNS fully-qualified names may end with a trailing dot.
	if !hl.Contains("example.com.") {
		t.Error("Contains should handle trailing dot in query domain")
	}
}

func TestHostlist_EmptyAndComments(t *testing.T) {
	hl := NewHostlist()
	err := hl.load(strings.NewReader("\n# just a comment\n   \n#another\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if hl.Size() != 0 {
		t.Errorf("size = %d after loading only comments/blanks, want 0", hl.Size())
	}
}

func TestHostlist_Reload(t *testing.T) {
	hl := NewHostlist()
	_ = hl.load(strings.NewReader("old.com\n"))

	// Reload with new set — old entries must be gone.
	_ = hl.load(strings.NewReader("new.com\n"))

	if hl.Contains("old.com") {
		t.Error("old.com should not be present after reload")
	}
	if !hl.Contains("new.com") {
		t.Error("new.com should be present after reload")
	}
}
