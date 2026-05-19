package visuals

import (
	"sort"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

// ---- add ---------------------------------------------------------------

func TestScene_Add_Single(t *testing.T) {
	s := NewScene("world")
	b := &Box{Label: "b", DimsMM: BoxDims{X: 100, Y: 200, Z: 50}}
	events, err := s.Add(b)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != EventAdded || events[0].Label != "b" {
		t.Errorf("unexpected event: %+v", events[0])
	}
}

func TestScene_Add_Multiple(t *testing.T) {
	s := NewScene("world")
	events, err := s.Add(
		&Box{Label: "b1", DimsMM: BoxDims{X: 100, Y: 100, Z: 100}},
		&Sphere{Label: "s1", RadiusMM: 50},
	)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if s.Len() != 2 {
		t.Errorf("scene len = %d, want 2", s.Len())
	}
}

func TestScene_Add_Duplicate(t *testing.T) {
	s := NewScene("world")
	if _, err := s.Add(&Box{Label: "dup", DimsMM: BoxDims{X: 1, Y: 1, Z: 1}}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Add(&Sphere{Label: "dup", RadiusMM: 10})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate error, got %v", err)
	}
}

func TestScene_Add_AtomicOnBatchCollision(t *testing.T) {
	s := NewScene("world")
	if _, err := s.Add(&Box{Label: "a", DimsMM: BoxDims{X: 1, Y: 1, Z: 1}}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Add(
		&Sphere{Label: "b", RadiusMM: 1},
		&Box{Label: "a", DimsMM: BoxDims{X: 2, Y: 2, Z: 2}},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	// 'b' must not have leaked in.
	if s.Contains("b") {
		t.Error("partial add leaked 'b' into scene")
	}
}

func TestScene_Add_Composite_Expands(t *testing.T) {
	s := NewScene("world")
	frame := CoordinateFrame{Label: "frame", SizeMM: 100}
	events, err := s.Add(frame)
	if err != nil {
		t.Fatalf("add composite: %v", err)
	}
	// Anchor sphere + 3 axis capsules.
	if len(events) != 4 {
		t.Fatalf("expected 4 events for CoordinateFrame, got %d", len(events))
	}
	got := s.Labels()
	want := []string{"frame", "frame_axis_x", "frame_axis_y", "frame_axis_z"}
	sort.Strings(want)
	if !equalStrings(got, want) {
		t.Errorf("labels = %v, want %v", got, want)
	}
}

// ---- update ------------------------------------------------------------

func TestScene_Update_PoseEmitsPerAxisPaths(t *testing.T) {
	s := NewScene("world")
	b := &Box{Label: "b", DimsMM: BoxDims{X: 100, Y: 100, Z: 100}}
	if _, err := s.Add(b); err != nil {
		t.Fatal(err)
	}
	b.Pose = Pose{X: 200, Y: -50}
	events, err := s.Update(b)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 update event, got %d", len(events))
	}
	paths := setOf(events[0].Paths)
	if !paths["poseInObserverFrame.pose.x"] || !paths["poseInObserverFrame.pose.y"] {
		t.Errorf("expected x and y paths, got %v", events[0].Paths)
	}
	if paths["poseInObserverFrame.pose.z"] || paths["poseInObserverFrame.pose.theta"] {
		t.Errorf("unexpected unchanged paths: %v", events[0].Paths)
	}
}

func TestScene_Update_MetadataOnlyChangeYieldsEmptyPathsEvent(t *testing.T) {
	// Color / opacity changes emit an UPDATED event with empty
	// Paths — the signal to consumers (SceneServiceBase /
	// applyEvents) that a renderer respawn (REMOVE + re-ADD with a
	// fresh UUID) is required. The renderer's UPDATED handler
	// drops metadata.* paths, so a plain UPDATED would be a no-op
	// at the viewer; the empty-Paths signal lets the consumer
	// rewrite the event into REMOVE + ADD on the wire.
	s := NewScene("world")
	b := &Box{
		Label:   "b",
		DimsMM:  BoxDims{X: 100, Y: 100, Z: 100},
		Color:   &Color{R: 255, G: 0, B: 0},
		Opacity: ptr(1.0),
	}
	if _, err := s.Add(b); err != nil {
		t.Fatal(err)
	}
	b.Color = &Color{R: 0, G: 255, B: 0}
	b.Opacity = ptr(0.5)
	events, err := s.Update(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d: %v", len(events), events)
	}
	if events[0].Kind != EventUpdated {
		t.Errorf("expected EventUpdated, got %v", events[0].Kind)
	}
	if events[0].Label != "b" {
		t.Errorf("expected label \"b\", got %q", events[0].Label)
	}
	if len(events[0].Paths) != 0 {
		t.Errorf("expected empty Paths (respawn signal), got %v", events[0].Paths)
	}
}

func TestScene_Update_NoChanges_NoEvent(t *testing.T) {
	s := NewScene("world")
	b := &Box{Label: "b", DimsMM: BoxDims{X: 100, Y: 100, Z: 100}}
	if _, err := s.Add(b); err != nil {
		t.Fatal(err)
	}
	events, err := s.Update(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Errorf("expected no events, got %v", events)
	}
}

func TestScene_Update_SphereRadius(t *testing.T) {
	s := NewScene("world")
	sp := &Sphere{Label: "sp", RadiusMM: 50}
	if _, err := s.Add(sp); err != nil {
		t.Fatal(err)
	}
	sp.RadiusMM = 80
	events, err := s.Update(sp)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || len(events[0].Paths) != 1 ||
		events[0].Paths[0] != "physicalObject.geometryType.value.radiusMm" {
		t.Errorf("unexpected events: %+v", events)
	}
}

func TestScene_Update_BoxDimsPerAxis(t *testing.T) {
	s := NewScene("world")
	b := &Box{Label: "b", DimsMM: BoxDims{X: 100, Y: 100, Z: 100}}
	if _, err := s.Add(b); err != nil {
		t.Fatal(err)
	}
	b.DimsMM = BoxDims{X: 200, Y: 100, Z: 100} // only x changed
	events, err := s.Update(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || len(events[0].Paths) != 1 ||
		events[0].Paths[0] != "physicalObject.geometryType.value.dimsMm.x" {
		t.Errorf("expected only dimsMm.x path, got %+v", events[0].Paths)
	}
}

func TestScene_Update_UnknownLabel(t *testing.T) {
	s := NewScene("world")
	_, err := s.Update(&Box{Label: "ghost", DimsMM: BoxDims{X: 1, Y: 1, Z: 1}})
	if err == nil || !strings.Contains(err.Error(), "unknown label") {
		t.Errorf("expected unknown-label error, got %v", err)
	}
}

func TestScene_Update_Snapshots_Recommit(t *testing.T) {
	// After update, the committed snapshot is post-update; a second
	// identical mutation produces no event.
	s := NewScene("world")
	b := &Box{Label: "b", DimsMM: BoxDims{X: 100, Y: 100, Z: 100}}
	if _, err := s.Add(b); err != nil {
		t.Fatal(err)
	}
	b.Pose = Pose{X: 200}
	first, err := s.Update(b)
	if err != nil || len(first) != 1 {
		t.Fatalf("first update: %v %v", first, err)
	}
	again, err := s.Update(b)
	if err != nil || len(again) != 0 {
		t.Errorf("expected empty re-update, got %v %v", again, err)
	}
}

// ---- add_or_update -----------------------------------------------------

func TestScene_AddOrUpdate_AddsNew(t *testing.T) {
	s := NewScene("world")
	events, err := s.AddOrUpdate(&Box{Label: "b", DimsMM: BoxDims{X: 1, Y: 1, Z: 1}})
	if err != nil || len(events) != 1 || events[0].Kind != EventAdded {
		t.Errorf("unexpected: %v %v", events, err)
	}
}

func TestScene_AddOrUpdate_UpdatesExisting(t *testing.T) {
	s := NewScene("world")
	b := &Box{Label: "b", DimsMM: BoxDims{X: 100, Y: 100, Z: 100}}
	if _, err := s.Add(b); err != nil {
		t.Fatal(err)
	}
	b.Pose = Pose{X: 50}
	events, err := s.AddOrUpdate(b)
	if err != nil || len(events) != 1 || events[0].Kind != EventUpdated {
		t.Errorf("unexpected: %v %v", events, err)
	}
}

// ---- remove ------------------------------------------------------------

func TestScene_Remove_ByObject(t *testing.T) {
	s := NewScene("world")
	b := &Box{Label: "b", DimsMM: BoxDims{X: 1, Y: 1, Z: 1}}
	if _, err := s.Add(b); err != nil {
		t.Fatal(err)
	}
	events := s.Remove(b)
	if len(events) != 1 || events[0].Kind != EventRemoved || events[0].Label != "b" {
		t.Errorf("unexpected: %+v", events)
	}
	if s.Len() != 0 {
		t.Errorf("expected empty scene, got len %d", s.Len())
	}
}

func TestScene_Remove_ByLabelString(t *testing.T) {
	s := NewScene("world")
	if _, err := s.Add(&Box{Label: "b", DimsMM: BoxDims{X: 1, Y: 1, Z: 1}}); err != nil {
		t.Fatal(err)
	}
	events := s.Remove("b")
	if len(events) != 1 || events[0].Kind != EventRemoved {
		t.Errorf("unexpected: %+v", events)
	}
}

func TestScene_Remove_UnknownIsSilent(t *testing.T) {
	s := NewScene("world")
	if _, err := s.Add(&Box{Label: "a", DimsMM: BoxDims{X: 1, Y: 1, Z: 1}}); err != nil {
		t.Fatal(err)
	}
	events := s.Remove("missing")
	if len(events) != 0 {
		t.Errorf("expected no events for unknown label, got %+v", events)
	}
	if !s.Contains("a") {
		t.Error("scene should still contain 'a'")
	}
}

func TestScene_Remove_CompositeAllConstituents(t *testing.T) {
	s := NewScene("world")
	frame := CoordinateFrame{Label: "frame", SizeMM: 100}
	if _, err := s.Add(frame); err != nil {
		t.Fatal(err)
	}
	events := s.Remove(frame)
	if len(events) != 4 {
		t.Errorf("expected 4 removed events, got %d", len(events))
	}
	if s.Len() != 0 {
		t.Errorf("expected empty scene, got len %d", s.Len())
	}
}

func TestScene_Clear(t *testing.T) {
	s := NewScene("world")
	if _, err := s.Add(
		&Box{Label: "b1", DimsMM: BoxDims{X: 1, Y: 1, Z: 1}},
		&Sphere{Label: "s1", RadiusMM: 10},
	); err != nil {
		t.Fatal(err)
	}
	events := s.Clear()
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
	if s.Len() != 0 {
		t.Errorf("scene should be empty after Clear")
	}
}

// ---- introspection -----------------------------------------------------

func TestScene_Get_ReturnsLiveReference(t *testing.T) {
	s := NewScene("world")
	b := &Box{Label: "b", DimsMM: BoxDims{X: 100, Y: 100, Z: 100}}
	if _, err := s.Add(b); err != nil {
		t.Fatal(err)
	}
	got := s.Get("b")
	if got != Visual(b) {
		t.Errorf("Get returned different reference: %v != %v", got, b)
	}
}

func TestScene_ParentFrame(t *testing.T) {
	if NewScene("world").ParentFrame() != "world" {
		t.Error("default parent frame should be 'world'")
	}
	if NewScene("robot_arm:eoa").ParentFrame() != "robot_arm:eoa" {
		t.Error("custom parent frame not preserved")
	}
	if NewScene("").ParentFrame() != "world" {
		t.Error("empty parent frame should default to 'world'")
	}
}

// ---- helpers -----------------------------------------------------------

func setOf(xs []string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		out[x] = true
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
