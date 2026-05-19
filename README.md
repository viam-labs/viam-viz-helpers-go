# viam-viz-helpers-go

Typed scene constructors for Viam world-state-store services and the 3D scene viewer (Go).

The Viam 3D viewer subscribes to `rdk:service:world_state_store` services for transform updates. The wire format is a closed set of geometry primitives plus a custom metadata schema, with quirks around field-mask paths, UUID rotation, and PCD encoding that aren't obvious from the proto definitions. This library lets module authors construct scenes from typed Go structs (`Box`, `Sphere`, `Pose`, `Color`, `Animation`, …) and embed a ready-made WSS service base, while the library handles the wire format and renderer-side quirks underneath.

- **GitHub:** [viam-labs/viam-viz-helpers-go](https://github.com/viam-labs/viam-viz-helpers-go)
- **Module path:** `github.com/viam-labs/viam-viz-helpers-go`
- **Package name:** `visuals` (used as `visuals.Box`, `visuals.Scene`, …)
- **Python sibling:** [viam-labs/viam-viz-helpers-python](https://github.com/viam-labs/viam-viz-helpers-python)
- **License:** Apache-2.0

## Install

```
go get github.com/viam-labs/viam-viz-helpers-go@main
```

Then in your module:

```go
import "github.com/viam-labs/viam-viz-helpers-go"
// used as visuals.Box, visuals.Scene, etc.
```

Pre-1.0 — there are no tagged releases yet. Pin to a SHA via `go.mod` if you need reproducibility. For local iteration against a consuming module, drop a `go.work` at the consumer's repo root:

```
go 1.25.1

use (
    .
    ../viam-viz-helpers-go
)
```

## Quickstart

A minimal world-state-store service that publishes three primitives and animates one:

```go
package mymodule

import (
    "context"
    "math"

    "go.viam.com/rdk/resource"
    "go.viam.com/rdk/services/worldstatestore"

    "github.com/viam-labs/viam-viz-helpers-go"
)

type myScene struct {
    resource.Named
    visuals.SceneServiceBase

    bobber *visuals.Sphere
}

func (s *myScene) Reconfigure(_ context.Context, _ resource.Dependencies, _ resource.Config) error {
    green := visuals.Color{R: 60, G: 180, B: 75}
    red := visuals.Color{R: 230, G: 25, B: 75}
    blue := visuals.Color{R: 0, G: 130, B: 200}

    s.bobber = &visuals.Sphere{
        Label:    "bobber",
        Pose:     visuals.PoseAt(300, 0, 100, 0, 0, 1, 0),
        RadiusMM: 80,
        Color:    &green,
    }
    return s.SetScene(visuals.SetSceneOpts{},
        &visuals.Box{
            Label:  "demo_box",
            Pose:   visuals.PoseAt(-200, 0, 100, 0, 0, 1, 0),
            DimsMM: visuals.BoxDims{X: 150, Y: 150, Z: 150},
            Color:  &red,
        },
        s.bobber,
        &visuals.Capsule{
            Label:    "demo_capsule",
            Pose:     visuals.PoseAt(100, 0, 100, 0, 0, 1, 0),
            RadiusMM: 50, LengthMM: 200,
            Color: &blue,
        },
    )
}

// SceneTick is the per-tick animation hook (visuals.SceneTicker
// interface). Mutate typed Visuals in place; return Scene.Update
// events.
func (s *myScene) SceneTick(scene *visuals.Scene, t float64) []visuals.SceneEvent {
    s.bobber.Pose = visuals.SpinPose(
        visuals.PoseAt(300, 0, 100, 0, 0, 1, 0), 3.0, t,
    )
    events, _ := scene.Update(s.bobber)
    return events
}
```

That's the full surface for a static-plus-one-animated scene. `SceneServiceBase` provides everything else: `ListUUIDs` / `GetTransform` / `StreamTransformChanges`, subscriber fanout, the tick goroutine, and a standard DoCommand verb set (`list` / `clear` / `snapshot` / `apply_events` / …).

For a fuller example exercising every primitive type, every animation mode, presets, and the driver→visualizer split, see [`viam-labs/example-visualizations-go`](https://github.com/viam-labs/example-visualizations-go).

## Architecture

### Typed object graph

Scenes are built from typed Visual types (`Box`, `Sphere`, `Capsule`, `Point`, `Frame`, `Arrow`, `Mesh`, `PointCloud`). Each carries the common fields (`Label`, `Pose`, `ParentFrame`, `Color`, `Opacity`, `ShowAxesHelper`, `Invisible`, `Animation`) plus shape-specific fields (`DimsMM`, `RadiusMM`, `LengthMM`, `MeshPath`, …).

**Pointer convention:** pass `&visuals.Box{...}` (pointer), not `visuals.Box{...}` (value), to `Scene.Add`. The Scene stores the Visual interface, but `Animation.Apply` type-switches on the concrete pointer types (`*Box`, `*Sphere`, …) to mutate state in place. Value-type Visuals fall through the switches silently — animations would not move them.

Composites (`CoordinateFrame`, `Line`, `BoundingBox`, `TrajectoryPlan`) expand into the underlying primitives at `Scene.Add` time. `TrajectoryPlan` is the motion-plan-shaped composite — pass a list of `Pose` waypoints (matches CBiRRT / motion-service output after forward kinematics), get back a polyline plus per-waypoint coordinate-frame triads.

### Animation specs

Eleven typed `AnimationSpec` implementations cover the common cases: `Static`, `Spin`, `Swing`, `Oscillate`, `Orbit`, `Pulse`, `Breathe`, `Flicker`, `Lifecycle`, `ForceVector`, `Trajectory`. Attach one to a Visual's `Animation` field; the library's `DefaultSceneTick` dispatches `Apply(visual, base, t)` every tick. `base` is a snapshot of the Visual at install time so animation math composes onto a stable rest state.

### Hook surface

`SceneHooks` is an empty marker (`type SceneHooks = any`). Each capability the library uses is an **optional interface** that the Hooks instance may or may not satisfy — the library type-asserts at the call site and falls back to a built-in default:

| Interface | When implemented | Library fallback |
| --- | --- | --- |
| `GeometryBuilder` | Module publishes primitives beyond the standard set (mesh, pointcloud, custom). | `BuildBasicGeometry` — handles box/sphere/capsule/point/arrow. |
| `BaseGeomProvider` | Same as above. | `DefaultBaseGeomForItem` — extracts shape fields for the standard set. |
| `AssetReader` | Module loads mesh / pointcloud / texture assets. | Error: "no asset reader configured". |
| `PresetLoader` | Module exposes named scene presets via the `preset` DoCommand verb. | Error: "no preset loader configured". |
| `CustomCommandHandler` | Module adds DoCommand verbs beyond the standard set. | Pass-through; debug-snapshot default reply. |
| `SceneTicker` | Module animates by mutating typed Visuals each tick (recommended). | Legacy `ComputeTick` path or no animation. |
| `LegacyAnimator` | Module uses the deprecated per-item `ComputeTick` path. | Skipped if `SceneTicker` is also implemented. |

Modules that publish only static or `Animation`-driven scenes of standard primitives can embed `SceneServiceBase`, set `Hooks = s` (where `s` implements `SceneTicker`), and be done — no other hooks required.

### Scene mutation

```go
scene := visuals.NewScene("world")
events, _ := scene.Add(box, sphere)            // []SceneEvent{kind:EventAdded, ...}
sphere.RadiusMM = 120
events, _ = scene.Update(sphere)               // []SceneEvent{kind:EventUpdated, paths:[...]}
scene.Remove("sphere_label")                   // []SceneEvent{kind:EventRemoved, ...}
```

`Scene.Update` diffs the visual's current `ToItem()` against the committed snapshot and produces UPDATED events with camelCase field-mask paths. When the diff touches metadata (color, opacity, parent_frame, show_axes_helper, invisible), `Scene.Update` emits an UPDATED with `Paths=[]` — the **respawn signal**. The renderer drops `metadata.*` on UPDATED, so the library translates the empty-paths event into REMOVE + re-ADD with a fresh UUID downstream.

### In-process registry

When two resources live in the same module binary, a downstream resource can hold a direct Go reference to an upstream resource via `visuals.Register` / `visuals.Lookup`, skipping the gRPC stub the framework's `Dependencies` injects. See `registry.go` for the API and the driver-visualizer pair in `example-visualizations-go` for the canonical pattern.

## Renderer quirks (load-bearing)

These are not opinions; they are properties of the current viewer's actual behavior, distilled from incident reports in the Python sibling's [`LESSONS.md`](https://github.com/viam-labs/example-visualizations-python/blob/main/LESSONS.md):

- **Field-mask paths must be camelCase.** The viewer ignores snake_case paths silently. The `Path*` constants in this package are the source of truth.
- **`metadata.*` paths are dropped on UPDATED.** The library compensates by escalating metadata diffs to a REMOVE+ADD respawn (see `Scene.Update`).
- **The renderer caches REMOVED UUIDs.** Re-adding the same UUID is silently dropped. `VersionedUUID` rotates UUIDs to dodge the cache; `Flicker` / `Lifecycle` / metadata-respawn paths use this.
- **PCD bytes must match `pointcloud.ToPCD` byte-for-byte.** Binary, `VERSION .7` literal, no leading `#` comments.
- **Mesh/PCD coordinates are in METERS.** RDK readers multiply by 1000 to convert to the internal mm convention.
- **Only PLY meshes render.** STL is auto-converted to PLY in `STLToPLY`; raw STL is silently dropped by the viewer.
- **In-process DoCommand preserves concrete slice types** (`[]string`, `[]map[string]any`); gRPC erases them to `[]any`. `coerceStringSlice` / `coerceEventsSlice` handle both shapes so `apply_events` works either way.

## Status

Pre-1.0. The API is stable enough for the consuming `example-visualizations-go` module but versioned releases are not yet tagged — pin to a SHA in `go.mod` if you need reproducibility. CI is an open follow-up.

Issues, suggestions, renderer-behavior reports: [open one](https://github.com/viam-labs/viam-viz-helpers-go/issues).
