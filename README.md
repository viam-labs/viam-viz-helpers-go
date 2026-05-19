# visuals — typed visual scene constructors for Viam (Go)

A Go library for building Viam world-state-store scenes from typed
Go structs instead of hand-built wire-format maps. Sibling of the
Python [`viam_visuals`](https://github.com/viam-labs/example-visualizations-python/tree/main/viam_visuals)
package.

This directory is the bootstrap in-tree version. When extracted, the
target import path is **`github.com/viam-labs/viam-viz-helpers-go/visuals`**.

## Quickstart

```go
import "github.com/viam-labs/viam-viz-helpers-go/visuals"

box := &visuals.Box{
    Label:  "demo_box",
    DimsMM: visuals.BoxDims{X: 100, Y: 200, Z: 50},
    Color:  &visuals.Color{R: 230, G: 25, B: 75},
    Opacity: ptr(0.8),
}

scene := visuals.NewScene("world")
events, _ := scene.Add(box)

box.DimsMM.X = 150
events, _ = scene.Update(box)  // diffs, emits camelCase field-mask paths
```

Or embed `visuals.SceneServiceBase` and get a ready-made WSS service
with the standard DoCommand verbs (list/clear/preset/snapshot/
set_uuid_strategy/`apply_events`), animation tick loop, and
subscriber fanout for free.

## Public API surface

### Pose, Color
- `Pose`, `IdentityPose()`, `PoseXYZ(x,y,z)`, `PoseAt(...)`,
  `LerpPose(a, b, t)`
- `Color`, `BoxDims`

`LerpPose` does true quaternion SLERP on orientation (linear on
position). Conversions match RDK's `spatialmath.QuatToOV` exactly so
the renderer reconstructs continuous rotations across the OV
singularity at `|OZ| = 1`.

### Shapes
- `Visual` (interface) — `Box`, `Sphere`, `Capsule`, `Point`, `Arrow`,
  `Mesh`, `PointCloud`
- `ToItems(...)` — convert to wire format

### Animations
- `AnimationSpec` (interface) — `Static`, `Spin`, `Swing`, `Oscillate`,
  `Orbit`, `Pulse`, `Breathe`, `Flicker`, `Lifecycle`, `ForceVector`,
  `Trajectory`
- `Animation` runtime struct; `IsAnimated(anim)` check

### Composites
- `Composite` (interface) — `CoordinateFrame`, `Line`, `BoundingBox`,
  `TrajectoryPlan`, `ArrowFromTo`

`TrajectoryPlan` matches motion-planner output (CBiRRT, RRT*,
motion-service): pass a list of `Pose` waypoints, get a polyline plus
per-waypoint coordinate-frame triads.

### Scene
- `Scene`, `NewScene(parentFrame)` — `Add`, `Update`, `AddOrUpdate`,
  `Remove`, `Clear`, `Get`, `Labels`
- `SceneEvent` with `Kind` ∈ `EventAdded`/`EventUpdated`/`EventRemoved`
- `EventsToWire(events)` — for the `apply_events` DoCommand

### Service base
- `SceneServiceBase` — embeddable WSS service; subclasses implement
  the `SceneHooks` interface for module-specific bits

### UUID strategy
- `InitialUUID`, `VersionedUUID`, `ValidStrategies`

### In-process registry
- `Register(name, resource)`, `Lookup(name)`, `Unregister(name)`,
  `RegisteredNames()`

### Path constants (field-mask)
- `PathTheta`, `PathX/Y/Z`, `PathOX/OY/OZ`, `PathSphereRadius`,
  `PathCapsule{Radius,Length}`, `PathBoxDims{X,Y,Z}`,
  `PathMetadataColor`, `PathMetadataOpac`

## Reference module

[`viam-labs/example-visualizations-go`](https://github.com/viam-labs/example-visualizations-go)
is the canonical first adopter. Three models in one binary demonstrate
the library's architecture: `standalone-playground`,
`playground-visualizer`, `playground-driver`.

## Conventions and gotchas

Read [`../LESSONS.md`](https://github.com/viam-labs/example-visualizations-python/blob/main/LESSONS.md)
(in the Python sibling repo) — every finding has file:line evidence
from the renderer's actual behavior. Highlights:

- Field-mask paths MUST be camelCase, not snake_case.
- PCD headers must match `pointcloud.ToPCD` byte-for-byte.
- Mesh/PCD file coordinates are in METERS, not millimeters.
- The viewer renders only PLY meshes (STL → PLY at load time).
- The renderer caches REMOVED UUIDs — re-add events for the same
  label silently drop.
- Go in-process DoCommand preserves concrete slice types
  (`[]string`, `[]map[string]any`); gRPC erases them to `[]any`.
  `coerceStringSlice` / `coerceEventsSlice` handle both shapes.

## License

Apache 2.0 — see [LICENSE](../LICENSE).
