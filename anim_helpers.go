// Imperative pose-composing helpers for animation.
//
// These functions take a "base" pose (the rest state of the entity)
// plus a time t (elapsed seconds), and return a new Pose with the
// appropriate animation math applied. They're pure — the input
// base is not mutated.
//
// Pair with SceneServiceBase.SceneTick:
//
//	type myService struct {
//	    resource.Named
//	    resource.TriviallyCloseable
//	    visuals.SceneServiceBase
//	    box      *visuals.Box
//	    basePose visuals.Pose
//	}
//
//	func (s *myService) SceneTick(scene *visuals.Scene, t float64) []visuals.SceneEvent {
//	    s.box.Pose = visuals.SpinPose(s.basePose, 3.0, t)
//	    events, _ := scene.Update(s.box)
//	    return events
//	}
//
// The helpers below cover the simple "absolute t → pose" animation
// modes. For animations that mutate non-pose fields (radius pulse,
// color cycle, etc.), mutate the field directly in your SceneTick;
// no helper is needed.
package visuals

import (
	"fmt"
	"math"
)

// SpinPose returns base with Theta set to a continuous rotation of
// 360°/periodS per second.
//
// Theta = (360° × t / periodS) mod 360° — absolute (base.Theta is
// ignored), so callers don't need to track accumulated angle.
func SpinPose(base Pose, periodS, t float64) Pose {
	b := fillPose(base)
	b.Theta = math.Mod(360.0*t/periodS, 360.0)
	return b
}

// OrbitPose returns base with position orbiting in a circle of
// radiusMM around base's position, in the plane perpendicular to
// axis. Default axis is "z" (orbit in world XY). Use "y" for XZ
// (vertical loop) or "x" for YZ.
func OrbitPose(base Pose, periodS, radiusMM, t float64, axis string) Pose {
	b := fillPose(base)
	phase := 2 * math.Pi * t / periodS
	c, s := math.Cos(phase), math.Sin(phase)
	switch axis {
	case "z", "":
		b.X += radiusMM * c
		b.Y += radiusMM * s
	case "y":
		b.X += radiusMM * c
		b.Z += radiusMM * s
	case "x":
		b.Y += radiusMM * c
		b.Z += radiusMM * s
	default:
		panic(fmt.Sprintf("axis must be 'x', 'y', or 'z'; got %q", axis))
	}
	return b
}

// OscillatePose returns base with one position axis offset by
// amplitudeMM × sin(2π t / periodS). Pass axis="x"/"y"/"z" (default
// "y").
func OscillatePose(base Pose, periodS, amplitudeMM, t float64, axis string) Pose {
	b := fillPose(base)
	delta := amplitudeMM * math.Sin(2*math.Pi*t/periodS)
	switch axis {
	case "x":
		b.X += delta
	case "y", "":
		b.Y += delta
	case "z":
		b.Z += delta
	default:
		panic(fmt.Sprintf("axis must be 'x', 'y', or 'z'; got %q", axis))
	}
	return b
}

// SwingPose returns base with theta swinging sinusoidally around
// base.Theta:
//
//	theta = base.Theta + amplitudeDeg × sin(2π t / periodS)
//
// Unlike SpinPose, swing is relative to base.Theta — useful for
// pendulum-like motion where the "rest" theta matters.
func SwingPose(base Pose, periodS, amplitudeDeg, t float64) Pose {
	b := fillPose(base)
	b.Theta = b.Theta + amplitudeDeg*math.Sin(2*math.Pi*t/periodS)
	return b
}

// PulseRange returns a sinusoidal value swinging between lo and hi
// with the given period. Use for scalar fields that should breathe
// between two extremes — sphere radius, box dim, opacity, etc.
//
// Example — box that pulses between 80 mm and 160 mm at period 2 s:
//
//	scale := visuals.PulseRange(80, 160, 2.0, t)
//	box.DimsMM = visuals.BoxDims{X: scale, Y: scale, Z: scale}
//
// Equivalent to base + amplitude × sin(2π t / periodS) where
// base = (lo + hi) / 2 and amplitude = (hi - lo) / 2.
func PulseRange(lo, hi, periodS, t float64) float64 {
	base := (lo + hi) / 2.0
	amplitude := (hi - lo) / 2.0
	return base + amplitude*math.Sin(2*math.Pi*t/periodS)
}

// TrajectoryPose returns an interpolated pose along a multi-waypoint
// trajectory. Walks waypoints over durationS seconds, lerping
// between adjacent pairs with LerpPose (quaternion SLERP on
// orientation). With loop=true, the trajectory restarts (snap back)
// once t exceeds durationS; with loop=false, clamps to the final
// waypoint.
//
// The waypoint list should match the shape of a planner output
// (CBiRRT / RRT* / motion-service): each element is a Pose.
//
// Example — runner walking a 5-waypoint plan over 12 seconds:
//
//	plan := []visuals.Pose{visuals.PoseAt(...), ...}
//	// In SceneTick:
//	s.runner.Pose = visuals.TrajectoryPose(plan, 12.0, t, true)
//	events, _ := scene.Update(s.runner)
func TrajectoryPose(waypoints []Pose, durationS, t float64, loop bool) Pose {
	n := len(waypoints)
	if n < 2 {
		panic(fmt.Sprintf("TrajectoryPose needs ≥ 2 waypoints; got %d", n))
	}
	nSegs := n - 1
	var progress float64
	if loop {
		progress = math.Mod(t/durationS*float64(nSegs), float64(nSegs))
	} else {
		progress = t / durationS * float64(nSegs)
		if progress < 0 {
			progress = 0
		} else if progress > float64(nSegs) {
			progress = float64(nSegs)
		}
	}
	segIdx := int(progress)
	if segIdx >= nSegs {
		segIdx = nSegs - 1
	}
	local := progress - float64(segIdx)
	return LerpPose(waypoints[segIdx], waypoints[segIdx+1], local)
}
