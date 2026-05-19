// In-process resource registry.
//
// Lets a downstream resource hold a direct Go reference to an
// upstream resource that lives in the same module binary, skipping
// the gRPC round-trip the framework's Dependencies stubs use.
//
// This matters when two resources ship from the same module and
// exchange events at tick rate: a 30Hz tick × 50 events × cross-
// process gRPC adds measurable latency and CPU. A direct method call
// is essentially free.
//
// Usage on the upstream side (e.g. a visualizer service that wants
// to be addressable in-process):
//
//	import "github.com/viam-labs/viam-viz-helpers-go/visuals"
//
//	func newVisualizer(...) (worldstatestore.Service, error) {
//	    s := &visualizer{...}
//	    if err := s.Reconfigure(...); err != nil { return nil, err }
//	    visuals.Register(conf.ResourceName().Name, s)
//	    return s, nil
//	}
//
//	func (s *visualizer) Close(ctx context.Context) error {
//	    visuals.Unregister(s.Name().Name)
//	    return s.SceneServiceBase.Close(ctx)
//	}
//
// Usage on the downstream side (e.g. a driver sensor):
//
//	vis := visuals.Lookup(cfg.Visualizer)
//	if vis == nil {
//	    // Visualizer lives in another module — fall back to the
//	    // gRPC stub the framework injected via Dependencies.
//	    raw, _ := deps[worldstatestore.Named(cfg.Visualizer)]
//	    vis = raw.(worldstatestore.Service)
//	}
//
// The registry is module-binary local: each Go module process
// gets its own map. Modules that ship visualizer and driver as the
// same binary share it; a separately-shipped driver module won't see
// the upstream and naturally falls through to its gRPC stub.
package visuals

import (
	"sort"
	"sync"
)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]interface{})
)

// Register stores instance under name. Replaces any prior
// registration with the same name (mirrors the framework's
// behaviour on reconfigure — a fresh constructor produces a fresh
// instance).
func Register(name string, instance interface{}) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = instance
}

// Unregister removes name from the registry. No-op if not present.
func Unregister(name string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(registry, name)
}

// Lookup returns the instance registered under name, or nil if it
// isn't registered (typically: lives in a different module process).
func Lookup(name string) interface{} {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[name]
}

// RegisteredNames returns the currently-registered names, sorted.
// Useful for debugging and the snapshot DoCommand verb.
func RegisteredNames() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
