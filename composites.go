package visuals

import (
	"fmt"
	"math"
)

// Composite is the contract for multi-Visual constructs. Each
// composite expands into a list of Visuals via ToVisuals — useful
// for the common patterns (coordinate-frame triad, polyline as
// capsule chain, wireframe bounding box) where a single typed
// object wraps the underlying multi-item shape.
//
// Scene.Add(composite) calls ToVisuals and installs each
// constituent independently; the composite itself is not tracked.
// Each composite's ToVisuals must return pointer-type Visuals
// (&Sphere{...}, &Arrow{...}, etc.) so animation.Apply can mutate
// them — value-type returns would fall through the pointer-only
// type switches in setVisualPose / setVisualColor / etc.
//
// Constituent labels are derived from the composite's Label or
// LabelPrefix field. Two composites whose constituents share a
// label collide at Scene.Add — pick distinct labels per composite.
type Composite interface {
	ToVisuals() []Visual
}

// CoordinateFrame is an anchor sphere plus three colored axis arrows
// parented to the anchor (a typed wrapper for the
// parent-frame-composition pattern).
//
// Use this whenever you'd otherwise hand-build a parent-anchor
// sphere plus three axis-aligned capsules — the
// parent-frame-composition pattern that powers reference_frame_demo.
//
// Animations attach to the anchor; the axes inherit motion through
// the parent-frame chain. The anchor's label is the user-supplied
// Label; axes use Label + "_axis_x" / "_y" / "_z".
// The three axes are Arrow primitives (cylindrical shaft + conical
// tip) so direction is visually obvious from the tip. Each arrow's
// tail sits at the frame origin, its tip pointing along the
// corresponding axis. AxisLengthMM and AxisRadiusMM parameterize
// the arrow size; per-axis colors can be customized independently.
//
// ShowAxesHelper defaults to true (nil pointer → on). The
// renderer's built-in RGB axes helper renders alongside the
// composite's explicit arrows — redundant when arrow colors match
// the standard R/G/B but useful when you've tinted them.
type CoordinateFrame struct {
	Label          string
	Pose           Pose
	SizeMM         float64
	ParentFrame    string
	Animation      AnimationSpec
	ShowAxesHelper *bool // nil → true (use ptrB(false) to disable)
	AnchorRadiusMM float64
	AxisRadiusMM   float64
	AxisLengthMM   float64 // 0 → use SizeMM
	AnchorColor    *Color
	AnchorOpacity  *float64
	AxisColorX     *Color
	AxisColorY     *Color
	AxisColorZ     *Color
	AxisOpacity    *float64
}

// ToVisuals expands the frame into [anchor, x, y, z].
func (cf CoordinateFrame) ToVisuals() []Visual {
	size := cf.SizeMM
	if size <= 0 {
		size = 100.0
	}
	axisLen := cf.AxisLengthMM
	if axisLen <= 0 {
		axisLen = size
	}
	anchorR := cf.AnchorRadiusMM
	if anchorR <= 0 {
		anchorR = 12.0
	}
	axisR := cf.AxisRadiusMM
	if axisR <= 0 {
		axisR = 8.0
	}
	anchorColor := cf.AnchorColor
	if anchorColor == nil {
		anchorColor = &Color{R: 120, G: 120, B: 120}
	}
	anchorOpacity := cf.AnchorOpacity
	if anchorOpacity == nil {
		anchorOpacity = ptrF(0.6)
	}
	xc := cf.AxisColorX
	if xc == nil {
		xc = &Color{R: 230, G: 25, B: 75}
	}
	yc := cf.AxisColorY
	if yc == nil {
		yc = &Color{R: 60, G: 180, B: 75}
	}
	zc := cf.AxisColorZ
	if zc == nil {
		zc = &Color{R: 0, G: 130, B: 200}
	}
	axisOpacity := cf.AxisOpacity
	if axisOpacity == nil {
		axisOpacity = ptrF(1.0)
	}
	showAxes := true
	if cf.ShowAxesHelper != nil {
		showAxes = *cf.ShowAxesHelper
	}

	// Pointer types throughout so SceneServiceBase.SceneTick →
	// Animation.Apply can mutate them in place. (Animation.Apply
	// type-switches on pointer receivers; value types would fall
	// through silently.)
	return []Visual{
		&Sphere{
			Label: cf.Label, Pose: cf.Pose, ParentFrame: cf.ParentFrame,
			RadiusMM: anchorR, Color: anchorColor, Opacity: anchorOpacity,
			ShowAxesHelper: showAxes, Animation: cf.Animation,
		},
		// Arrow tails sit at the frame origin; local +Z is the shaft
		// direction. Each axis arrow orients its local +Z along the
		// matching world axis.
		&Arrow{
			Label: cf.Label + "_axis_x", ParentFrame: cf.Label,
			Pose:     PoseAt(0, 0, 0, 1, 0, 0, 0),
			RadiusMM: axisR, LengthMM: axisLen,
			Color: xc, Opacity: axisOpacity,
		},
		&Arrow{
			Label: cf.Label + "_axis_y", ParentFrame: cf.Label,
			Pose:     PoseAt(0, 0, 0, 0, 1, 0, 0),
			RadiusMM: axisR, LengthMM: axisLen,
			Color: yc, Opacity: axisOpacity,
		},
		&Arrow{
			Label: cf.Label + "_axis_z", ParentFrame: cf.Label,
			// Default identity orientation (+Z up).
			Pose:     PoseAt(0, 0, 0, 0, 0, 1, 0),
			RadiusMM: axisR, LengthMM: axisLen,
			Color: zc, Opacity: axisOpacity,
		},
	}
}

// ptrB is an internal helper to take a bool by pointer.
func ptrB(v bool) *bool { return &v }

// Line — polyline drawn as a chain of capsule segments. The wire
// format has no first-class line primitive; this composite
// synthesizes one from capsules whose local +Z is aligned to each
// segment's direction.
//
// Points further apart than 1 µm (segment length >= 1e-6) get a
// capsule segment between them; closer-than-1µm pairs are skipped
// silently (a sub-µm capsule would render as nothing).
//
// WidthMM is the diameter of the line in mm; defaults to 4.0 when
// zero. Each segment's local +Z is aligned to the segment direction.
type Line struct {
	LabelPrefix string
	Points      []Pose
	WidthMM     float64
	ParentFrame string
	Color       *Color
	Opacity     *float64
}

// ToVisuals expands the polyline into a chain of *Capsule segments.
// Labels follow "<LabelPrefix>_seg_NN" (zero-padded, in point order,
// skipping any sub-µm segments). Panics if len(Points) < 2.
func (l Line) ToVisuals() []Visual {
	if len(l.Points) < 2 {
		panic(fmt.Sprintf("Line needs at least 2 points; got %d", len(l.Points)))
	}
	width := l.WidthMM
	if width <= 0 {
		width = 4.0
	}
	out := make([]Visual, 0, len(l.Points)-1)
	segIdx := 0
	for i := 0; i < len(l.Points)-1; i++ {
		a, b := l.Points[i], l.Points[i+1]
		dx, dy, dz := b.X-a.X, b.Y-a.Y, b.Z-a.Z
		segLen := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if segLen < 1e-6 {
			continue
		}
		out = append(out, &Capsule{
			Label:       fmt.Sprintf("%s_seg_%02d", l.LabelPrefix, segIdx),
			ParentFrame: l.ParentFrame,
			Pose: PoseAt(
				(a.X+b.X)/2.0, (a.Y+b.Y)/2.0, (a.Z+b.Z)/2.0,
				dx/segLen, dy/segLen, dz/segLen, 0,
			),
			RadiusMM: width / 2.0,
			LengthMM: segLen,
			Color:    l.Color,
			Opacity:  l.Opacity,
		})
		segIdx++
	}
	return out
}

// BoundingBox is an axis-aligned bounding box, either solid or
// wireframe.
//
// With Wireframe=false (default), produces a single solid *Box with
// the given DimsMM. With Wireframe=true, produces 12 capsule edges
// tracing the box outline — useful for object-detection overlays
// where you want bounds without occluding what's inside.
//
// Internal labels (wireframe mode): "<Label>_edge_NN" for the 12
// edges, indexed in (x, y, z) order (4 X-edges, then 4 Y-edges,
// then 4 Z-edges; the NN suffix is zero-padded).
//
// EdgeRadiusMM controls the wireframe stroke thickness; defaults
// to 2.0 when zero. DimsMM must be all > 0.
type BoundingBox struct {
	Label        string
	DimsMM       BoxDims
	Pose         Pose
	ParentFrame  string
	Wireframe    bool
	Color        *Color
	Opacity      *float64
	EdgeRadiusMM float64
}

// ToVisuals expands the bounding box per the Wireframe flag.
func (bb BoundingBox) ToVisuals() []Visual {
	must(bb.DimsMM.X > 0 && bb.DimsMM.Y > 0 && bb.DimsMM.Z > 0,
		"BoundingBox.DimsMM must all be > 0; got %v", bb.DimsMM)

	if !bb.Wireframe {
		return []Visual{&Box{
			Label: bb.Label, Pose: bb.Pose, ParentFrame: bb.ParentFrame,
			DimsMM: bb.DimsMM, Color: bb.Color, Opacity: bb.Opacity,
		}}
	}

	dx, dy, dz := bb.DimsMM.X, bb.DimsMM.Y, bb.DimsMM.Z
	hx, hy, hz := dx/2, dy/2, dz/2
	edgeR := bb.EdgeRadiusMM
	if edgeR <= 0 {
		edgeR = 2.0
	}
	out := make([]Visual, 0, 12)
	i := 0
	add := func(p Pose, length float64) {
		out = append(out, &Capsule{
			Label:       fmt.Sprintf("%s_edge_%02d", bb.Label, i),
			ParentFrame: bb.ParentFrame,
			Pose:        p,
			RadiusMM:    edgeR, LengthMM: length,
			Color: bb.Color, Opacity: bb.Opacity,
		})
		i++
	}
	// 4 X-edges.
	for _, sy := range []float64{-1, 1} {
		for _, sz := range []float64{-1, 1} {
			add(PoseAt(0, sy*hy, sz*hz, 1, 0, 0, 0), dx)
		}
	}
	// 4 Y-edges.
	for _, sx := range []float64{-1, 1} {
		for _, sz := range []float64{-1, 1} {
			add(PoseAt(sx*hx, 0, sz*hz, 0, 1, 0, 0), dy)
		}
	}
	// 4 Z-edges.
	for _, sx := range []float64{-1, 1} {
		for _, sy := range []float64{-1, 1} {
			add(PoseAt(sx*hx, sy*hy, 0, 0, 0, 1, 0), dz)
		}
	}
	return out
}

// ArrowFromTo builds an Arrow pointing from start to end.
//
// The arrow's pose origin sits at start; its orientation vector
// points toward end; its length equals the distance between them.
// Useful for "draw a force vector" / "show a motion plan from A
// to B" without computing orientation yourself.
//
// Panics if start and end coincide (zero-length arrow).
func ArrowFromTo(label string, start, end Pose, radiusMM float64) *Arrow {
	dx, dy, dz := end.X-start.X, end.Y-start.Y, end.Z-start.Z
	length := math.Sqrt(dx*dx + dy*dy + dz*dz)
	must(length >= 1e-6, "ArrowFromTo needs distinct points; |end-start|=%v", length)
	return &Arrow{
		Label:    label,
		Pose:     PoseAt(start.X, start.Y, start.Z, dx/length, dy/length, dz/length, 0),
		LengthMM: length,
		RadiusMM: radiusMM,
	}
}

// ptrF is an internal helper to take a float64 by pointer.
func ptrF(v float64) *float64 { return &v }

// TrajectoryPlan — visualization for a motion plan (list of poses
// with orientation). Expands to a polyline connecting the waypoints
// plus a CoordinateFrame triad at each waypoint so the orientation
// at each step is visible.
//
// Designed to match the shape of motion-planner output (CBiRRT,
// RRT*, motion-service plans). After forward-kinematics on the
// planner's joint-position output, each step is a Cartesian pose;
// pass that list in as Waypoints and the composite renders the plan.
//
// Pair with LerpPose to animate a "runner" that walks the plan
// between adjacent waypoints with smoothly interpolating orientation.
//
// Internal labels: line segments use "<LabelPrefix>_path_seg_NN";
// waypoint frames use "<LabelPrefix>_wp_N" for the anchor and the
// usual _axis_x/y/z suffixes.
type TrajectoryPlan struct {
	LabelPrefix string
	Waypoints   []Pose
	ParentFrame string

	// Path line styling.
	LineColor   *Color
	LineWidthMM float64
	LineOpacity *float64

	// Per-waypoint CoordinateFrame styling.
	ShowFrames          bool // default false → true; set ShowFramesOverride
	ShowFramesOverride  *bool
	FrameSizeMM         float64
	FrameAnchorRadiusMM float64
	FrameAxisRadiusMM   float64
	FrameAnchorColor    *Color
	FrameAnchorOpacity  *float64
	FrameAxisOpacity    *float64
	FrameShowAxesHelper *bool // nil → false (the explicit arrows are the show)
}

// ToVisuals expands the plan into a Line composite plus N
// CoordinateFrames.
func (tp TrajectoryPlan) ToVisuals() []Visual {
	must(len(tp.Waypoints) >= 2,
		"TrajectoryPlan needs at least 2 waypoints; got %d", len(tp.Waypoints))

	lineColor := tp.LineColor
	if lineColor == nil {
		lineColor = &Color{R: 100, G: 180, B: 220}
	}
	lineWidth := tp.LineWidthMM
	if lineWidth <= 0 {
		lineWidth = 6.0
	}
	lineOpacity := tp.LineOpacity
	if lineOpacity == nil {
		lineOpacity = ptrF(0.6)
	}
	showFrames := true
	if tp.ShowFramesOverride != nil {
		showFrames = *tp.ShowFramesOverride
	}
	frameSize := tp.FrameSizeMM
	if frameSize <= 0 {
		frameSize = 80.0
	}
	frameAnchorR := tp.FrameAnchorRadiusMM
	if frameAnchorR <= 0 {
		frameAnchorR = 6.0
	}
	frameAxisR := tp.FrameAxisRadiusMM
	if frameAxisR <= 0 {
		frameAxisR = 4.0
	}
	frameAnchorColor := tp.FrameAnchorColor
	if frameAnchorColor == nil {
		frameAnchorColor = &Color{R: 120, G: 120, B: 120}
	}
	frameAnchorOp := tp.FrameAnchorOpacity
	if frameAnchorOp == nil {
		frameAnchorOp = ptrF(0.5)
	}
	frameAxisOp := tp.FrameAxisOpacity
	if frameAxisOp == nil {
		frameAxisOp = ptrF(1.0)
	}
	frameShowHelper := tp.FrameShowAxesHelper
	if frameShowHelper == nil {
		frameShowHelper = ptrB(false)
	}

	out := []Visual{}

	// Path line.
	out = append(out, Line{
		LabelPrefix: tp.LabelPrefix + "_path",
		Points:      append([]Pose(nil), tp.Waypoints...),
		WidthMM:     lineWidth,
		ParentFrame: tp.ParentFrame,
		Color:       lineColor,
		Opacity:     lineOpacity,
	}.ToVisuals()...)

	// Per-waypoint CoordinateFrames.
	if showFrames {
		for i, wp := range tp.Waypoints {
			out = append(out, CoordinateFrame{
				Label:          fmt.Sprintf("%s_wp_%d", tp.LabelPrefix, i),
				Pose:           wp,
				ParentFrame:    tp.ParentFrame,
				SizeMM:         frameSize,
				AxisLengthMM:   frameSize,
				AxisRadiusMM:   frameAxisR,
				AnchorRadiusMM: frameAnchorR,
				AnchorColor:    frameAnchorColor,
				AnchorOpacity:  frameAnchorOp,
				AxisOpacity:    frameAxisOp,
				ShowAxesHelper: frameShowHelper,
			}.ToVisuals()...)
		}
	}
	return out
}
