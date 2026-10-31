package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
)

// Write serializes s as indented JSON and writes it to path.
// The file is created with permissions 0600.
func Write(path string, s *Snapshot) error {
	if s == nil {
		return fmt.Errorf("%w: nil snapshot", ErrInvalidSnapshot)
	}
	if s.FormatVersion != FormatVersion {
		return fmt.Errorf("%w %q", ErrUnsupportedFormatVersion, s.FormatVersion)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling snapshot: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing snapshot file %q: %w", path, err)
	}

	return nil
}
