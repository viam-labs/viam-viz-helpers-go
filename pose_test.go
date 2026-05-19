package visuals

import (
	"math"
	"testing"
)

// rotateVecByQuat applies rotation quaternion (w, x, y, z) to a 3-vector.
func rotateVecByQuat(q [4]float64, v [3]float64) [3]float64 {
	w, x, y, z := q[0], q[1], q[2], q[3]
	vx, vy, vz := v[0], v[1], v[2]
	rx := (1-2*(y*y+z*z))*vx + 2*(x*y-w*z)*vy + 2*(x*z+w*y)*vz
	ry := 2*(x*y+w*z)*vx + (1-2*(x*x+z*z))*vy + 2*(y*z-w*x)*vz
	rz := 2*(x*z-w*y)*vx + 2*(y*z+w*x)*vy + (1-2*(x*x+y*y))*vz
	return [3]float64{rx, ry, rz}
}

// rotationsEquivalent tests that two quaternions represent the same
// rotation by rotating three linearly-independent probe vectors.
func rotationsEquivalent(q1, q2 [4]float64, tol float64) bool {
	probes := [][3]float64{{1, 0, 0}, {0, 1, 0}, {1, 2, 3}}
	for _, v := range probes {
		v1 := rotateVecByQuat(q1, v)
		v2 := rotateVecByQuat(q2, v)
		for i := 0; i < 3; i++ {
			if math.Abs(v1[i]-v2[i]) > tol {
				return false
			}
		}
	}
	return true
}

func TestOVToQuatRoundTripPreservesRotation(t *testing.T) {
	cases := [][4]float64{
		{0, 0, 1, 0},  // identity
		{0, 0, 1, 90}, // roll around world Z
		{1, 0, 0, 0},  // local Z → world +X
		{0, 1, 0, 0},  // local Z → world +Y
		{0, 0, -1, 0}, // flipped
		{1 / math.Sqrt2, 0, 1 / math.Sqrt2, 45},
		{0.5, 0.5, 1 / math.Sqrt2, 30},
	}
	for _, c := range cases {
		ox, oy, oz, theta := c[0], c[1], c[2], c[3]
		q1 := ovToQuat(ox, oy, oz, theta)
		ox2, oy2, oz2, theta2 := quatToOV(q1[0], q1[1], q1[2], q1[3])
		q2 := ovToQuat(ox2, oy2, oz2, theta2)
		if !rotationsEquivalent(q1, q2, 1e-6) {
			t.Errorf("round-trip changed rotation: input=(%v,%v,%v,θ=%v) "+
				"q1=%v out=(%v,%v,%v,θ=%v) q2=%v",
				ox, oy, oz, theta, q1, ox2, oy2, oz2, theta2, q2)
		}
	}
}

func TestLerpPoseAtTZeroReturnsFirstPose(t *testing.T) {
	a := PoseAt(10, 20, 30, 1, 0, 0, 0)
	b := PoseAt(40, 50, 60, 0, 0, 1, 90)
	r := LerpPose(a, b, 0)
	if math.Abs(r.X-a.X) > 1e-6 || math.Abs(r.Y-a.Y) > 1e-6 || math.Abs(r.Z-a.Z) > 1e-6 {
		t.Errorf("position @ t=0 should be a: got (%v,%v,%v) want (%v,%v,%v)",
			r.X, r.Y, r.Z, a.X, a.Y, a.Z)
	}
	qa := ovToQuat(a.OX, a.OY, a.OZ, a.Theta)
	qr := ovToQuat(r.OX, r.OY, r.OZ, r.Theta)
	if !rotationsEquivalent(qa, qr, 1e-5) {
		t.Errorf("orientation @ t=0 should match a")
	}
}

func TestLerpPoseAtTOneReturnsSecondPose(t *testing.T) {
	a := PoseAt(10, 20, 30, 1, 0, 0, 0)
	b := PoseAt(40, 50, 60, 0, 0, 1, 90)
	r := LerpPose(a, b, 1)
	if math.Abs(r.X-b.X) > 1e-6 || math.Abs(r.Y-b.Y) > 1e-6 || math.Abs(r.Z-b.Z) > 1e-6 {
		t.Errorf("position @ t=1 should be b: got (%v,%v,%v) want (%v,%v,%v)",
			r.X, r.Y, r.Z, b.X, b.Y, b.Z)
	}
	qb := ovToQuat(b.OX, b.OY, b.OZ, b.Theta)
	qr := ovToQuat(r.OX, r.OY, r.OZ, r.Theta)
	if !rotationsEquivalent(qb, qr, 1e-5) {
		t.Errorf("orientation @ t=1 should match b")
	}
}

// Regression: lerping through the OV singularity at |oz|=1. Naive
// lerp produces visible flips; SLERP produces constant-speed great-
// circle interpolation.
func TestLerpPoseSmoothThroughOZSingularity(t *testing.T) {
	a := PoseAt(0, 0, 0, 1, 0, 0, 0)
	b := PoseAt(0, 0, 0, 0, 0, 1, 90)
	n := 100
	quats := make([][4]float64, n+1)
	for i := 0; i <= n; i++ {
		tt := float64(i) / float64(n)
		r := LerpPose(a, b, tt)
		quats[i] = ovToQuat(r.OX, r.OY, r.OZ, r.Theta)
	}
	angles := make([]float64, n)
	for i := 0; i < n; i++ {
		q1, q2 := quats[i], quats[i+1]
		dot := math.Abs(q1[0]*q2[0] + q1[1]*q2[1] + q1[2]*q2[2] + q1[3]*q2[3])
		if dot > 1 {
			dot = 1
		}
		angles[i] = 2 * math.Acos(dot)
	}
	sum := 0.0
	maxA := 0.0
	for _, a := range angles {
		sum += a
		if a > maxA {
			maxA = a
		}
	}
	avg := sum / float64(len(angles))
	if maxA > avg*1.05 {
		t.Errorf("non-constant-speed SLERP: max step %.4f° vs avg %.4f° "+
			"(interpolation flipping through singularity)",
			maxA*180/math.Pi, avg*180/math.Pi)
	}
}

func TestLerpPoseMidpointIsHalfwayInSO3(t *testing.T) {
	a := PoseAt(0, 0, 0, 1, 0, 0, 0)
	b := PoseAt(0, 0, 0, 0, 0, 1, 90)
	r := LerpPose(a, b, 0.5)
	qa := ovToQuat(a.OX, a.OY, a.OZ, a.Theta)
	qb := ovToQuat(b.OX, b.OY, b.OZ, b.Theta)
	qr := ovToQuat(r.OX, r.OY, r.OZ, r.Theta)
	ang := func(p, q [4]float64) float64 {
		dot := math.Abs(p[0]*q[0] + p[1]*q[1] + p[2]*q[2] + p[3]*q[3])
		if dot > 1 {
			dot = 1
		}
		return 2 * math.Acos(dot)
	}
	aToMid := ang(qa, qr)
	midToB := ang(qr, qb)
	if math.Abs(aToMid-midToB) > 1e-4 {
		t.Errorf("midpoint not halfway in SO(3): a→mid=%.3f° mid→b=%.3f°",
			aToMid*180/math.Pi, midToB*180/math.Pi)
	}
}

func TestLerpPoseTakesShorterArc(t *testing.T) {
	a := PoseAt(0, 0, 0, 0, 0, 1, 0)
	b := PoseAt(0, 0, 0, 0, 0, 1, 270) // equivalent to -90 via short arc
	r := LerpPose(a, b, 0.5)
	qa := ovToQuat(a.OX, a.OY, a.OZ, a.Theta)
	qr := ovToQuat(r.OX, r.OY, r.OZ, r.Theta)
	dot := math.Abs(qa[0]*qr[0] + qa[1]*qr[1] + qa[2]*qr[2] + qa[3]*qr[3])
	if dot > 1 {
		dot = 1
	}
	angle := 2 * math.Acos(dot) * 180 / math.Pi
	if math.Abs(angle-45) > 1.0 {
		t.Errorf("expected ~45° (short arc), got %.3f°", angle)
	}
}
