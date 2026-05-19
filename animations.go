package visuals

// Animation modes for the worldstatestore wire format.
//
// An Item's Animation block selects a mode and per-mode params. At
// each tick the service-side ComputeTick (in the consuming module)
// returns the per-item pose + geometry overrides for time t plus the
// field-mask paths the viewer needs in the UPDATED event.
//
// IMPORTANT: Paths are camelCase, not snake_case. The official
// worldstatestore guide says snake_case, but the renderer
// empirically only honors the camelCase form the RDK fake emits.

// Field-mask path constants — single source of truth for the
// camelCase paths the viewer honors today.
const (
	PathTheta         = "poseInObserverFrame.pose.theta"
	PathX             = "poseInObserverFrame.pose.x"
	PathY             = "poseInObserverFrame.pose.y"
	PathZ             = "poseInObserverFrame.pose.z"
	PathOX            = "poseInObserverFrame.pose.oX"
	PathOY            = "poseInObserverFrame.pose.oY"
	PathOZ            = "poseInObserverFrame.pose.oZ"
	PathSphereRadius  = "physicalObject.geometryType.value.radiusMm"
	PathCapsuleRadius = "physicalObject.geometryType.value.radiusMm"
	PathCapsuleLength = "physicalObject.geometryType.value.lengthMm"
	PathBoxDimsX      = "physicalObject.geometryType.value.dimsMm.x"
	PathBoxDimsY      = "physicalObject.geometryType.value.dimsMm.y"
	PathBoxDimsZ      = "physicalObject.geometryType.value.dimsMm.z"
	PathMetadataColor = "metadata.color"
	PathMetadataOpac  = "metadata.opacity"
)

// SupportedModes is the closed set of valid Animation.Mode values
// the library and the worldstatestore wire format recognize. Items
// with a Mode outside this set are rejected at validation time.
var SupportedModes = []string{
	"none", "orbit", "oscillate", "spin", "swing", "pulse", "trajectory",
	"force_vector", "breathe", "flicker", "lifecycle",
}

// SupportedAxes is the closed set of valid Axis values for the
// animation modes that take one (Oscillate, Pulse with a specific
// axis, Orbit). Modes that ignore Axis treat it as the empty string.
var SupportedAxes = []string{"x", "y", "z"}

// Lifecycle-convention colors and opacities, used by the Lifecycle
// animation mode's Apply method. Matches the official
// worldstatestore guide: blue@50% (appearing), orange@100% (alive),
// red@50% (disappearing), then REMOVED (gone). Exported so module
// authors implementing custom Lifecycle behavior can match the
// convention without re-defining the palette.
var (
	LifecycleColorAppearing      = Color{R: 66, G: 165, B: 245}
	LifecycleColorAlive          = Color{R: 255, G: 152, B: 0}
	LifecycleColorDisappearing   = Color{R: 244, G: 67, B: 54}
	LifecycleOpacityAppearing    = 0.5
	LifecycleOpacityAlive        = 1.0
	LifecycleOpacityDisappearing = 0.5
)

// LifecycleOpacityDispearing is a typo-preserving alias for
// LifecycleOpacityDisappearing. Deprecated: use the correctly-spelled
// constant.
var LifecycleOpacityDispearing = LifecycleOpacityDisappearing

// Animation is the per-item animation config (Item.Animation). It's
// the **union** of every per-mode parameter: only the fields
// relevant to the selected Mode are read by the tick loop, the rest
// are ignored. For example, when Mode is "spin", only PeriodS is
// read; the trajectory Waypoints field is meaningless and untouched.
//
// Construct typically through one of the typed AnimationSpec
// implementations (Spin, Pulse, …) and their ToAnimation method,
// which validate the mode-specific subset of fields. Constructing
// an Animation literal by hand is supported but error-prone — easy
// to forget which fields the chosen Mode actually consumes.
type Animation struct {
	Mode string
	// Pose-based modes.
	RadiusMM     float64
	AmplitudeMM  float64
	PeriodS      float64
	Axis         string
	AmplitudeDeg float64
	// trajectory
	Waypoints []Pose
	DurationS float64
	Loop      bool // default true
	// force_vector
	LengthAmplitudeMM float64
	RadiusAmplitudeMM float64
	TiltDeg           float64
	PrecessionSpeed   float64
	ColorSpeed        float64
	// breathe
	Amplitude float64
	// flicker
	DutyCycle         float64
	PhaseOffsetS      float64
	RotateUUIDOnReadd *bool // pointer so "unset" (default true) is distinguishable from "false"
	// lifecycle
	AppearS    float64
	AliveS     float64
	DisappearS float64
	GoneS      float64
	// Internal: explicit-value tracking.
	HasLoop bool
}

// IsAnimated returns true iff the animation should tick.
func IsAnimated(a Animation) bool {
	return a.Mode != "" && a.Mode != "none"
}

// Overrides bundles per-tick metadata overrides emitted by certain
// animation modes — these mutate fields the renderer drops on
// UPDATED, so the library translates them into a REMOVE+ADD respawn
// at the wire. ForceVector emits Color; Breathe emits Opacity;
// Flicker and Lifecycle emit InScene; Lifecycle emits all three.
//
// InScene semantics: nil means "no scene-graph mutation this tick"
// (the entity stays in whichever state it's in). A non-nil pointer
// is an explicit declaration: *true = "make sure this entity is in
// the scene" (rising edge triggers ADD); *false = "remove it"
// (falling edge triggers REMOVE).
type Overrides struct {
	Color   *Color
	Opacity *float64
	InScene *bool
}

// BaseGeom holds the shape-specific base dim/radius/length fields.
//
// Only one set of fields is meaningful per shape type:
//   - box: Dims + HasDims
//   - sphere: RadiusMM
//   - capsule, arrow: RadiusMM + LengthMM
//   - point, mesh, pointcloud: none of the above are read
//
// The PCDBytesOverride field is a service-layer escape hatch for
// chunked-delivery pointclouds: when non-nil, the geometry builder
// for pointcloud items emits these bytes instead of reading the
// file fresh. Used internally; callers typically leave it nil.
type BaseGeom struct {
	RadiusMM         float64
	LengthMM         float64
	Dims             BoxDims
	HasDims          bool
	PCDBytesOverride []byte
}

// TickResult is the return value of LegacyAnimator.ComputeTick.
// Pose and Geom are the new state for the entity this tick. Paths
// is the camelCase field-mask path list naming what changed —
// emitted in the UPDATED event's UpdatedFields. Overrides, if
// non-nil, carries metadata / scene-graph-membership changes that
// the library translates into respawn events (REMOVE+ADD with a
// fresh UUID) since the renderer drops metadata.* on UPDATED.
//
// Deprecated: prefer the SceneTicker + Animation.Apply path, which
// returns []SceneEvent directly and lets the library handle the
// respawn dispatch via Scene.Update.
type TickResult struct {
	Pose      Pose
	Geom      BaseGeom
	Paths     []string
	Overrides *Overrides
}

// ---- AnimationSpec interface + concrete types --------------------------
//
// The Animation struct above is the union of all per-mode params —
// what gets stored in Item.Animation and read by the tick loop.
// AnimationSpec is the typed surface authors write: each concrete
// spec (Spin, Pulse, …) builds the right Animation struct via
// ToAnimation().

// AnimationSpec is the contract for typed animation parameter sets.
// Each concrete spec validates its inputs at construction (via
// ToAnimation) and produces an Animation suitable for Item.Animation.
type AnimationSpec interface {
	ToAnimation() Animation
}

// Static is the explicit "no animation" — equivalent to leaving
// Animation set to its zero value or passing nil to animOf.
type Static struct{}

// ToAnimation implements AnimationSpec.
func (Static) ToAnimation() Animation { return Animation{Mode: "none"} }

// Spin is continuous rotation around the entity's local Z axis.
// PeriodS is the time in seconds for one full revolution.
type Spin struct{ PeriodS float64 }

// ToAnimation implements AnimationSpec.
func (s Spin) ToAnimation() Animation {
	return Animation{Mode: "spin", PeriodS: s.PeriodS}
}

// Swing is bounded pendulum motion around a fixed axis.
// AmplitudeDeg is the half-amplitude in degrees.
type Swing struct {
	AmplitudeDeg float64
	PeriodS      float64
	PhaseOffsetS float64
}

// ToAnimation implements AnimationSpec.
func (s Swing) ToAnimation() Animation {
	return Animation{
		Mode: "swing", AmplitudeDeg: s.AmplitudeDeg,
		PeriodS: s.PeriodS, PhaseOffsetS: s.PhaseOffsetS,
	}
}

// Oscillate translates back and forth along a world axis.
// Axis must be "x", "y", or "z". AmplitudeMM is signed — negative
// inverts the cycle phase.
type Oscillate struct {
	Axis         string
	AmplitudeMM  float64
	PeriodS      float64
	PhaseOffsetS float64
}

// ToAnimation implements AnimationSpec.
func (o Oscillate) ToAnimation() Animation {
	must(o.Axis == "x" || o.Axis == "y" || o.Axis == "z",
		"Oscillate.Axis must be x|y|z; got %q", o.Axis)
	return Animation{
		Mode: "oscillate", Axis: o.Axis,
		AmplitudeMM: o.AmplitudeMM, PeriodS: o.PeriodS,
		PhaseOffsetS: o.PhaseOffsetS,
	}
}

// Orbit is circular translation in the XY plane around the entity's
// base pose.
type Orbit struct {
	RadiusMM float64
	PeriodS  float64
}

// ToAnimation implements AnimationSpec.
func (o Orbit) ToAnimation() Animation {
	return Animation{Mode: "orbit", RadiusMM: o.RadiusMM, PeriodS: o.PeriodS}
}

// Pulse scales a primitive's size over each period. For Sphere /
// Capsule, modulates the radius; for Box, set Axis to "x"/"y"/"z"
// to modulate that dimension.
type Pulse struct {
	AmplitudeMM float64
	PeriodS     float64
	Axis        string
}

// ToAnimation implements AnimationSpec.
func (p Pulse) ToAnimation() Animation {
	return Animation{
		Mode: "pulse", AmplitudeMM: p.AmplitudeMM,
		PeriodS: p.PeriodS, Axis: p.Axis,
	}
}

// Breathe is a smooth opacity oscillation around the entity's base
// opacity. Amplitude is the swing in [0, 1] space.
type Breathe struct {
	Amplitude float64
	PeriodS   float64
}

// ToAnimation implements AnimationSpec.
func (b Breathe) ToAnimation() Animation {
	return Animation{Mode: "breathe", Amplitude: b.Amplitude, PeriodS: b.PeriodS}
}

// Flicker makes the entity blink in and out of the scene.
// DutyCycle in [0, 1] is the fraction of each period the entity is
// visible. RotateUUIDOnReadd defaults to a true value when nil;
// set explicitly to a *bool(false) to demonstrate the renderer's
// REMOVED-UUID cache bug.
type Flicker struct {
	PeriodS           float64
	DutyCycle         float64
	PhaseOffsetS      float64
	RotateUUIDOnReadd *bool
}

// ToAnimation implements AnimationSpec.
func (f Flicker) ToAnimation() Animation {
	return Animation{
		Mode: "flicker", PeriodS: f.PeriodS, DutyCycle: f.DutyCycle,
		PhaseOffsetS: f.PhaseOffsetS, RotateUUIDOnReadd: f.RotateUUIDOnReadd,
	}
}

// Lifecycle cycles through the worldstatestore lifecycle color
// convention: appearing (blue, 50% opacity) → alive (orange, 100%)
// → disappearing (red, 50%) → gone (REMOVED).
type Lifecycle struct {
	AppearS      float64
	AliveS       float64
	DisappearS   float64
	GoneS        float64
	PhaseOffsetS float64
}

// ToAnimation implements AnimationSpec.
func (l Lifecycle) ToAnimation() Animation {
	return Animation{
		Mode:    "lifecycle",
		AppearS: l.AppearS, AliveS: l.AliveS,
		DisappearS: l.DisappearS, GoneS: l.GoneS,
		PhaseOffsetS: l.PhaseOffsetS,
	}
}

// ForceVector drives an Arrow's length, radius, orientation
// (precessing around world Z at a fixed tilt), and color
// simultaneously. Useful for previewing force / wrench
// visualizations.
type ForceVector struct {
	PeriodS           float64
	LengthAmplitudeMM float64
	RadiusAmplitudeMM float64
	TiltDeg           float64
	PrecessionSpeed   float64
	ColorSpeed        float64
}

// ToAnimation implements AnimationSpec.
func (fv ForceVector) ToAnimation() Animation {
	return Animation{
		Mode: "force_vector", PeriodS: fv.PeriodS,
		LengthAmplitudeMM: fv.LengthAmplitudeMM,
		RadiusAmplitudeMM: fv.RadiusAmplitudeMM,
		TiltDeg:           fv.TiltDeg,
		PrecessionSpeed:   fv.PrecessionSpeed,
		ColorSpeed:        fv.ColorSpeed,
	}
}

// Trajectory walks through a sequence of pose waypoints over
// DurationS, optionally looping. Position and orientation are
// linearly interpolated between adjacent waypoints.
type Trajectory struct {
	Waypoints []Pose
	DurationS float64
	Loop      bool
}

// ToAnimation implements AnimationSpec.
func (t Trajectory) ToAnimation() Animation {
	must(len(t.Waypoints) >= 2,
		"Trajectory needs at least 2 waypoints; got %d", len(t.Waypoints))
	return Animation{
		Mode: "trajectory", Waypoints: t.Waypoints,
		DurationS: t.DurationS, Loop: t.Loop, HasLoop: true,
	}
}

// animOf returns the Animation produced by a spec, or a "none"
// Animation when the spec is nil. Used by Visual.ToItem to resolve
// the animation field.
func animOf(a AnimationSpec) Animation {
	if a == nil {
		return Animation{Mode: "none"}
	}
	return a.ToAnimation()
}
