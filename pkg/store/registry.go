package store

import (
	"fmt"
	"sort"
)

type entry struct {
	factory Factory
	schema  ConfigSchema
}

var registry = map[string]entry{}

// Register adds a store implementation to the global registry.
// Each store package calls this in its init() function.
func Register(name string, schema ConfigSchema, f Factory) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("store %q already registered", name))
	}
	registry[name] = entry{factory: f, schema: schema}
}

// Create instantiates a store by name with the given config.
func Create(name string, cfg map[string]string) (Store, error) {
	e, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown store: %q", name)
	}
	return e.factory(cfg)
}

// Schemas returns the config schemas for all registered stores, sorted by name.
func Schemas() []ConfigSchema {
	schemas := make([]ConfigSchema, 0, len(registry))
	for _, e := range registry {
		schemas = append(schemas, e.schema)
	}
	sort.Slice(schemas, func(i, j int) bool {
		return schemas[i].Name < schemas[j].Name
	})
	return schemas
}

// Names returns sorted list of registered store names.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
