package visuals

import "fmt"

// Visual is the typed scene-item interface: anything that can produce
// an Item (the wire-format value type). Each primitive struct (Box,
// Sphere, Capsule, Point, Frame, Arrow, Mesh, PointCloud) implements
// it via a value receiver.
//
// Pointer convention: pass pointers to the Scene
// (&visuals.Box{...}, not visuals.Box{...}). The Scene stores the
// Visual interface but Animation.Apply mutates the underlying struct
// in place via type-switch on the concrete pointer type (*Box,
// *Sphere, …). Value-type Visuals fall through silently — animations
// would not move them. The shape constructors and composite
// expansions in this package all return pointer types for this
// reason.
type Visual interface {
	ToItem() Item
}

// ToItems materializes a slice of Visuals into Items. Convenience
// for callers that build visuals positionally and flush to the wire
// format the service consumes.
func ToItems(vs ...Visual) []Item {
	out := make([]Item, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.ToItem())
	}
	return out
}

// ---- Primitive types ---------------------------------------------------
//
// Each primitive carries the common Visual fields (Label, Pose,
// ParentFrame, Color, Opacity, ShowAxesHelper, Invisible, Animation)
// plus its shape-specific fields. Duplicating the common fields per
// struct (rather than embedding) keeps struct-literal call sites
// idiomatic — no nested initialization paths.
//
// Pointer-field conventions:
//   - Color *Color, Opacity *float64: nil means "use the renderer's
//     default" (gray, fully opaque). Take the address of a stack-
//     allocated value: c := visuals.Color{R: 255}; box.Color = &c.
//   - Animation AnimationSpec: interface, nil = static. Set to a
//     concrete spec value (Spin{...}, Pulse{...}, …) and the
//     library's SceneTick dispatch will call Apply on every tick.

// Box is a solid axis-aligned box. DimsMM (x, y, z) gives the
// extent in millimeters; all three dimensions must be > 0.
type Box struct {
	Label          string
	Pose           Pose
	ParentFrame    string
	Color          *Color
	Opacity        *float64
	ShowAxesHelper bool
	Invisible      bool
	Animation      AnimationSpec
	DimsMM         BoxDims
}

// ToItem implements Visual.
func (b Box) ToItem() Item {
	must(b.Label != "", "Box requires Label")
	must(b.DimsMM.X > 0 && b.DimsMM.Y > 0 && b.DimsMM.Z > 0,
		"Box.DimsMM must all be > 0; got %v", b.DimsMM)
	return Item{
		Type: "box", Label: b.Label,
		Pose: fillPose(b.Pose), ParentFrame: b.ParentFrame,
		HasDims: true, DimsMM: b.DimsMM,
		Color: b.Color, Opacity: b.Opacity,
		ShowAxesHelper: b.ShowAxesHelper, Invisible: b.Invisible,
		Animation: animOf(b.Animation),
	}
}

// Sphere is a solid sphere of the given radius in millimeters.
// RadiusMM must be > 0.
type Sphere struct {
	Label          string
	Pose           Pose
	ParentFrame    string
	Color          *Color
	Opacity        *float64
	ShowAxesHelper bool
	Invisible      bool
	Animation      AnimationSpec
	RadiusMM       float64
}

// ToItem implements Visual.
func (s Sphere) ToItem() Item {
	must(s.Label != "", "Sphere requires Label")
	must(s.RadiusMM > 0, "Sphere.RadiusMM must be > 0; got %v", s.RadiusMM)
	return Item{
		Type: "sphere", Label: s.Label,
		Pose: fillPose(s.Pose), ParentFrame: s.ParentFrame,
		RadiusMM: s.RadiusMM,
		Color:    s.Color, Opacity: s.Opacity,
		ShowAxesHelper: s.ShowAxesHelper, Invisible: s.Invisible,
		Animation: animOf(s.Animation),
	}
}

// Capsule is a cylinder with hemispherical end caps. RadiusMM is the
// cylinder radius; LengthMM is the total tip-to-tip length (including
// both hemispherical caps). The local +Z axis is the long axis.
// Both fields must be > 0.
type Capsule struct {
	Label          string
	Pose           Pose
	ParentFrame    string
	Color          *Color
	Opacity        *float64
	ShowAxesHelper bool
	Invisible      bool
	Animation      AnimationSpec
	RadiusMM       float64
	LengthMM       float64
}

// ToItem implements Visual.
func (c Capsule) ToItem() Item {
	must(c.Label != "", "Capsule requires Label")
	must(c.RadiusMM > 0, "Capsule.RadiusMM must be > 0; got %v", c.RadiusMM)
	must(c.LengthMM > 0, "Capsule.LengthMM must be > 0; got %v", c.LengthMM)
	return Item{
		Type: "capsule", Label: c.Label,
		Pose: fillPose(c.Pose), ParentFrame: c.ParentFrame,
		RadiusMM: c.RadiusMM, LengthMM: c.LengthMM,
		Color: c.Color, Opacity: c.Opacity,
		ShowAxesHelper: c.ShowAxesHelper, Invisible: c.Invisible,
		Animation: animOf(c.Animation),
	}
}

// Point is a marker point — a position with no extent.
//
// The wire format has no first-class Point primitive; this is
// internally rendered as a small sphere whose radius is fixed by
// the service implementation's BuildGeometry hook (the default
// BuildBasicGeometry uses PointMarkerRadiusMM = 8). A zero-radius
// sphere would render as nothing in the viewer, so the radius is
// not user-configurable here.
type Point struct {
	Label          string
	Pose           Pose
	ParentFrame    string
	Color          *Color
	Opacity        *float64
	ShowAxesHelper bool
	Invisible      bool
	Animation      AnimationSpec
}

// ToItem implements Visual.
func (p Point) ToItem() Item {
	must(p.Label != "", "Point requires Label")
	return Item{
		Type: "point", Label: p.Label,
		Pose: fillPose(p.Pose), ParentFrame: p.ParentFrame,
		Color: p.Color, Opacity: p.Opacity,
		ShowAxesHelper: p.ShowAxesHelper, Invisible: p.Invisible,
		Animation: animOf(p.Animation),
	}
}

// Frame is a pure transform anchor — a reference frame other
// Visuals can parent to without rendering anything visible itself.
//
// Use it to declare hierarchy: place a Frame at the position you
// want to be the "joint" or "pivot", then give other Visuals
// ParentFrame=<frame.Label>. Moving the Frame transports the
// children with it; the renderer composes the parent transform
// automatically.
//
// Internally a tiny sphere with Invisible=true. ShowAxesHelper
// defaults to true so the anchor's pose is visible during
// development. Set Invisible=false to render the sphere body too.
//
// Example:
//
//	pivot := &visuals.Frame{Label: "pivot", Pose: visuals.PoseAt(500, 0, 300, 0, 0, 1, 0)}
//	child := &visuals.Box{
//	    Label: "child", Pose: visuals.PoseAt(80, 0, 0, 0, 0, 1, 0),
//	    DimsMM: visuals.BoxDims{X: 40, Y: 40, Z: 40},
//	    ParentFrame: "pivot",
//	}
//	// Rotate the pivot; the child rotates with it:
//	pivot.Pose = visuals.PoseAt(500, 0, 300, 0, 0, 1, 45)
//	events, _ := scene.Update(pivot)
type Frame struct {
	Label       string
	Pose        Pose
	ParentFrame string

	// Visible: render the underlying 1 mm sphere body. Defaults to
	// false (anchor invisible — only the axes helper paints).
	Visible bool
	// HideAxes: hide the renderer's built-in axes helper. Defaults
	// to false (axes shown). Combined with Visible=false, this
	// produces a fully-invisible anchor — useful for sub-anchors
	// in a deep hierarchy where you don't want a wall of triads.
	HideAxes bool
}

// ToItem implements Visual.
func (f Frame) ToItem() Item {
	must(f.Label != "", "Frame requires Label")
	return Item{
		Type: "sphere", Label: f.Label,
		Pose: fillPose(f.Pose), ParentFrame: f.ParentFrame,
		RadiusMM:       1.0,
		Invisible:      !f.Visible,
		ShowAxesHelper: !f.HideAxes,
	}
}

// Arrow is a procedural cylinder-shaft + cone-tip mesh along the
// entity's local +Z axis.
//
// LengthMM is the total tail-to-tip length; RadiusMM is the shaft
// radius (the cone tip is a fixed multiple of this). To point an
// arrow from one world point to another, use ArrowFromTo — it
// computes the orientation vector and length for you.
type Arrow struct {
	Label          string
	Pose           Pose
	ParentFrame    string
	Color          *Color
	Opacity        *float64
	ShowAxesHelper bool
	Invisible      bool
	Animation      AnimationSpec
	LengthMM       float64
	RadiusMM       float64
}

// ToItem implements Visual.
func (a Arrow) ToItem() Item {
	must(a.Label != "", "Arrow requires Label")
	must(a.LengthMM > 0, "Arrow.LengthMM must be > 0; got %v", a.LengthMM)
	must(a.RadiusMM > 0, "Arrow.RadiusMM must be > 0; got %v", a.RadiusMM)
	return Item{
		Type: "arrow", Label: a.Label,
		Pose: fillPose(a.Pose), ParentFrame: a.ParentFrame,
		LengthMM: a.LengthMM, RadiusMM: a.RadiusMM,
		Color: a.Color, Opacity: a.Opacity,
		ShowAxesHelper: a.ShowAxesHelper, Invisible: a.Invisible,
		Animation: animOf(a.Animation),
	}
}

// Mesh is a mesh loaded from a PLY or STL asset on disk.
//
// MeshPath is resolved by the service's AssetReader hook (the
// library doesn't open files); pass a path relative to the module
// directory or whatever convention the service implements. STL is
// auto-converted to PLY at load time via STLToPLY because the
// viewer only renders PLY meshes — see LESSONS.md::mesh-formats in
// the consuming example module.
//
// Set RawSTL=true to deliberately bypass the STL→PLY conversion
// and ship raw STL bytes with content_type="stl". The proto and
// RDK reader accept this, but the viewer silently drops it. This
// field exists solely to reproduce that bug; production callers
// must leave it false.
type Mesh struct {
	Label          string
	Pose           Pose
	ParentFrame    string
	Color          *Color
	Opacity        *float64
	ShowAxesHelper bool
	Invisible      bool
	Animation      AnimationSpec
	MeshPath       string
	RawSTL         bool
}

// ToItem implements Visual.
func (m Mesh) ToItem() Item {
	must(m.Label != "", "Mesh requires Label")
	must(m.MeshPath != "", "Mesh requires MeshPath")
	return Item{
		Type: "mesh", Label: m.Label,
		Pose: fillPose(m.Pose), ParentFrame: m.ParentFrame,
		MeshPath: m.MeshPath, RawSTL: m.RawSTL,
		Color: m.Color, Opacity: m.Opacity,
		ShowAxesHelper: m.ShowAxesHelper, Invisible: m.Invisible,
		Animation: animOf(m.Animation),
	}
}

// PointCloud is a point cloud loaded from a PCD asset.
//
// PCD bytes must match RDK's pointcloud.ToPCD format byte-for-byte
// (binary, "VERSION .7" literal, no leading "#" comments). The
// viewer's parser is strict-order; ASCII / binary_compressed / extra
// comments silently fail. See ParsePCDBinary in pcd_io.go for what
// the library accepts.
//
// Setting Chunked=true with a positive ChunkSize opts into
// experimental chunked delivery: the service ships chunk 0 inline
// in the initial transform with a metadata.chunks sub-struct, and
// exposes the rest via the get_entity_chunk DoCommand verb. The
// viewer's behavior on this contract is unverified — whether it
// fetches subsequent chunks is open. Leave Chunked=false unless
// specifically probing the chunked path.
type PointCloud struct {
	Label          string
	Pose           Pose
	ParentFrame    string
	Color          *Color
	Opacity        *float64
	ShowAxesHelper bool
	Invisible      bool
	Animation      AnimationSpec
	PointcloudPath string
	Chunked        bool
	ChunkSize      int
}

// ToItem implements Visual.
func (pc PointCloud) ToItem() Item {
	must(pc.Label != "", "PointCloud requires Label")
	must(pc.PointcloudPath != "", "PointCloud requires PointcloudPath")
	if pc.Chunked {
		must(pc.ChunkSize > 0,
			"PointCloud.ChunkSize must be > 0 when Chunked=true; got %v", pc.ChunkSize)
	}
	return Item{
		Type: "pointcloud", Label: pc.Label,
		Pose: fillPose(pc.Pose), ParentFrame: pc.ParentFrame,
		PointcloudPath: pc.PointcloudPath,
		Chunked:        pc.Chunked, ChunkSize: pc.ChunkSize,
		Color: pc.Color, Opacity: pc.Opacity,
		ShowAxesHelper: pc.ShowAxesHelper, Invisible: pc.Invisible,
		Animation: animOf(pc.Animation),
	}
}

// must panics with a formatted message when cond is false. Used by
// ToItem / ToAnimation to surface construction errors immediately
// instead of at wire-encode time.
func must(cond bool, format string, args ...any) {
	if !cond {
		panic(fmt.Sprintf(format, args...))
	}
}
