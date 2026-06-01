package proxy

import "testing"

func TestFallbackUUIDv4_TruncatesNodeSegment(t *testing.T) {
	got := fallbackUUIDv4(0x123456789abcdef)
	want := "00000000-0000-4000-8000-456789abcdef"
	if got != want {
		t.Fatalf("fallbackUUIDv4() = %q, want %q", got, want)
	}
}
