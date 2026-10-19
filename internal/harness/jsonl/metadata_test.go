package jsonl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanMetadataBoundsLargeRecordsAndContinues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	large := `{"type":"message","message":{"content":"` + strings.Repeat("x", metadataPrefixLimit*3) + `"}}`
	data := large + "\n{unrelated malformed tail\n" + `{"type":"custom-title","customTitle":"Renamed \"session\""}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	var title string
	var sawTruncated bool
	err := ScanMetadata(context.Background(), path, func(line MetadataLine) error {
		sawTruncated = sawTruncated || line.Truncated
		if kind, _ := StringField(line.Prefix, "type"); kind == "custom-title" {
			title, _ = StringField(line.Prefix, "customTitle")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawTruncated || title != `Renamed "session"` {
		t.Fatalf("truncated = %v, title = %q", sawTruncated, title)
	}
}

func TestStringFieldOnlyReadsTopLevelStrings(t *testing.T) {
	raw := []byte(`{"payload":{"type":"nested"},"type" : "title","title":"A\\B"}`)
	if got, ok := StringField(raw, "type"); !ok || got != "title" {
		t.Fatalf("type = %q, %v", got, ok)
	}
	if got, ok := StringField(raw, "title"); !ok || got != `A\B` {
		t.Fatalf("title = %q, %v", got, ok)
	}
}

func TestStringFieldDecodesJSONEscapes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "escaped slash", raw: `{"title":"path\/to\/file"}`, want: "path/to/file"},
		{name: "surrogate pair", raw: `{"title":"Launch \ud83d\ude80"}`, want: "Launch 🚀"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := StringField([]byte(test.raw), "title")
			if !ok || got != test.want {
				t.Fatalf("title = %q, %v; want %q", got, ok, test.want)
			}
		})
	}
}
