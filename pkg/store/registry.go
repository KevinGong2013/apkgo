package store

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
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

// CreateContext is Create under ctx. What ctx carries for a store's
// lifetime is picked up here — today the HTTP recorder of
// httptrace.WithRecorder: every request the store then makes, including
// the sign-in its constructor performs, is recorded under name.
func CreateContext(ctx context.Context, name string, cfg map[string]string) (Store, error) {
	return CreateForEnvironmentContext(ctx, name, cfg, EnvironmentProduction)
}

// CreateForEnvironment instantiates a store for the requested environment.
// Regular factories remain production-configured; the caller is responsible
// for not executing them in sandbox mode.
func CreateForEnvironment(name string, cfg map[string]string, environment Environment) (Store, error) {
	return CreateForEnvironmentContext(context.Background(), name, cfg, environment)
}

// CreateForEnvironmentContext is CreateForEnvironment under ctx; see
// CreateContext.
func CreateForEnvironmentContext(ctx context.Context, name string, cfg map[string]string, environment Environment) (Store, error) {
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
	storeCfg, release := httptrace.Carry(ctx, name, storeCfg)
	defer release()

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

// Platform reports the package platform the named store accepts —
// PlatformHarmony for HarmonyOS-only stores, PlatformAndroid otherwise
// (including for unknown names, so callers can treat the answer as a
// plain string compare). Same "type.instance" resolution as AcceptsAAB.
func Platform(name string) string {
	e, _, ok := lookup(name)
	if ok && e.schema.Platform != "" {
		return e.schema.Platform
	}
	return PlatformAndroid
}

// SupportsSandbox reports whether the named store has an isolated sandbox
// API. Unknown stores and regular production-only stores return false.
func SupportsSandbox(name string) bool {
	e, _, ok := lookup(name)
	return ok && e.schema.SupportsSandbox
}

// Known reports whether name is a registered store, directly or as a
// "type.instance" name of one.
func Known(name string) bool {
	_, _, ok := lookup(name)
	return ok
}

// ListingSpecFor returns the listing (商店资料) spec the named store
// declared, or nil if it can't update its listing. Same "type.instance"
// resolution as AcceptsAAB.
func ListingSpecFor(name string) *ListingSpec {
	e, _, ok := lookup(name)
	if !ok {
		return nil
	}
	return e.schema.Listing
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
