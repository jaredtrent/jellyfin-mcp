package jellyfin

import (
	"log"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

// SchemaFor generates a cleaned JSON schema for T with unnecessary "null" union
// types stripped from optional properties.
func SchemaFor[T any]() *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		log.Fatalf("schema generation failed: %v", err)
	}
	cleanSchema(schema)
	return schema
}

// WithEnums generates a cleaned JSON schema for T and applies enum constraints
// to the specified fields.
func WithEnums[T any](enums map[string][]any) *jsonschema.Schema {
	return WithEnumsFiltered[T](enums, nil)
}

// WithEnumsFiltered generates a cleaned JSON schema for T, optionally keeping
// only the JSON property names in keepProps (when non-nil), and applies enum
// constraints to the specified fields.
func WithEnumsFiltered[T any](enums map[string][]any, keepProps []string) *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		log.Fatalf("schema generation failed: %v", err)
	}
	if keepProps != nil && schema.Properties != nil {
		keep := make(map[string]bool, len(keepProps))
		for _, k := range keepProps {
			keep[k] = true
		}
		for k := range schema.Properties {
			if !keep[k] {
				delete(schema.Properties, k)
			}
		}
		if len(schema.Required) > 0 {
			filtered := schema.Required[:0]
			for _, r := range schema.Required {
				if keep[r] {
					filtered = append(filtered, r)
				}
			}
			schema.Required = filtered
		}
	}
	for field, values := range enums {
		if prop, ok := schema.Properties[field]; ok {
			prop.Enum = slices.Clone(values)
		}
	}
	cleanSchema(schema)
	return schema
}

// cleanSchema recursively removes redundant "null" union types on optional
// object properties (those not listed in Required). Optional fields carry
// omitempty and are omitted rather than serialized as null, while required
// slices/maps keep their nullable union so zero-value validation succeeds.
func cleanSchema(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	for _, def := range s.Defs {
		cleanSchema(def)
	}
	if s.Items != nil {
		cleanSchema(s.Items)
	}
	if s.AdditionalProperties != nil {
		cleanSchema(s.AdditionalProperties)
	}
	if len(s.Properties) == 0 {
		return
	}
	req := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		req[r] = true
	}
	for name, prop := range s.Properties {
		if prop == nil {
			continue
		}
		cleanSchema(prop)
		if !req[name] && len(prop.Types) == 2 && slices.Contains(prop.Types, "null") {
			for _, t := range prop.Types {
				if t != "null" {
					prop.Type = t
					prop.Types = nil
					break
				}
			}
		}
	}
}
