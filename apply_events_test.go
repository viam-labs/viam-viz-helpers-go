package visuals

import (
	"context"
	"strings"
	"testing"
	"time"

	commonpb "go.viam.com/api/common/v1"
	"go.viam.com/rdk/services/worldstatestore"
)

// Aliases for the regression test helpers below.
type worldstatestoreChangeForTest = worldstatestore.TransformChange

func asWSChan(ch chan worldstatestore.TransformChange) chan worldstatestore.TransformChange {
	return ch
}

// ---- test helpers -----------------------------------------------------

type fakeHooks struct{}

func (fakeHooks) BuildGeometry(_ Item, _ BaseGeom) (*commonpb.Geometry, error) {
	// Minimal geometry — apply_events doesn't actually need the
	// geom contents, just that BuildGeometry returns a non-error.
	return &commonpb.Geometry{}, nil
}
func (fakeHooks) ReadAsset(_ string) ([]byte, error) { return nil, nil }
func (fakeHooks) ComputeTick(item Item, _ Pose, _ BaseGeom, _ float64) TickResult {
	return TickResult{Pose: item.Pose}
}
func (fakeHooks) IsAnimated(_ Item) bool              { return false }
func (fakeHooks) LoadPreset(_ string) ([]Item, error) { return nil, nil }
func (fakeHooks) BaseGeomForItem(item Item) BaseGeom {
	bg := BaseGeom{}
	if item.HasDims {
		bg.Dims = item.DimsMM
		bg.HasDims = true
	}
	if item.RadiusMM > 0 {
		bg.RadiusMM = item.RadiusMM
	}
	if item.LengthMM > 0 {
		bg.LengthMM = item.LengthMM
	}
	return bg
}

func (fakeHooks) HandleCustomCommand(_ context.Context, _ map[string]any) (map[string]any, bool, error) {
	return nil, false, nil
}

func newBareService(t *testing.T) *SceneServiceBase {
	t.Helper()
	s := &SceneServiceBase{
		Hooks:               fakeHooks{},
		DefaultTickHz:       30,
		DefaultUUIDStrategy: "stable",
		DefaultParentFrame:  "world",
	}
	// Use ReconfigureWith to populate the same fields the framework would.
	if err := s.ReconfigureWith(nil, 30, "stable", "world"); err != nil {
		t.Fatalf("ReconfigureWith: %v", err)
	}
	return s
}

func boxMap(label string) map[string]any {
	return map[string]any{
		"type":      "box",
		"label":     label,
		"pose":      map[string]any{"x": 0.0, "y": 0.0, "z": 0.0, "ox": 0.0, "oy": 0.0, "oz": 1.0, "theta": 0.0},
		"dims_mm":   map[string]any{"x": 100.0, "y": 100.0, "z": 100.0},
		"color":     map[string]any{"r": 255.0, "g": 0.0, "b": 0.0},
		"opacity":   1.0,
		"animation": map[string]any{"mode": "none"},
	}
}

// ---- happy path -------------------------------------------------------

func TestApplyEvents_AddInstallsItem(t *testing.T) {
	s := newBareService(t)
	result, err := s.DoCommand(context.Background(), map[string]any{
		"command": "apply_events",
		"events": []any{
			map[string]any{"kind": "added", "label": "b1", "item": boxMap("b1")},
		},
	})
	if err != nil {
		t.Fatalf("apply_events: %v", err)
	}
	if result["added"] != 1 {
		t.Errorf("expected added=1, got %v", result["added"])
	}
	if _, ok := s.state["b1"]; !ok {
		t.Errorf("expected state to contain 'b1'")
	}
}

func TestApplyEvents_UpdatedReplacesState(t *testing.T) {
	s := newBareService(t)
	// First add.
	_, _ = s.DoCommand(context.Background(), map[string]any{
		"command": "apply_events",
		"events": []any{
			map[string]any{"kind": "added", "label": "b1", "item": boxMap("b1")},
		},
	})

	// Then update with moved pose.
	newItem := boxMap("b1")
	newItem["pose"].(map[string]any)["x"] = 200.0
	result, err := s.DoCommand(context.Background(), map[string]any{
		"command": "apply_events",
		"events": []any{
			map[string]any{
				"kind":  "updated",
				"label": "b1",
				"item":  newItem,
				"paths": []any{"poseInObserverFrame.pose.x"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["updated"] != 1 {
		t.Errorf("expected updated=1, got %v", result["updated"])
	}
	if s.state["b1"].BasePose.X != 200 {
		t.Errorf("expected pose.x=200, got %v", s.state["b1"].BasePose.X)
	}
}

func TestApplyEvents_RemovedDropsState(t *testing.T) {
	s := newBareService(t)
	_, _ = s.DoCommand(context.Background(), map[string]any{
		"command": "apply_events",
		"events": []any{
			map[string]any{"kind": "added", "label": "b1", "item": boxMap("b1")},
		},
	})
	result, _ := s.DoCommand(context.Background(), map[string]any{
		"command": "apply_events",
		"events": []any{
			map[string]any{"kind": "removed", "label": "b1"},
		},
	})
	if result["removed"] != 1 {
		t.Errorf("expected removed=1, got %v", result["removed"])
	}
	if _, ok := s.state["b1"]; ok {
		t.Errorf("'b1' should be gone")
	}
}

// ---- namespace --------------------------------------------------------

func TestApplyEvents_NamespacePrefixesLabels(t *testing.T) {
	s := newBareService(t)
	_, _ = s.DoCommand(context.Background(), map[string]any{
		"command":   "apply_events",
		"namespace": "driver1",
		"events": []any{
			map[string]any{"kind": "added", "label": "obj_a", "item": boxMap("obj_a")},
		},
	})
	if _, ok := s.state["driver1/obj_a"]; !ok {
		t.Errorf("expected 'driver1/obj_a' in state, got labels %v", labelsOf(s.state))
	}
	if _, ok := s.state["obj_a"]; ok {
		t.Errorf("unprefixed label should not exist")
	}
}

func TestApplyEvents_TwoNamespacesCoexist(t *testing.T) {
	s := newBareService(t)
	for _, ns := range []string{"red", "blue"} {
		_, _ = s.DoCommand(context.Background(), map[string]any{
			"command":   "apply_events",
			"namespace": ns,
			"events": []any{
				map[string]any{"kind": "added", "label": "x", "item": boxMap("x")},
			},
		})
	}
	if _, ok := s.state["red/x"]; !ok {
		t.Errorf("expected 'red/x' in state")
	}
	if _, ok := s.state["blue/x"]; !ok {
		t.Errorf("expected 'blue/x' in state")
	}
}

// ---- errors -----------------------------------------------------------

func TestApplyEvents_ErrorsRecordedPerEvent(t *testing.T) {
	s := newBareService(t)
	result, _ := s.DoCommand(context.Background(), map[string]any{
		"command": "apply_events",
		"events": []any{
			map[string]any{"kind": "added", "label": "good", "item": boxMap("good")},
			map[string]any{"kind": "updated", "label": "ghost", "item": boxMap("ghost")},
			map[string]any{"kind": "added", "label": "good", "item": boxMap("good")}, // dup
			map[string]any{"kind": "added", "label": "also_good", "item": boxMap("also_good")},
		},
	})
	if result["added"] != 2 {
		t.Errorf("expected added=2, got %v", result["added"])
	}
	errors := result["errors"].([]string)
	if len(errors) != 2 {
		t.Errorf("expected 2 errors, got %d: %v", len(errors), errors)
	}
}

func TestApplyEvents_UnknownKind(t *testing.T) {
	s := newBareService(t)
	result, _ := s.DoCommand(context.Background(), map[string]any{
		"command": "apply_events",
		"events": []any{
			map[string]any{"kind": "wat", "label": "x"},
		},
	})
	errors := result["errors"].([]string)
	if len(errors) != 1 || !strings.Contains(errors[0], "unknown kind") {
		t.Errorf("expected unknown-kind error, got %v", errors)
	}
}

// ---- round trip --------------------------------------------------------

// Regression: in-process Go→Go driver calls produce []string for
// paths (not []any). The visualizer must extract them as paths or
// the renderer sees UPDATED events with empty UpdatedFields and
// the boxes don't animate. (Found on dell-2 deploy of v0.0.11.)
func TestApplyEvents_PathsAsStringSliceInProcessRoundTrip(t *testing.T) {
	s := newBareService(t)
	_, _ = s.DoCommand(context.Background(), map[string]any{
		"command": "apply_events",
		"events": []any{
			map[string]any{"kind": "added", "label": "b1", "item": boxMap("b1")},
		},
	})

	// Capture broadcasts to assert UpdatedFields is non-empty.
	captured := make(chan struct {
		Paths []string
		Kind  string
	}, 4)
	s.mu.Lock()
	ch := make(chan worldstatestoreChangeForTest, 256)
	s.subscribers = append(s.subscribers, asWSChan(ch))
	s.mu.Unlock()
	go func() {
		for c := range ch {
			captured <- struct {
				Paths []string
				Kind  string
			}{Paths: c.UpdatedFields, Kind: c.ChangeType.String()}
		}
	}()

	// Send an UPDATED event with paths typed as []string — the exact
	// shape the driver-side EventsToWire produces.
	newItem := boxMap("b1")
	newItem["pose"].(map[string]any)["x"] = 200.0
	_, _ = s.DoCommand(context.Background(), map[string]any{
		"command": "apply_events",
		"events": []any{
			map[string]any{
				"kind":  "updated",
				"label": "b1",
				"item":  newItem,
				"paths": []string{"poseInObserverFrame.pose.x"}, // <-- []string, not []any
			},
		},
	})

	// Drain captured events until we see the UPDATED.
	deadline := time.After(100 * time.Millisecond)
	var got struct {
		Paths []string
		Kind  string
	}
	for {
		select {
		case ev := <-captured:
			if ev.Kind == "TRANSFORM_CHANGE_TYPE_UPDATED" {
				got = ev
				goto done
			}
		case <-deadline:
			t.Fatal("never saw UPDATED event")
		}
	}
done:
	if len(got.Paths) != 1 || got.Paths[0] != "poseInObserverFrame.pose.x" {
		t.Errorf("UPDATED with empty/wrong paths (would silently skip in renderer): %v", got.Paths)
	}
}

// Regression: same as above but for the events list itself — driver
// emits []map[string]any directly, not []any. Visualizer must accept
// both shapes.
func TestApplyEvents_EventsListAsMapSlice(t *testing.T) {
	s := newBareService(t)
	// Pass events as []map[string]any (what EventsToWire returns).
	wire := []map[string]any{
		{"kind": "added", "label": "b1", "item": boxMap("b1")},
	}
	result, err := s.DoCommand(context.Background(), map[string]any{
		"command": "apply_events",
		"events":  wire,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["added"] != 1 {
		t.Errorf("expected added=1, got %v (events shape rejection?)", result["added"])
	}
}

func TestApplyEvents_SceneRoundTrip(t *testing.T) {
	scene := NewScene("world")
	box := &Box{Label: "demo", DimsMM: BoxDims{X: 100, Y: 100, Z: 100}}
	events, _ := scene.Add(box)

	s := newBareService(t)
	wire := EventsToWire(events)
	// EventsToWire returns []map[string]any; DoCommand expects []any.
	wireAny := make([]any, len(wire))
	for i, w := range wire {
		wireAny[i] = w
	}
	result, err := s.DoCommand(context.Background(), map[string]any{
		"command": "apply_events",
		"events":  wireAny,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["added"] != 1 {
		t.Errorf("expected added=1, got %v", result["added"])
	}
	if _, ok := s.state["demo"]; !ok {
		t.Errorf("expected 'demo' in state")
	}
}

// ---- helpers ----------------------------------------------------------

func labelsOf(m map[string]*ItemState) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
