package visuals

import (
	"fmt"
	"sync/atomic"
	"time"
)

// UUID strategy helpers for WorldStateStore services.
//
// The Viam 3D viewer accepts two patterns for the on-wire uuid of
// each Transform:
//
//   - "stable":    every entity keeps its UUID for life. Animations
//                  push UPDATED events with field-mask paths.
//                  Matches the RDK fake.
//   - "versioned": every tick allocates a fresh UUID (timestamp +
//                  monotonic counter suffix), emits REMOVED for the
//                  prior version and ADDED for the new one. Matches
//                  the apriltag-tracker pattern. Use this when the
//                  renderer drops UPDATED events for stable UUIDs.
//
// The renderer also caches REMOVED UUIDs and silently drops
// subsequent ADDED events for the same UUID — any animation that
// mutates scene-graph membership (lifecycle, flicker) needs to
// rotate the UUID even in stable strategy. VersionedUUID below is
// the canonical UUID generator; callers responsible for choosing
// when to use it.

// ValidStrategies lists the accepted uuid_strategy config values.
var ValidStrategies = []string{"stable", "versioned"}

// Module-global atomic counter shared across goroutines. Combined
// with epoch ms it guarantees uniqueness even when multiple UUIDs
// are allocated within the same millisecond.
var uuidCounter int64

// InitialUUID returns the UUID for an entity at install time,
// given the service's strategy. "stable" → label bytes (identity
// stays human-readable). "versioned" → a fresh timestamp-suffixed
// UUID via VersionedUUID.
func InitialUUID(label, strategy string) []byte {
	if strategy == "versioned" {
		return VersionedUUID(label)
	}
	return []byte(label)
}

// VersionedUUID allocates a fresh UUID for label of the form
// "<label>_<epoch_ms>_<counter>". Used by the versioned strategy
// and by REMOVED→ADDED transitions in stable strategy when the
// entity's scene-graph membership changes (the renderer caches
// REMOVED UUIDs and drops subsequent ADDEDs for the same one).
func VersionedUUID(label string) []byte {
	c := atomic.AddInt64(&uuidCounter, 1)
	return []byte(fmt.Sprintf("%s_%d_%d", label, time.Now().UnixMilli(), c))
}
