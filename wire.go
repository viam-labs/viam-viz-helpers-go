// Wire-format parsers for the apply_events DoCommand verb.
//
// The Python driver serializes its Scene events via
// “viam_visuals.events_to_wire“ which produces a dict shape with
// snake_case keys. The Go visualizer's apply_events receives those
// dicts (as “map[string]any“) and needs to turn them into
// visuals.Item / paths to feed installItemLocked.
//
// We don't add JSON tags directly to Item / Pose / Color / Animation
// because the existing struct API doesn't depend on JSON
// serialization — wiring it would create two parallel encodings to
// keep in sync. Instead we use private wire structs with explicit
// snake_case tags and a small conversion helper.
package visuals

import (
	"encoding/json"
	"fmt"
)

// wireItem mirrors the dict shape Scene.events_to_wire emits on the
// Python side. Snake_case JSON tags match the wire format exactly.
type wireItem struct {
	Type           string         `json:"type"`
	Label          string         `json:"label"`
	ParentFrame    string         `json:"parent_frame,omitempty"`
	Pose           *wirePose      `json:"pose,omitempty"`
	DimsMM         *wireDims      `json:"dims_mm,omitempty"`
	RadiusMM       *float64       `json:"radius_mm,omitempty"`
	LengthMM       *float64       `json:"length_mm,omitempty"`
	MeshPath       string         `json:"mesh_path,omitempty"`
	RawSTL         bool           `json:"raw_stl,omitempty"`
	PointcloudPath string         `json:"pointcloud_path,omitempty"`
	Color          *wireColor     `json:"color,omitempty"`
	Opacity        *float64       `json:"opacity,omitempty"`
	ShowAxesHelper bool           `json:"show_axes_helper,omitempty"`
	Invisible      bool           `json:"invisible,omitempty"`
	Chunked        bool           `json:"chunked,omitempty"`
	ChunkSize      int            `json:"chunk_size,omitempty"`
	Animation      *wireAnimation `json:"animation,omitempty"`
}

type wirePose struct {
	X     float64 `json:"x,omitempty"`
	Y     float64 `json:"y,omitempty"`
	Z     float64 `json:"z,omitempty"`
	OX    float64 `json:"ox,omitempty"`
	OY    float64 `json:"oy,omitempty"`
	OZ    float64 `json:"oz,omitempty"`
	Theta float64 `json:"theta,omitempty"`
}

type wireDims struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type wireColor struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
}

// wireAnimation: drivers push items with Mode="none" almost always —
// the driver is doing the animation client-side. Mode is the only
// field we have to honor for the visualizer's tick logic.
type wireAnimation struct {
	Mode string `json:"mode"`
}

// ItemFromMap parses a wire-format event item dict into a
// visuals.Item. Goes via JSON to leverage the struct tags; the cost
// (two allocations + a marshal/unmarshal pair per item) is small
// next to the rest of an apply_events broadcast cycle.
func ItemFromMap(m map[string]any) (Item, error) {
	if m == nil {
		return Item{}, fmt.Errorf("item is nil")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return Item{}, fmt.Errorf("marshal item map: %w", err)
	}
	var w wireItem
	if err := json.Unmarshal(b, &w); err != nil {
		return Item{}, fmt.Errorf("unmarshal item: %w", err)
	}
	return w.toItem(), nil
}

func (w wireItem) toItem() Item {
	it := Item{
		Type:           w.Type,
		Label:          w.Label,
		ParentFrame:    w.ParentFrame,
		MeshPath:       w.MeshPath,
		RawSTL:         w.RawSTL,
		PointcloudPath: w.PointcloudPath,
		ShowAxesHelper: w.ShowAxesHelper,
		Invisible:      w.Invisible,
		Chunked:        w.Chunked,
		ChunkSize:      w.ChunkSize,
	}
	if w.Pose != nil {
		it.Pose = Pose{
			X: w.Pose.X, Y: w.Pose.Y, Z: w.Pose.Z,
			OX: w.Pose.OX, OY: w.Pose.OY, OZ: w.Pose.OZ,
			Theta: w.Pose.Theta,
		}
	}
	if w.DimsMM != nil {
		it.DimsMM = BoxDims{X: w.DimsMM.X, Y: w.DimsMM.Y, Z: w.DimsMM.Z}
		it.HasDims = true
	}
	if w.RadiusMM != nil {
		it.RadiusMM = *w.RadiusMM
	}
	if w.LengthMM != nil {
		it.LengthMM = *w.LengthMM
	}
	if w.Color != nil {
		c := Color{R: w.Color.R, G: w.Color.G, B: w.Color.B}
		it.Color = &c
	}
	if w.Opacity != nil {
		op := *w.Opacity
		it.Opacity = &op
	}
	if w.Animation != nil {
		it.Animation = Animation{Mode: w.Animation.Mode}
	}
	return it
}

// EventsToWire serializes a list of SceneEvent to the dict form the
// apply_events DoCommand verb accepts. Counterpart of the Python
// viam_visuals.events_to_wire helper.
func EventsToWire(events []SceneEvent) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		rec := map[string]any{"kind": e.Kind, "label": e.Label}
		if e.Kind != EventRemoved {
			rec["item"] = itemToMap(e.Item)
		}
		if len(e.Paths) > 0 {
			rec["paths"] = e.Paths
		}
		out = append(out, rec)
	}
	return out
}

func itemToMap(it Item) map[string]any {
	m := map[string]any{
		"type":  it.Type,
		"label": it.Label,
		"pose": map[string]any{
			"x": it.Pose.X, "y": it.Pose.Y, "z": it.Pose.Z,
			"ox": it.Pose.OX, "oy": it.Pose.OY, "oz": it.Pose.OZ,
			"theta": it.Pose.Theta,
		},
	}
	if it.ParentFrame != "" {
		m["parent_frame"] = it.ParentFrame
	}
	if it.HasDims {
		m["dims_mm"] = map[string]any{
			"x": it.DimsMM.X, "y": it.DimsMM.Y, "z": it.DimsMM.Z,
		}
	}
	if it.RadiusMM != 0 {
		m["radius_mm"] = it.RadiusMM
	}
	if it.LengthMM != 0 {
		m["length_mm"] = it.LengthMM
	}
	if it.MeshPath != "" {
		m["mesh_path"] = it.MeshPath
	}
	if it.RawSTL {
		m["raw_stl"] = true
	}
	if it.PointcloudPath != "" {
		m["pointcloud_path"] = it.PointcloudPath
	}
	if it.Color != nil {
		m["color"] = map[string]any{
			"r": it.Color.R, "g": it.Color.G, "b": it.Color.B,
		}
	}
	if it.Opacity != nil {
		m["opacity"] = *it.Opacity
	}
	if it.ShowAxesHelper {
		m["show_axes_helper"] = true
	}
	if it.Invisible {
		m["invisible"] = true
	}
	if it.Animation.Mode != "" {
		m["animation"] = map[string]any{"mode": it.Animation.Mode}
	}
	return m
}
