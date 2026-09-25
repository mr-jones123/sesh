// Package registry lists the harnesses sesh can import from and export to.
package registry

import (
	"fmt"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/harness/claude"
	"github.com/mr-jones123/sesh/internal/harness/codex"
	"github.com/mr-jones123/sesh/internal/harness/pi"
)

func Adapters() []harness.Adapter {
	return []harness.Adapter{pi.New(), claude.New(), codex.New()}
}

// Exporters lists targets whose native transcripts sesh can write. A target
// is added only after its output is verified against the real harness.
func Exporters() []harness.Exporter {
	return []harness.Exporter{pi.New()}
}

func Find(name string) (harness.Adapter, error) {
	for _, adapter := range Adapters() {
		if adapter.Name() == name {
			return adapter, nil
		}
	}
	return nil, fmt.Errorf("unknown harness %q", name)
}

func FindExporter(name string) (harness.Exporter, error) {
	for _, exporter := range Exporters() {
		if exporter.Name() == name {
			return exporter, nil
		}
	}
	return nil, fmt.Errorf("unsupported target %q; supported: pi", name)
}

func Detect(path string) (harness.Adapter, error) {
	for _, adapter := range Adapters() {
		if adapter.Detect(path) {
			return adapter, nil
		}
	}
	return nil, fmt.Errorf("could not detect harness for %q; use --harness", path)
}
