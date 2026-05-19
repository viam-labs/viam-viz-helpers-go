package visuals

import (
	"math"
	"testing"
)

// ---- SpinPose --------------------------------------------------------

func TestSpinPose_AtTZero_IsZeroTheta(t *testing.T) {
	p := SpinPose(PoseAt(10, 0, 0, 0, 0, 1, 0), 3.0, 0)
	if p.Theta != 0 {
		t.Errorf("expected theta=0 at t=0, got %v", p.Theta)
	}
}

func TestSpinPose_AtOnePeriod_WrapsToZero(t *testing.T) {
	p := SpinPose(IdentityPose(), 3.0, 3.0)
	if math.Abs(p.Theta) > 1e-9 {
		t.Errorf("expected theta=0 at one period, got %v", p.Theta)
	}
}

func TestSpinPose_AtQuarterPeriod_Is90(t *testing.T) {
	p := SpinPose(IdentityPose(), 4.0, 1.0)
	if math.Abs(p.Theta-90.0) > 1e-9 {
		t.Errorf("expected theta=90 at quarter period, got %v", p.Theta)
	}
}

func TestSpinPose_IgnoresBaseTheta(t *testing.T) {
	p := SpinPose(PoseAt(0, 0, 0, 0, 0, 1, 180), 4.0, 0)
	if p.Theta != 0 {
		t.Errorf("spin should ignore base.Theta; got %v", p.Theta)
	}
}

func TestSpinPose_PreservesPosition(t *testing.T) {
	base := PoseAt(100, 200, 300, 0, 0, 1, 0)
	p := SpinPose(base, 4.0, 1.0)
	if p.X != 100 || p.Y != 200 || p.Z != 300 {
		t.Errorf("position changed: got (%v,%v,%v)", p.X, p.Y, p.Z)
	}
}

// ---- OrbitPose -------------------------------------------------------

func TestOrbitPose_ZAxis_AtTZero_LandsOnPositiveX(t *testing.T) {
	base := PoseAt(50, 100, 200, 0, 0, 1, 0)
	p := OrbitPose(base, 4.0, 10.0, 0.0, "z")
	if math.Abs(p.X-60) > 1e-9 || math.Abs(p.Y-100) > 1e-9 || p.Z != 200 {
		t.Errorf("expected (60,100,200), got (%v,%v,%v)", p.X, p.Y, p.Z)
	}
}

func TestOrbitPose_ZAxis_AtQuarterPeriod_LandsOnPositiveY(t *testing.T) {
	base := PoseAt(50, 100, 0, 0, 0, 1, 0)
	p := OrbitPose(base, 4.0, 10.0, 1.0, "z")
	if math.Abs(p.X-50) > 1e-9 || math.Abs(p.Y-110) > 1e-9 {
		t.Errorf("expected (50,110,...), got (%v,%v)", p.X, p.Y)
	}
}

func TestOrbitPose_YAxis_OrbitsXZ(t *testing.T) {
	p := OrbitPose(IdentityPose(), 4.0, 10.0, 0.0, "y")
	if math.Abs(p.X-10) > 1e-9 || p.Y != 0 || math.Abs(p.Z) > 1e-9 {
		t.Errorf("expected (10,0,~0), got (%v,%v,%v)", p.X, p.Y, p.Z)
	}
}

func TestOrbitPose_EmptyAxis_DefaultsToZ(t *testing.T) {
	p := OrbitPose(IdentityPose(), 4.0, 10.0, 0.0, "")
	if math.Abs(p.X-10) > 1e-9 {
		t.Errorf("empty axis should default to z; got x=%v", p.X)
	}
}

// ---- OscillatePose ---------------------------------------------------

func TestOscillatePose_AtTZero_IsBase(t *testing.T) {
	base := PoseAt(10, 20, 30, 0, 0, 1, 0)
	p := OscillatePose(base, 4.0, 50.0, 0.0, "y")
	if p.X != 10 || p.Y != 20 || p.Z != 30 {
		t.Errorf("at t=0 should be base; got (%v,%v,%v)", p.X, p.Y, p.Z)
	}
}

func TestOscillatePose_DefaultAxisY(t *testing.T) {
	p := OscillatePose(IdentityPose(), 4.0, 50.0, 1.0, "")
	if math.Abs(p.Y-50.0) > 1e-9 {
		t.Errorf("expected y=50 at quarter period, got %v", p.Y)
	}
}

// ---- SwingPose -------------------------------------------------------

func TestSwingPose_AtTZero_IsBaseTheta(t *testing.T) {
	p := SwingPose(PoseAt(0, 0, 0, 0, 0, 1, 30), 4.0, 45.0, 0.0)
	if p.Theta != 30 {
		t.Errorf("expected theta=30 at t=0, got %v", p.Theta)
	}
}

func TestSwingPose_AtQuarterPeriod_IsBasePlusAmplitude(t *testing.T) {
	p := SwingPose(PoseAt(0, 0, 0, 0, 0, 1, 30), 4.0, 45.0, 1.0)
	if math.Abs(p.Theta-75.0) > 1e-9 {
		t.Errorf("expected theta=75 (30 + 45) at quarter period, got %v", p.Theta)
	}
}

// ---- PulseRange ------------------------------------------------------

func TestPulseRange_AtTZero_IsMidpoint(t *testing.T) {
	if got := PulseRange(80, 160, 2.0, 0); got != 120 {
		t.Errorf("expected 120 at t=0, got %v", got)
	}
}

func TestPulseRange_AtQuarterPeriod_IsHigh(t *testing.T) {
	if got := PulseRange(80, 160, 4.0, 1.0); math.Abs(got-160) > 1e-9 {
		t.Errorf("expected 160 at quarter period, got %v", got)
	}
}

func TestPulseRange_AtThreeQuarterPeriod_IsLow(t *testing.T) {
	if got := PulseRange(80, 160, 4.0, 3.0); math.Abs(got-80) > 1e-9 {
		t.Errorf("expected 80 at three-quarter period, got %v", got)
	}
}

// ---- TrajectoryPose --------------------------------------------------

func TestTrajectoryPose_AtTZero_IsFirstWaypoint(t *testing.T) {
	wps := []Pose{
		PoseAt(0, 0, 0, 0, 0, 1, 0),
		PoseAt(100, 0, 0, 0, 0, 1, 0),
		PoseAt(200, 0, 0, 0, 0, 1, 0),
	}
	p := TrajectoryPose(wps, 10.0, 0, true)
	if math.Abs(p.X) > 1e-9 {
		t.Errorf("expected x=0, got %v", p.X)
	}
}

func TestTrajectoryPose_AtSegmentMidpoint(t *testing.T) {
	// 2 segments, 10 s total. At t = 2.5 (half of first 5-s segment).
	wps := []Pose{
		PoseAt(0, 0, 0, 0, 0, 1, 0),
		PoseAt(100, 0, 0, 0, 0, 1, 0),
		PoseAt(200, 0, 0, 0, 0, 1, 0),
	}
	p := TrajectoryPose(wps, 10.0, 2.5, true)
	if math.Abs(p.X-50) > 1e-6 {
		t.Errorf("expected x=50, got %v", p.X)
	}
}

func TestTrajectoryPose_LoopSnapsBack(t *testing.T) {
	wps := []Pose{
		PoseAt(0, 0, 0, 0, 0, 1, 0),
		PoseAt(100, 0, 0, 0, 0, 1, 0),
	}
	p := TrajectoryPose(wps, 10.0, 10.0, true)
	if math.Abs(p.X) > 1e-6 {
		t.Errorf("expected snap to wp0 (x=0), got %v", p.X)
	}
}

func TestTrajectoryPose_NoLoopClamps(t *testing.T) {
	wps := []Pose{
		PoseAt(0, 0, 0, 0, 0, 1, 0),
		PoseAt(100, 0, 0, 0, 0, 1, 0),
	}
	p := TrajectoryPose(wps, 10.0, 20.0, false)
	if math.Abs(p.X-100) > 1e-6 {
		t.Errorf("expected clamp to final wp (x=100), got %v", p.X)
	}
}
