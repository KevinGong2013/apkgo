package store

import (
	"fmt"
	"sort"
	"strings"
)

type entry struct {
	factory            Factory
	environmentFactory EnvironmentFactory
	schema             ConfigSchema
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

// RegisterWithEnvironment registers a store whose credentials or endpoint
// depend on the selected remote environment.
func RegisterWithEnvironment(name string, schema ConfigSchema, f EnvironmentFactory) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("store %q already registered", name))
	}
	registry[name] = entry{environmentFactory: f, schema: schema}
}

// Create instantiates a store by name with the given config.
// Supports "type.instance" naming: e.g. "script.cdn-upload" resolves to
// the "script" store type with instance name "cdn-upload".
func Create(name string, cfg map[string]string) (Store, error) {
	return CreateForEnvironment(name, cfg, EnvironmentProduction)
}

// CreateForEnvironment instantiates a store for the requested environment.
// Regular factories remain production-configured; the caller is responsible
// for not executing them in sandbox mode.
func CreateForEnvironment(name string, cfg map[string]string, environment Environment) (Store, error) {
	e, instance, ok := lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown store: %q", name)
	}

	storeCfg := make(map[string]string, len(cfg)+1)
	for key, value := range cfg {
		storeCfg[key] = value
	}
	if instance != "" {
		storeCfg["_name"] = instance
	}

	if e.environmentFactory != nil {
		return e.environmentFactory(storeCfg, environment)
	}
	return e.factory(storeCfg)
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

// AcceptsAAB reports whether the named store has declared AAB support
// in its ConfigSchema. The "type.instance" naming convention used for
// multi-instance stores (e.g. "script.cdn-upload") is resolved to the
// base type before lookup. Unknown names return false.
func AcceptsAAB(name string) bool {
	e, ok := registry[name]
	if !ok {
		if dot := strings.Index(name, "."); dot > 0 {
			e, ok = registry[name[:dot]]
		}
	}
	return ok && e.schema.AcceptsAAB
}

// SupportsScheduledRelease reports whether the named store declared
// scheduled-release (定时发布) support in its ConfigSchema. Uses the same
// "type.instance" resolution as AcceptsAAB. Unknown names return false.
func SupportsScheduledRelease(name string) bool {
	e, ok := registry[name]
	if !ok {
		if dot := strings.Index(name, "."); dot > 0 {
			e, ok = registry[name[:dot]]
		}
	}
	return ok && e.schema.SupportsScheduledRelease
}

// SupportsURLPush reports whether the named store declared download-mode
// (pull-from-URL) support in its ConfigSchema. Same "type.instance"
// resolution as AcceptsAAB. Unknown names return false.
func SupportsURLPush(name string) bool {
	e, ok := registry[name]
	if !ok {
		if dot := strings.Index(name, "."); dot > 0 {
			e, ok = registry[name[:dot]]
		}
	}
	return ok && e.schema.SupportsURLPush
}

// SupportsSandbox reports whether the named store has an isolated sandbox
// API. Unknown stores and regular production-only stores return false.
func SupportsSandbox(name string) bool {
	e, _, ok := lookup(name)
	return ok && e.schema.SupportsSandbox
}

func lookup(name string) (entry, string, bool) {
	if e, ok := registry[name]; ok {
		return e, "", true
	}
	if dot := strings.Index(name, "."); dot > 0 {
		if e, ok := registry[name[:dot]]; ok {
			return e, name[dot+1:], true
		}
	}
	return entry{}, "", false
}
