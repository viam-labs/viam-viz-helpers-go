package visuals

// Color is an RGB color with channel values in [0, 255]. The wire
// format encodes each channel as a uint8; the integer type here
// keeps construction syntax light (no casts at call sites).
type Color struct {
	R, G, B int
}

// BoxDims is the per-axis box size in millimeters.
type BoxDims struct{ X, Y, Z float64 }

// HSVToRGB converts HSV (each in [0, 1]) to an 8-bit RGB Color.
// Useful for animations that cycle through the rainbow. Hue wraps:
// HSVToRGB(1.5, 1, 1) == HSVToRGB(0.5, 1, 1).
//
// Example: cycle a sphere through the spectrum at 1 cycle per 5
// seconds in your SceneTick:
//
//	c := visuals.HSVToRGB(math.Mod(t/5.0, 1.0), 1, 1)
//	sphere.Color = &c
//	return scene.Update(sphere)
func HSVToRGB(h, s, v float64) Color {
	h -= float64(int(h)) // h mod 1
	if h < 0 {
		h++
	}
	h6 := h * 6.0
	i := int(h6) % 6
	f := h6 - float64(int(h6))
	p, q, tval := v*(1-s), v*(1-s*f), v*(1-s*(1-f))
	var r, g, b float64
	switch i {
	case 0:
		r, g, b = v, tval, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, tval
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = tval, p, v
	default:
		r, g, b = v, p, q
	}
	return Color{R: int(r * 255), G: int(g * 255), B: int(b * 255)}
}

// SnapStep quantizes a value in [lo, hi] to one of nSteps discrete
// values. Use when mutating renderer-respawn-triggering fields
// (color, opacity, ParentFrame, ShowAxesHelper, Invisible) in a
// high-rate tick loop. Scene.Update emits a respawn event once per
// distinct snapped value, so snapping bounds the wire-level
// REMOVE+ADD churn (and the renderer's REMOVED-UUID cache growth)
// to nSteps events per cycle instead of one per tick.
//
// Example — cycle hue through 16 steps per 6-second cycle:
//
//	hueStep := visuals.SnapStep(math.Mod(t/6, 1), 16, 0, 1)
//	c := visuals.HSVToRGB(hueStep, 1, 1)
//	box.Color = &c
//	events, _ := scene.Update(box)
//
// With nSteps=16 and a 6 s cycle, scene.Update emits at most 16
// respawns per 6 s (≈ 2.7 Hz) regardless of tick rate.
func SnapStep(value float64, nSteps int, lo, hi float64) float64 {
	if nSteps <= 0 {
		return lo
	}
	if hi <= lo {
		return lo
	}
	span := hi - lo
	u := (value - lo) / span
	if u < 0 {
		u = 0
	} else if u > 1 {
		u = 1
	}
	step := int(u * float64(nSteps))
	if step >= nSteps {
		step = nSteps - 1
	}
	return lo + (float64(step)/float64(nSteps))*span
}
