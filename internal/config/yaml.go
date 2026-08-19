// Package config loads and validates sync-assign configuration.
package config

import (
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v4"
)

func decodeYAML(reader io.Reader, value any) error {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	if err := decoder.Decode(value); err != nil {
		return err
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple YAML documents are not allowed")
		}
		return fmt.Errorf("decode trailing YAML: %w", err)
	}
	return nil
}

func encodeYAML(writer io.Writer, value any) error {
	encoder := yaml.NewEncoder(writer)
	encoder.SetIndent(2)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	return encoder.Close()
}
