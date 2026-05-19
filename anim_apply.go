// Apply methods on the AnimationSpec types — the imperative side
// of the new SceneTick-based animation path.
//
// Each spec implements Apply(visual, base Visual, t float64). The
// library's default SceneServiceBase.SceneTick iterates the Scene,
// looks up each Visual's animation spec, calls Apply, and emits the
// resulting scene.Update events.
//
// Apply takes pointers (the concrete shape types: *Box, *Sphere,
// etc., which Scene stores via the Visual interface) and mutates
// them in place. The base argument is a snapshot of the Visual at
// SetScene time — it carries the "rest state" so apply math is
// stable across ticks.
//
// All Apply methods are no-ops when the visual / base aren't a
// pointer to a known concrete shape type. This means future shape
// types fall back silently rather than panicking.
package visuals

import (
	"math"
)

// ---- pose helpers ----------------------------------------------------

// visualPose returns the Pose of a concrete shape pointer stored in
// the scene. Returns zero Pose for unknown types.
func visualPose(v Visual) Pose {
	switch x := v.(type) {
	case *Box:
		return x.Pose
	case *Sphere:
		return x.Pose
	case *Capsule:
		return x.Pose
	case *Point:
		return x.Pose
	case *Frame:
		return x.Pose
	case *Arrow:
		return x.Pose
	case *Mesh:
		return x.Pose
	case *PointCloud:
		return x.Pose
	}
	return Pose{}
}

// setVisualPose mutates the Pose field on a concrete shape pointer.
// No-op for unknown types.
func setVisualPose(v Visual, p Pose) {
	switch x := v.(type) {
	case *Box:
		x.Pose = p
	case *Sphere:
		x.Pose = p
	case *Capsule:
		x.Pose = p
	case *Point:
		x.Pose = p
	case *Frame:
		x.Pose = p
	case *Arrow:
		x.Pose = p
	case *Mesh:
		x.Pose = p
	case *PointCloud:
		x.Pose = p
	}
}

// setVisualColor mutates the Color field on a concrete shape pointer.
func setVisualColor(v Visual, c *Color) {
	switch x := v.(type) {
	case *Box:
		x.Color = c
	case *Sphere:
		x.Color = c
	case *Capsule:
		x.Color = c
	case *Point:
		x.Color = c
	case *Arrow:
		x.Color = c
	case *Mesh:
		x.Color = c
	case *PointCloud:
		x.Color = c
	}
}

// setVisualOpacity mutates the Opacity field on a concrete shape pointer.
func setVisualOpacity(v Visual, op *float64) {
	switch x := v.(type) {
	case *Box:
		x.Opacity = op
	case *Sphere:
		x.Opacity = op
	case *Capsule:
		x.Opacity = op
	case *Point:
		x.Opacity = op
	case *Arrow:
		x.Opacity = op
	case *Mesh:
		x.Opacity = op
	case *PointCloud:
		x.Opacity = op
	}
}

// setVisualInvisible mutates the Invisible field on a concrete
// shape pointer. Frame doesn't have an Invisible field (it has its
// own Visible flag); flicker on a Frame is a no-op.
func setVisualInvisible(v Visual, inv bool) {
	switch x := v.(type) {
	case *Box:
		x.Invisible = inv
	case *Sphere:
		x.Invisible = inv
	case *Capsule:
		x.Invisible = inv
	case *Point:
		x.Invisible = inv
	case *Arrow:
		x.Invisible = inv
	case *Mesh:
		x.Invisible = inv
	case *PointCloud:
		x.Invisible = inv
	}
}

// ---- Apply on each AnimationSpec -------------------------------------

// Apply on Static is a no-op (no animation).
func (Static) Apply(_ Visual, _ Visual, _ float64) {}

// Apply on Spin: visual.Pose = SpinPose(base.Pose, period, t).
func (s Spin) Apply(visual, base Visual, t float64) {
	setVisualPose(visual, SpinPose(visualPose(base), s.PeriodS, t))
}

// Apply on Swing: theta swings sinusoidally around base.Theta.
func (s Swing) Apply(visual, base Visual, t float64) {
	setVisualPose(
		visual,
		SwingPose(visualPose(base), s.PeriodS, s.AmplitudeDeg, t+s.PhaseOffsetS),
	)
}

// Apply on Oscillate: position offset along axis sinusoidally.
func (o Oscillate) Apply(visual, base Visual, t float64) {
	setVisualPose(
		visual,
		OscillatePose(visualPose(base), o.PeriodS, o.AmplitudeMM, t+o.PhaseOffsetS, o.Axis),
	)
}

// Apply on Orbit: circular position translation in XY around base.
func (o Orbit) Apply(visual, base Visual, t float64) {
	setVisualPose(
		visual,
		OrbitPose(visualPose(base), o.PeriodS, o.RadiusMM, t, "z"),
	)
}

// Apply on Pulse: dispatches on shape type. Box: dims_mm (optional
// axis); Sphere/Capsule: radius (Capsule also bumps length).
func (p Pulse) Apply(visual, base Visual, t float64) {
	delta := p.AmplitudeMM * math.Sin(2*math.Pi*t/p.PeriodS)
	switch vis := visual.(type) {
	case *Box:
		b, ok := base.(*Box)
		if !ok {
			return
		}
		bd := b.DimsMM
		switch p.Axis {
		case "x":
			vis.DimsMM = BoxDims{X: math.Max(0.1, bd.X+delta), Y: bd.Y, Z: bd.Z}
		case "y":
			vis.DimsMM = BoxDims{X: bd.X, Y: math.Max(0.1, bd.Y+delta), Z: bd.Z}
		case "z":
			vis.DimsMM = BoxDims{X: bd.X, Y: bd.Y, Z: math.Max(0.1, bd.Z+delta)}
		default:
			vis.DimsMM = BoxDims{
				X: math.Max(0.1, bd.X+delta),
				Y: math.Max(0.1, bd.Y+delta),
				Z: math.Max(0.1, bd.Z+delta),
			}
		}
	case *Sphere:
		b, ok := base.(*Sphere)
		if !ok {
			return
		}
		vis.RadiusMM = math.Max(0.1, b.RadiusMM+delta)
	case *Capsule:
		b, ok := base.(*Capsule)
		if !ok {
			return
		}
		vis.RadiusMM = math.Max(0.1, b.RadiusMM+delta)
		vis.LengthMM = math.Max(0.1, b.LengthMM+delta)
	}
}

// Apply on Breathe: opacity oscillates around base.Opacity.
func (b Breathe) Apply(visual, base Visual, t float64) {
	var baseOpacity float64 = 1.0
	switch x := base.(type) {
	case *Box:
		if x.Opacity != nil {
			baseOpacity = *x.Opacity
		}
	case *Sphere:
		if x.Opacity != nil {
			baseOpacity = *x.Opacity
		}
	case *Capsule:
		if x.Opacity != nil {
			baseOpacity = *x.Opacity
		}
	case *Mesh:
		if x.Opacity != nil {
			baseOpacity = *x.Opacity
		}
	}
	op := baseOpacity + b.Amplitude*math.Sin(2*math.Pi*t/b.PeriodS)
	if op < 0 {
		op = 0
	}
	if op > 1 {
		op = 1
	}
	setVisualOpacity(visual, &op)
}

// Apply on Flicker: visible vs invisible based on duty cycle.
func (f Flicker) Apply(visual, _ Visual, t float64) {
	cycle := math.Mod(t+f.PhaseOffsetS, f.PeriodS)
	invisible := cycle/f.PeriodS >= f.DutyCycle
	setVisualInvisible(visual, invisible)
}

// Apply on Lifecycle: phase machine cycling through color/opacity
// states. Matches the official viam-visualization lifecycle convention.
func (l Lifecycle) Apply(visual, _ Visual, t float64) {
	cycle := l.AppearS + l.AliveS + l.DisappearS + l.GoneS
	local := math.Mod(t+l.PhaseOffsetS, cycle)
	var (
		colorAppear     = Color{R: 95, G: 150, B: 255}
		colorAlive      = Color{R: 255, G: 150, B: 50}
		colorDisappear  = Color{R: 255, G: 90, B: 70}
		opAppear        = 0.5
		opAlive         = 1.0
		opDisappear     = 0.5
	)
	if local < l.AppearS {
		c := colorAppear
		op := opAppear
		setVisualColor(visual, &c)
		setVisualOpacity(visual, &op)
		setVisualInvisible(visual, false)
	} else if local < l.AppearS+l.AliveS {
		c := colorAlive
		op := opAlive
		setVisualColor(visual, &c)
		setVisualOpacity(visual, &op)
		setVisualInvisible(visual, false)
	} else if local < l.AppearS+l.AliveS+l.DisappearS {
		c := colorDisappear
		op := opDisappear
		setVisualColor(visual, &c)
		setVisualOpacity(visual, &op)
		setVisualInvisible(visual, false)
	} else {
		setVisualInvisible(visual, true)
	}
}

// Apply on ForceVector: Arrow-only. Mutates length, radius,
// orientation (precession around world Z at fixed tilt), and color.
//
// Known artifact: cycling color dirties metadata every tick, which
// Scene.Update escalates to REMOVE+ADD respawn (renderer ignores
// metadata.* on UPDATED). On initial page-load the renderer can race
// the first respawn and leave a frozen ghost arrow next to the live
// one. Filed against the renderer; left in place here so the demo
// keeps showing color animation. Mitigations if needed: snap_step
// the hue to a few steps per period, or use the BreathingShapes
// label-rotation pattern.
func (fv ForceVector) Apply(visual, base Visual, t float64) {
	v, ok := visual.(*Arrow)
	if !ok {
		return
	}
	b, ok := base.(*Arrow)
	if !ok {
		return
	}
	phase := 2 * math.Pi * t / fv.PeriodS
	v.LengthMM = math.Max(0.1, b.LengthMM+fv.LengthAmplitudeMM*math.Sin(phase))
	v.RadiusMM = math.Max(0.1, b.RadiusMM+fv.RadiusAmplitudeMM*math.Sin(phase+math.Pi/3))
	tiltRad := fv.TiltDeg * math.Pi / 180.0
	precession := phase * fv.PrecessionSpeed
	v.Pose = Pose{
		X: b.Pose.X, Y: b.Pose.Y, Z: b.Pose.Z,
		OX:    math.Sin(tiltRad) * math.Cos(precession),
		OY:    math.Sin(tiltRad) * math.Sin(precession),
		OZ:    math.Cos(tiltRad),
		Theta: 0,
	}
	hue := math.Mod(t*fv.ColorSpeed/fv.PeriodS, 1.0)
	c := HSVToRGB(hue, 1, 1)
	v.Color = &c
}

// Apply on Trajectory: visual.Pose = TrajectoryPose(waypoints, duration, t, loop).
func (tr Trajectory) Apply(visual, _ Visual, t float64) {
	if len(tr.Waypoints) < 2 {
		return
	}
	setVisualPose(visual, TrajectoryPose(tr.Waypoints, tr.DurationS, t, tr.Loop))
}

// snapshotVisual returns a fresh pointer to a copy of v's struct
// value, used by SceneServiceBase.SetScene to capture each Visual's
// rest state for animation Apply calls. Shallow copy: inner
// pointers (Color, Opacity) point at the original allocations, but
// Apply only reads them so that's safe.
func snapshotVisual(v Visual) Visual {
	switch x := v.(type) {
	case *Box:
		c := *x
		return &c
	case *Sphere:
		c := *x
		return &c
	case *Capsule:
		c := *x
		return &c
	case *Point:
		c := *x
		return &c
	case *Frame:
		c := *x
		return &c
	case *Arrow:
		c := *x
		return &c
	case *Mesh:
		c := *x
		return &c
	case *PointCloud:
		c := *x
		return &c
	}
	return v
}

// Applicable is the optional extension interface on AnimationSpec
// that exposes the Apply method. SceneServiceBase.SceneTick
// type-asserts: specs that satisfy Applicable get the new
// dispatch path; specs that don't are skipped (their animation
// won't tick under SceneTick).
//
// Every spec in this package implements Apply, so they all satisfy
// Applicable. The interface exists so external animation specs can
// opt in or out independently.
type Applicable interface {
	Apply(visual, base Visual, t float64)
}
