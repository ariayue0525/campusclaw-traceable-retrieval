package server

import (
	"bytes"
	"errors"
	"testing"
)

type memoryFile struct { *bytes.Reader }
func (memoryFile) Close() error { return nil }

func TestReadLimited(t *testing.T) {
	b, err := readLimited(memoryFile{bytes.NewReader([]byte("hello"))}, 5)
	if err != nil || string(b) != "hello" { t.Fatalf("unexpected result %q %v", b, err) }
	_, err = readLimited(memoryFile{bytes.NewReader([]byte("toolarge"))}, 4)
	if !errors.Is(err, errTooLarge) { t.Fatalf("expected errTooLarge, got %v", err) }
}

func TestRandomTokenIsUnique(t *testing.T) {
	a, err := randomToken(); if err != nil { t.Fatal(err) }
	b, err := randomToken(); if err != nil { t.Fatal(err) }
	if a == b || len(a) < 40 { t.Fatalf("tokens are not sufficiently random-looking") }
}

func TestSameOrigin(t *testing.T) {
	if !sameOrigin("http://localhost:8080", "localhost:8080") { t.Fatal("expected same origin") }
	if sameOrigin("https://attacker.example", "localhost:8080") { t.Fatal("cross origin must fail") }
}

func TestFilenameEncodingRemovesPath(t *testing.T) {
	got := urlEncodeFilename("../../lesson plan.md")
	if got != "lesson%20plan.md" { t.Fatalf("unexpected encoded filename %q", got) }
}

