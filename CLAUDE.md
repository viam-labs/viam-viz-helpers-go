# CLAUDE.md — viam-viz-helpers-go

Operational context for agents working in this repository. The user-facing entry point is `README.md`; this file is the load-bearing knowledge an agent needs **before** writing code or proposing changes.

## What this is

`viam-viz-helpers-go` is a Go library that wraps the Viam world-state-store wire format. It is consumed by Viam modules that publish geometries to the 3D scene viewer (`rdk:service:world_state_store`).

- **Repo:** `viam-labs/viam-viz-helpers-go` (public, Apache-2.0).
- **Module path:** `github.com/viam-labs/viam-viz-helpers-go`.
- **Package name:** `visuals` (at module root — `import "github.com/viam-labs/viam-viz-helpers-go"` is used as `visuals.Foo`).
- **Python sibling:** `viam-labs/viam-viz-helpers-python` — same architecture, same wire format. Cross-language behavior should stay in lockstep.
- **Canonical consumer:** [`viam-labs/example-visualizations-go`](https://github.com/viam-labs/example-visualizations-go). Its sibling [`example-visualizations-python/LESSONS.md`](https://github.com/viam-labs/example-visualizations-python/blob/main/LESSONS.md) is the renderer-behavior incident log — read it before debugging anything wire-format-shaped.

## File layout

```
shapes.go              # Visual interface + Box/Sphere/Capsule/Point/Frame/Arrow/Mesh/PointCloud structs.
pose.go                # Pose struct + Pose helpers + fillPose + LerpPose (quaternion SLERP).
color.go               # Color, BoxDims, HSVToRGB, SnapStep.
animations.go          # AnimationSpec interface + 11 concrete specs + Animation/Overrides/BaseGeom/TickResult.
anim_apply.go          # Apply methods on each AnimationSpec (the typed mutate-in-place path).
anim_helpers.go        # Pure pose-composing helpers (SpinPose, OrbitPose, etc.).
composites.go          # Composite interface + CoordinateFrame/Line/BoundingBox/TrajectoryPlan + ArrowFromTo.
scene.go               # Scene + SceneEvent + diff logic.
service.go             # SceneServiceBase — embeddable WSS service. Owns state/subscribers/tick/DoCommand.
wire.go                # ItemFromMap (wire-format map → Item) + EventsToWire (SceneEvent → wire).
registry.go            # In-process resource registry (Register/Lookup/Unregister/RegisteredNames).
uuid_strategy.go       # InitialUUID / VersionedUUID / ValidStrategies.
item.go                # Item — the wire-format value struct (intermediate between Visual and proto).
basic_geometry.go      # BuildBasicGeometry + DefaultBaseGeomForItem — the GeometryBuilder/BaseGeomProvider default.
metadata.go            # MetadataOpts + BuildMetadata (the visualization library schema).
mesh_io.go             # STL→PLY conversion + PLY vertex-color extraction + ArrowPLYBytes (procedural arrow mesh).
pcd_io.go              # ParsePCDBinary + BuildPCDChunk (RDK-byte-exact PCD format + chunked splitter).
internal/              # Pure-data constants used by mesh / PCD helpers.

*_test.go              # `go test ./...`
go.mod / go.sum
README.md              # User-facing surface.
CLAUDE.md              # This file.
```

## Architecture

### Two halves: data layer + service layer

1. **Data layer** (everything except `service.go`). Typed structs, pure functions, no RDK service dependency. Build a Scene, mutate Visuals, call `scene.Update(...)` to get diff events. Usable standalone for testing scene logic or constructing wire-format payloads to send to a remote service.

2. **Service layer** (`service.go`'s `SceneServiceBase`). Embed it to inherit `ListUUIDs` / `GetTransform` / `StreamTransformChanges`, the subscriber fanout, the animation tick goroutine, and the standard DoCommand verb set.

### Scene-centric animation (recommended path)

```go
type myScene struct {
    resource.Named
    visuals.SceneServiceBase

    box *visuals.Box
}

func (s *myScene) Reconfigure(_ context.Context, _ resource.Dependencies, _ resource.Config) error {
    s.box = &visuals.Box{
        Label: "demo", DimsMM: visuals.BoxDims{X: 100, Y: 100, Z: 100},
        Animation: visuals.Spin{PeriodS: 3},
    }
    return s.SetScene(visuals.SetSceneOpts{}, s.box)
}

func (s *myScene) SceneTick(scene *visuals.Scene, t float64) []visuals.SceneEvent {
    return s.DefaultSceneTick(scene, t) // iterates scene, calls Animation.Apply
}
```

`scene.Update(s.box)` diffs the visual's current `ToItem()` against the snapshot taken at `SetScene` time. Three outcomes:

- **No change**: no event.
- **Pose/geom change**: UPDATED with `Paths=[...]` (camelCase).
- **Metadata change** (color, opacity, parent_frame, show_axes_helper, invisible): UPDATED with `Paths=[]` — the **respawn signal**. The renderer drops `metadata.*` on UPDATED, so the library translates empty-paths UPDATEDs into REMOVE+ADD with a fresh `VersionedUUID` downstream. See `service.go::applyEvents` for the wire-layer translation.

### Pointer-vs-value: the silent landmine

`Visual` is an interface. `Scene.Add` stores interface values. `Animation.Apply` uses a type-switch on **pointer types** (`*Box`, `*Sphere`, …) to mutate state in place:

```go
func setVisualPose(v Visual, p Pose) {
    switch x := v.(type) {
    case *Box:    x.Pose = p
    case *Sphere: x.Pose = p
    // ...
    }
}
```

If you pass `visuals.Box{...}` (value) to `Scene.Add` instead of `&visuals.Box{...}` (pointer), the switch falls through silently. Animation runs every tick but never mutates anything; you see a static entity and wonder why. **Always pass pointers.** Composites' `ToVisuals()` returns `[]Visual` with pointer-typed elements specifically for this reason.

### Optional interfaces

`type SceneHooks = any` — there is no required hook contract. Every capability is an **optional interface** the library type-asserts at the call site, with a built-in fallback when the assertion fails:

| Interface | When to implement | Library fallback |
| --- | --- | --- |
| `GeometryBuilder` | Module publishes primitives beyond box/sphere/capsule/point/arrow (mesh, pointcloud, custom). | `BuildBasicGeometry` — handles the standard five. |
| `BaseGeomProvider` | Same as above. | `DefaultBaseGeomForItem` — extracts shape fields. |
| `AssetReader` | Module loads mesh / pointcloud / texture assets. | Returns error: "no asset reader". |
| `PresetLoader` | Module exposes named scene presets via the `preset` DoCommand. | Returns error: "no preset loader". |
| `CustomCommandHandler` | Module adds DoCommand verbs beyond the standard set. | Falls through to the default debug-snapshot reply. |
| `SceneTicker` | Module animates by mutating typed Visuals each tick (recommended). | Legacy `ComputeTick` path, or no animation. |
| `LegacyAnimator` | Pre-existing module using the deprecated per-item `ComputeTick`. | Skipped if `SceneTicker` is also satisfied. |

`SimpleSceneExample` in the consuming module implements **only** `SceneTicker`. Most modules need 2-3 interfaces at most.

### Legacy ComputeTick path

`LegacyAnimator.ComputeTick(item, basePose, baseGeom, t)` returns a `TickResult{Pose, Geom, Paths, Overrides}`. The library handles wire emission. Pre-existing modules using this still work; the library checks for `SceneTicker` first and falls back to `LegacyAnimator` only if not found. New code should use `SceneTicker`.

### In-process registry

`visuals.Register` / `visuals.Lookup` is a thread-safe singleton map keyed by resource name. Use it when two resources live in the same module binary and exchange events at tick rate — a direct method call is free; the framework's gRPC stub is not. See `registry.go` and the driver-visualizer pair in `example-visualizations-go` for the canonical pattern.

## Renderer quirks (DO NOT FORGET)

These are properties of the current Viam 3D viewer's actual behavior. The library's correctness depends on respecting them. Cross-reference [`example-visualizations-python/LESSONS.md`](https://github.com/viam-labs/example-visualizations-python/blob/main/LESSONS.md) for file:line evidence on each.

- **Field-mask paths must be camelCase.** The viewer's `updateEntity` handler ignores snake_case silently — animations using snake_case paths produce no visible motion. The `Path*` constants in `animations.go` are camelCase; never edit them to snake_case without confirming the viewer accepts it. The example module's 0.0.32 release broke every animation by trying this.
- **`metadata.*` paths are dropped on UPDATED.** The viewer's `updateEntity` only handles `poseInObserverFrame.pose.*` and `physicalObject.*` prefixes; `metadata.*` is silently dropped. Color / opacity / show_axes_helper / invisible changes must be communicated via REMOVE+ADD respawn (handled automatically by `Scene.Update` + `applyEvents`).
- **The renderer caches REMOVED UUIDs.** Re-adding an entity with the same UUID after REMOVED silently no-ops. `VersionedUUID` rotates UUIDs on every metadata-respawn / flicker / lifecycle re-add to dodge this. Never re-use a UUID after REMOVED.
- **PCD bytes must match `pointcloud.ToPCD` byte-for-byte.** Binary format, `VERSION .7` (literal, not `0.7`), no leading `#` comments. The viewer's parser is strict-order; the RDK reader is lax.
- **Mesh/PCD file coordinates are in METERS.** RDK readers multiply by 1000 internally. Writing raw mm into a PLY/PCD file makes the renderer draw it 1000× too big.
- **Only PLY meshes render.** STL is parsed (RDK accepts `content_type="stl"`) but the viewer drops it silently. `Mesh` auto-converts STL→PLY at load time via `STLToPLY`. `Mesh{RawSTL: true}` opts out to reproduce the bug; never use this in production.
- **`Transform.metadata` uses the `viamrobotics/visualization` schema.** Five required keys: `colors`, `color_format`, `opacities`, `show_axes_helper`, `invisible`. The library's `BuildMetadata` emits this correctly. The RDK fake at `services/worldstatestore/fake/moving_geos_world.go` uses the obsolete shape — do not copy from it.
- **In-process DoCommand preserves concrete slice types.** When a driver in the same module process calls `visualizer.DoCommand(...)`, `[]string` stays `[]string` and `[]map[string]any` stays itself. Over gRPC, `structpb` erases both to `[]any`. The library's `coerceStringSlice` / `coerceEventsSlice` in `service.go` handle both shapes. A naive type assertion like `evt["paths"].([]any)` would silently fail in-process, drop the field-mask, and produce UPDATEDs with no `UpdatedFields` — a wire-level no-op.

## API design conventions

- **Struct literals at call sites.** Every Visual / Composite / AnimationSpec is a plain struct with public fields. `&visuals.Box{Label: "x", DimsMM: visuals.BoxDims{X: 100}}` is the canonical construction form. No `NewBox(...)` constructors — they don't pay for themselves in Go.
- **Pointer-field semantics: `nil` means "use the default".** `Color *Color`, `Opacity *float64` — nil means renderer-default. Take the address of a stack-allocated value: `c := visuals.Color{R: 255}; box.Color = &c`. The `must(...)` validator in `shapes.go::ToItem` panics on missing required fields (Label, positive radius / length / dims); it does not require Color or Opacity.
- **Value-receiver `ToItem()`; mutate via pointer.** Every Visual struct has a value receiver `ToItem()` so it can be passed by value when only the wire form is needed. The `Apply` dispatch type-switches on pointer types because mutation needs to flow back to the Scene's stored interface value. See `anim_apply.go` for the switch tables.
- **Composites return `[]Visual` with pointer elements.** `CoordinateFrame.ToVisuals()` returns `[]Visual{&Sphere{...}, &Arrow{...}, ...}`. Value returns would break animation; never change this without auditing every composite.

## Tests

```
go test ./...
```

All library tests are co-located with the source (`scene_test.go`, `pose_test.go`, …). The service layer is exercised more fully by the consuming `example-visualizations-go` module — significant changes to `service.go` should run that module's test suite too:

```
cd ../example-visualizations-go
# With go.work pointing here, go.mod's @main require is shadowed:
echo 'use ( . ../viam-viz-helpers-go )' > go.work
go work init . ../viam-viz-helpers-go    # if not already initialized
go test ./...
```

## Don't

- **Don't change `Path*` constants to snake_case.** The 0.0.32 example module release broke every animation by trying this. The viewer's actual behavior requires camelCase regardless of what the worldstatestore proto guide says.
- **Don't bypass `VersionedUUID` on REMOVE+ADD cycles.** The renderer caches REMOVED UUIDs; re-using the same UUID after REMOVED produces an invisible entity.
- **Don't return value-type Visuals from `ToVisuals()`.** Animation.Apply type-switches on pointers; value returns fall through silently and the composite's constituents won't animate.
- **Don't break the pointer convention in shape constructors.** All examples should pass `&visuals.Box{...}` to `Scene.Add` / `SetScene`. Value-form usage is technically legal (Scene stores the interface either way) but disables animation silently — surprising failure mode.
- **Don't add features that change the wire format** (new metadata keys, new geometry types) without confirming the viewer accepts them. The `viamrobotics/visualization` repo is the canonical source — NOT the RDK fake.
- **Don't introduce a runtime dependency on the consuming example module.** This library is upstream of the example modules; the example modules import the library, never the reverse.
- **Don't tag a release** until the v1.0 API decision is made. Pre-1.0 consumers pin to a SHA in `go.mod`.

## Cross-language parity

The Python sibling (`viam-labs/viam-viz-helpers-python`) implements the same architecture, same wire format, same renderer-quirk workarounds. Behavioral changes should land in both libraries together. Discrepancies are bugs.

- Go uses `Visual` interface + concrete struct pointers; Python uses dataclass inheritance. Different idioms, same intent.
- Go's `SceneHooks = any` plus optional interface assertions; Python's `SceneServiceBase` plus subclass method overrides. Different language affordances, same surface area.
- Animation math should produce bit-identical poses at the same `t` across languages (modulo floating-point ordering). If you're chasing a Python/Go discrepancy in a consuming module, this is the place to check first.

## Releasing notes

No tagged releases yet — main is the only consumable ref. When v1.0 is cut, add an entry here per release with the date, the API-affecting changes, and any renderer-quirk discoveries that drove the bump.
