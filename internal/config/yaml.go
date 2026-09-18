// Package config loads and validates sync-assign configuration.
package config

import (
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v4"
)

func (spec *AssignmentSpec) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if err := node.Decode(&spec.Path); err != nil {
			return err
		}
		spec.Archetype = nil
		return nil
	case yaml.MappingNode:
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode || (key.Value != "path" && key.Value != "archetype") {
				return fmt.Errorf("field %s not found in type config.AssignmentSpec", key.Value)
			}
		}
		type assignmentSpec AssignmentSpec
		var decoded assignmentSpec
		if err := node.Decode(&decoded); err != nil {
			return err
		}
		*spec = AssignmentSpec(decoded)
		return nil
	default:
		return fmt.Errorf("assignment must be a path scalar or mapping")
	}
}

func (spec AssignmentSpec) MarshalYAML() (any, error) {
	if spec.Archetype == nil {
		return spec.Path, nil
	}
	type assignmentSpec AssignmentSpec
	return assignmentSpec(spec), nil
}

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
