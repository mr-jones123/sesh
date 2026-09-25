package importer

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

func Find(name string) (harness.Adapter, error) {
	for _, adapter := range Adapters() {
		if adapter.Name() == name {
			return adapter, nil
		}
	}
	return nil, fmt.Errorf("unknown harness %q", name)
}

func Detect(path string) (harness.Adapter, error) {
	for _, adapter := range Adapters() {
		if adapter.Detect(path) {
			return adapter, nil
		}
	}
	return nil, fmt.Errorf("could not detect harness for %q; use --harness", path)
}
