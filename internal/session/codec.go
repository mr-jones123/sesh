package session

import (
	"encoding/json"
	"fmt"
	"io"
)

func Decode(reader io.Reader) (Session, error) {
	var result Session
	if err := json.NewDecoder(reader).Decode(&result); err != nil {
		return Session{}, fmt.Errorf("decode session: %w", err)
	}
	if result.ID == "" {
		return Session{}, fmt.Errorf("session id is required")
	}
	if result.Harness == "" {
		return Session{}, fmt.Errorf("session harness is required")
	}
	return result, nil
}

func Encode(writer io.Writer, result Session) error {
	if result.ID == "" {
		return fmt.Errorf("session id is required")
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func DecodeBundle(reader io.Reader) (Bundle, error) {
	var bundle Bundle
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return Bundle{}, fmt.Errorf("decode bundle: %w", err)
	}
	if bundle.FormatVersion != CurrentFormatVersion {
		return Bundle{}, fmt.Errorf("unsupported bundle format version %d", bundle.FormatVersion)
	}
	if bundle.Session.ID == "" {
		return Bundle{}, fmt.Errorf("bundle session id is required")
	}
	if bundle.Session.Harness == "" {
		return Bundle{}, fmt.Errorf("bundle session harness is required")
	}
	return bundle, nil
}

func EncodeBundle(writer io.Writer, bundle Bundle) error {
	if bundle.FormatVersion == 0 {
		bundle.FormatVersion = CurrentFormatVersion
	}
	if bundle.FormatVersion != CurrentFormatVersion {
		return fmt.Errorf("unsupported bundle format version %d", bundle.FormatVersion)
	}
	if bundle.Session.ID == "" {
		return fmt.Errorf("bundle session id is required")
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(bundle)
}
