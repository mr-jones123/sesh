package session

import (
	"bytes"
	"testing"
)

func TestBundleRoundTripKeepsRawLinesExact(t *testing.T) {
	line := `{"b": 1,  "a": "<tag> & 1.50", "n": 1e3}`
	var encoded bytes.Buffer
	err := EncodeBundle(&encoded, Bundle{
		Session:    Session{ID: "s1", Harness: "pi"},
		RawRecords: []RawLine{{Line: 1, Record: line}},
	})
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeBundle(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.RawRecords[0].Record; got != line {
		t.Fatalf("record = %q, want %q", got, line)
	}
}

func TestDecodeBundleRejectsOldFormatVersion(t *testing.T) {
	_, err := DecodeBundle(bytes.NewBufferString(`{"format_version":1,"session":{"id":"s1","harness":"pi","events":[]}}`))
	if err == nil {
		t.Fatal("DecodeBundle accepted format version 1")
	}
}
