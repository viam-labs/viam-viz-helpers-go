package visuals

// SceneServiceBase — inheritable WorldStateStore service base.
//
// A module author who wants to publish a world-state-store scene
// embeds SceneServiceBase in their service struct and provides a
// SceneHooks implementation that fills in the module-specific
// bits — geometry building, asset reading, animation tick math,
// preset lookup, custom DoCommand verbs.
//
// SceneServiceBase owns the generic WSS plumbing: state map,
// subscriber fanout, the animation tick goroutine, UUID strategy,
// and the standard nine DoCommand verbs (list / remove / clear /
// preset / snapshot / set_uuid_strategy).
//
// Usage:
//
//   type myService struct {
//       resource.Named
//       resource.TriviallyCloseable
//       visuals.SceneServiceBase
//   }
//
//   // Implement SceneHooks on *myService.
//   func (s *myService) BuildGeometry(...) (*commonpb.Geometry, error) { ... }
//   func (s *myService) ReadAsset(path string) ([]byte, error) { ... }
//   // ... etc
//
//   // Wire up in your constructor:
//   func newMyService(...) (worldstatestore.Service, error) {
//       s := &myService{Named: ...}
//       s.SceneServiceBase.Hooks = s
//       s.SceneServiceBase.Logger = logger
//       if err := s.Reconfigure(ctx, deps, conf); err != nil {
//           return nil, err
//       }
//       return s, nil
//   }

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	commonpb "go.viam.com/api/common/v1"
	wsspb "go.viam.com/api/service/worldstatestore/v1"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/services/worldstatestore"
)

// SceneHooks is a marker for what gets attached to
// SceneServiceBase.Hooks. Every capability the library uses is an
// optional interface that the Hooks instance may or may not satisfy
// — the library type-asserts at the call site and falls back to a
// built-in default when the assertion fails. The marker exists for
// documentation; functionally, `any` would work equally well.
//
// Available capabilities (each is its own interface in this file):
//
//   - GeometryBuilder       — custom item.Type → proto Geometry mapping.
//                             Default: visuals.BuildBasicGeometry,
//                             handles box/sphere/capsule/point/arrow.
//   - BaseGeomProvider      — custom item.Type → BaseGeom extraction.
//                             Default: visuals.DefaultBaseGeomForItem.
//   - AssetReader           — mesh / pointcloud loading.
//   - PresetLoader          — "preset" DoCommand verb.
//   - CustomCommandHandler  — additional DoCommand verbs.
//   - SceneTicker           — Scene-centric per-frame animation.
//   - LegacyAnimator        — deprecated per-item animation tick.
//
// A service that only publishes static primitives (no presets, no
// custom verbs, no assets) can pass nil Hooks. A service like
// SimpleSceneExample that animates typed objects only needs to
// implement SceneTicker.
type SceneHooks = any

// GeometryBuilder is the optional hook for modules whose item.Type
// set extends beyond the standard primitives (mesh, pointcloud,
// custom shapes). When Hooks doesn't satisfy this, the library
// uses visuals.BuildBasicGeometry.
type GeometryBuilder interface {
	BuildGeometry(item Item, override BaseGeom) (*commonpb.Geometry, error)
}

// BaseGeomProvider is the optional hook for modules whose item.Type
// set extends beyond the standard primitives. When Hooks doesn't
// satisfy this, the library uses visuals.DefaultBaseGeomForItem.
type BaseGeomProvider interface {
	BaseGeomForItem(item Item) BaseGeom
}

// AssetReader is the optional hook for modules that load mesh /
// pointcloud / texture data from disk or another source. Items with
// MeshPath or PointcloudPath fields trigger calls into this; modules
// with no assets can omit it.
type AssetReader interface {
	ReadAsset(path string) ([]byte, error)
}

// PresetLoader is the optional hook for modules that expose named
// scene presets via the "preset" DoCommand verb (or the Reconfigure
// preset config). Modules with no presets can omit it.
type PresetLoader interface {
	LoadPreset(name string) ([]Item, error)
}

// CustomCommandHandler is the optional hook for modules that add
// their own DoCommand verbs beyond the standard set. Return
// (nil, false, nil) for any verb you don't recognize and the base
// will fall through to its default debug-snapshot reply.
type CustomCommandHandler interface {
	HandleCustomCommand(ctx context.Context, command map[string]any) (response map[string]any, handled bool, err error)
}

// LegacyAnimator is the optional legacy per-item animation hook.
//
// Deprecated: implement SceneTicker.SceneTick(scene, t) instead.
// The new API mutates typed Visual objects via the Scene API and
// returns the diff events from scene.Update, avoiding the
// tuple-return shape and field-mask-path bookkeeping here. Existing
// modules using the legacy path still work — the library checks for
// SceneTicker first and falls back to LegacyAnimator only if not
// found.
type LegacyAnimator interface {
	ComputeTick(item Item, basePose Pose, baseGeom BaseGeom, t float64) TickResult
	IsAnimated(item Item) bool
}

// SceneTicker is an optional extension hook a module can implement
// alongside SceneHooks to use the Scene-centric per-frame animation
// API instead of the legacy SceneHooks.ComputeTick path.
//
// When the Hooks instance satisfies this interface, the tick loop
// calls SceneTick(scene, t) every 1/tick_hz seconds. The subclass
// mutates typed Visual / Composite objects in the scene and returns
// the diff events produced by scene.Update(...). The library
// broadcasts the events to subscribers and handles renderer quirks
// (notably: empty-Paths UPDATED events produced by metadata-only
// changes are translated to REMOVE + re-ADD with a fresh UUID so
// the renderer actually paints color / opacity changes).
//
// Example:
//
//	func (s *myService) SceneTick(scene *visuals.Scene, t float64) []visuals.SceneEvent {
//	    s.myBox.Pose = visuals.PoseAt(100*math.Cos(t), 100*math.Sin(t), 100, 0, 0, 1, 0)
//	    c := visuals.HSVToRGB(math.Mod(t/6, 1), 1, 1)
//	    s.myBox.Color = &c
//	    events, _ := scene.Update(s.myBox)
//	    return events
//	}
type SceneTicker interface {
	SceneTick(scene *Scene, t float64) []SceneEvent
}

// ItemState is the per-item runtime state.
type ItemState struct {
	Item            Item
	BasePose        Pose
	BaseGeom        BaseGeom
	UUID            []byte
	Transform       *commonpb.Transform
	VisibleToViewer bool
	ChunkedState    *ChunkedState
}

// ChunkedState carries the parsed PCD body for chunked-delivery
// pointcloud items. Reused for later get_entity_chunk fetches.
type ChunkedState struct {
	HeaderBytes     []byte
	BodyBytes       []byte
	Stride          int
	TotalPoints     int
	ChunkSizePoints int
	NChunks         int
}

// hookBuildGeometry calls the GeometryBuilder hook if present,
// otherwise falls back to BuildBasicGeometry.
func (s *SceneServiceBase) hookBuildGeometry(item Item, override BaseGeom) (*commonpb.Geometry, error) {
	if b, ok := s.Hooks.(GeometryBuilder); ok {
		return b.BuildGeometry(item, override)
	}
	return BuildBasicGeometry(item)
}

// hookBaseGeomForItem calls the BaseGeomProvider hook if present,
// otherwise falls back to DefaultBaseGeomForItem.
func (s *SceneServiceBase) hookBaseGeomForItem(item Item) BaseGeom {
	if p, ok := s.Hooks.(BaseGeomProvider); ok {
		return p.BaseGeomForItem(item)
	}
	return DefaultBaseGeomForItem(item)
}

// SceneServiceBase is the inheritable worldstatestore.Service base.
// A module's service struct embeds it, sets Hooks to an instance
// satisfying whatever optional interfaces the module needs (see
// SceneHooks doc), and gets the WSS plumbing for free: state map,
// subscriber fanout, tick goroutine, standard DoCommand verbs, and
// renderer-quirk workarounds (camelCase paths, REMOVE+ADD respawn
// on metadata changes, UUID rotation on lifecycle re-add).
//
// Two animation paths coexist:
//
//   - Scene-centric (recommended): Hooks implements SceneTicker.
//     SetScene installs typed Visual objects; SceneTick mutates them
//     and returns events from scene.Update. The library translates
//     metadata-touching empty-paths UPDATEDs into respawns.
//   - Legacy per-item (LegacyAnimator): Hooks implements
//     ComputeTick + IsAnimated. Returns a TickResult per animated
//     item per tick; the library handles wire-format emission.
//
// The legacy path is checked only when SceneTicker is not
// implemented — modules can mix paradigms but not within the same
// service instance.
//
// All public fields below are set in the module's constructor
// before Reconfigure is called. Zero values trigger sensible
// fallbacks (see DefaultTickHzOr etc.). The unexported fields hold
// runtime state; access them through methods (State, Mu).
type SceneServiceBase struct {
	Hooks  SceneHooks
	Logger logging.Logger

	// Defaults — set in the module's constructor before calling
	// Reconfigure. Zero values fall back to library defaults
	// (DefaultTickHz: 30 Hz, DefaultUUIDStrategy: "stable",
	// DefaultParentFrame: "world", DefaultChunkSizePoints: 2000,
	// MaxTickHz: 30 Hz).
	DefaultTickHz          float64
	DefaultUUIDStrategy    string
	DefaultParentFrame     string
	DefaultPreset          string
	DefaultChunkSizePoints int
	MaxTickHz              float64

	// Scene-centric API: a typed object-graph that backs the service
	// state. Subclasses install Visuals via SetScene(...) and mutate
	// them in their SceneTick hook (see SceneHooks). The library
	// handles diff'ing, field-mask path emission, and renderer-quirk
	// workarounds (metadata-only → REMOVE+ADD respawn) internally.
	//
	// Allocated lazily in ReconfigureWith / SetScene.
	Scene *Scene

	mu          sync.Mutex
	state       map[string]*ItemState
	subscribers []chan worldstatestore.TransformChange
	tickStop    chan struct{}
	tickDone    chan struct{}
	animT0      time.Time

	// baseVisuals: per-label snapshots of each Visual at SetScene
	// time. The default tick dispatch passes the snapshot as the
	// "rest state" when calling animation.Apply, so Apply math
	// reads from a stable base rather than the in-progress
	// mutation.
	baseVisuals map[string]Visual

	tickHz       float64
	uuidStrategy string
	parentFrame  string

	// Diagnostic counters surfaced in the debug snapshot. Atomic
	// reads aren't needed because all mutations occur under s.mu.
	broadcastsTotal int64
	updatesTotal    int64
	lastBroadcast   broadcastDebug
}

// broadcastDebug captures the most recent broadcast for debugging
// via the {} debug DoCommand. Cheap (the kind is a uint8 and the
// paths are at most a few short strings) and only updated on
// broadcastLocked which already runs under s.mu.
type broadcastDebug struct {
	Kind         string
	Label        string
	Paths        []string
	HasTransform bool
}

// ReconfigureWith does the SceneServiceBase reconfigure. Takes the
// items list the module produced (from config or preset) and the
// effective tick/uuid/parent-frame attributes.
//
// Modules typically call this from their own Reconfigure after
// parsing the config struct.
func (s *SceneServiceBase) ReconfigureWith(
	items []Item,
	tickHz float64,
	uuidStrategy string,
	parentFrame string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tickHz = tickHz
	if s.tickHz == 0 {
		s.tickHz = s.defaultTickHzOr()
	}
	s.uuidStrategy = uuidStrategy
	if s.uuidStrategy == "" {
		s.uuidStrategy = s.defaultUUIDStrategyOr()
	}
	s.parentFrame = parentFrame
	if s.parentFrame == "" {
		s.parentFrame = s.defaultParentFrameOr()
	}

	// Stop any prior tick.
	if s.tickStop != nil {
		close(s.tickStop)
		s.tickStop = nil
	}

	prior := make([]*commonpb.Transform, 0, len(s.state))
	for _, st := range s.state {
		prior = append(prior, st.Transform)
	}
	s.state = map[string]*ItemState{}

	for _, it := range items {
		if err := s.installItemLocked(it); err != nil {
			if s.Logger != nil {
				s.Logger.Warnw("install_item failed", "label", it.Label, "err", err)
			}
		}
	}

	for _, tf := range prior {
		s.broadcastLocked(worldstatestore.TransformChange{
			ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_REMOVED,
			Transform:  tf,
		})
	}
	for _, st := range s.state {
		s.broadcastLocked(worldstatestore.TransformChange{
			ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_ADDED,
			Transform:  st.Transform,
		})
	}

	// Start the tick goroutine if the Hooks implements SceneTicker
	// (new API) OR any item has a declarative animation spec (legacy
	// ComputeTick path).
	_, hasSceneTicker := s.Hooks.(SceneTicker)
	legacy, hasLegacy := s.Hooks.(LegacyAnimator)
	wantsTick := hasSceneTicker
	if !wantsTick && hasLegacy {
		for _, st := range s.state {
			if legacy.IsAnimated(st.Item) {
				wantsTick = true
				break
			}
		}
	}
	if wantsTick {
		s.tickStop = make(chan struct{})
		s.tickDone = make(chan struct{})
		s.animT0 = time.Now()
		go s.tickLoop(s.tickStop, s.tickDone)
	}
	return nil
}

// SetScene installs typed Visual / Composite objects as the new
// scene state. Composites expand to their constituent Visuals; each
// is tracked in s.Scene so subclasses can keep references and mutate
// them on each SceneTick.
//
// Broadcasts REMOVED for any prior state and ADDED for the new
// state, then restarts the tick task if this service uses animation
// (either the new SceneTicker hook or the legacy ComputeTick path).
//
// Example:
//
//	func (s *myService) Reconfigure(ctx, deps, conf) error {
//	    s.myBox = &visuals.Box{Label: "demo", Pose: ..., DimsMM: ..., Color: ...}
//	    return s.SetScene(visuals.SetSceneOpts{
//	        TickHz: 30, UUIDStrategy: "stable", ParentFrame: "world",
//	    }, s.myBox)
//	}
//
// For services that build wire-format Items directly (no typed
// objects), use ReconfigureWith instead.
func (s *SceneServiceBase) SetScene(opts SetSceneOpts, visuals ...interface{}) error {
	parent := opts.ParentFrame
	if parent == "" {
		parent = s.defaultParentFrameOr()
	}
	// Build a fresh Scene so subscribers' initial-burst sees the
	// post-mutation snapshot.
	s.Scene = NewScene(parent)
	addEvents, err := s.Scene.Add(visuals...)
	if err != nil {
		return err
	}
	// Snapshot each Visual's rest state for animation Apply calls.
	s.baseVisuals = map[string]Visual{}
	for _, label := range s.Scene.Labels() {
		if v := s.Scene.Get(label); v != nil {
			s.baseVisuals[label] = snapshotVisual(v)
		}
	}
	items := make([]Item, 0, len(addEvents))
	for _, e := range addEvents {
		items = append(items, e.Item)
	}
	return s.ReconfigureWith(items, opts.TickHz, opts.UUIDStrategy, parent)
}

// DefaultSceneTick is the library's default per-frame animation
// dispatcher. It iterates s.Scene, looks at each Visual's Animation
// field (via the concrete shape pointer's type), and — when the
// concrete animation spec implements Applicable — calls
// spec.Apply(visual, base, t) followed by scene.Update(visual).
// Returns the diff events from all the updates.
//
// Services that embed SceneServiceBase and want this behavior can
// implement SceneTicker by delegating:
//
//	func (s *myService) SceneTick(scene *visuals.Scene, t float64) []visuals.SceneEvent {
//	    return s.DefaultSceneTick(scene, t)
//	}
//
// (Go's value semantics don't support "default method" on an
// interface — embedding doesn't automatically satisfy SceneTicker
// unless the subclass exposes the method.)
func (s *SceneServiceBase) DefaultSceneTick(scene *Scene, t float64) []SceneEvent {
	var events []SceneEvent
	for _, label := range scene.Labels() {
		v := scene.Get(label)
		if v == nil {
			continue
		}
		spec := animSpecFor(v)
		if spec == nil {
			continue
		}
		app, ok := spec.(Applicable)
		if !ok {
			continue
		}
		base := s.baseVisuals[label]
		if base == nil {
			continue
		}
		app.Apply(v, base, t)
		if upd, err := scene.Update(v); err == nil && len(upd) > 0 {
			events = append(events, upd...)
		}
	}
	return events
}

// animSpecFor returns the AnimationSpec field on a concrete shape
// pointer, or nil if the shape has no animation set.
func animSpecFor(v Visual) AnimationSpec {
	switch x := v.(type) {
	case *Box:
		return x.Animation
	case *Sphere:
		return x.Animation
	case *Capsule:
		return x.Animation
	case *Point:
		return x.Animation
	case *Arrow:
		return x.Animation
	case *Mesh:
		return x.Animation
	case *PointCloud:
		return x.Animation
	}
	return nil
}

// SetSceneOpts is the tick/UUID/parent-frame config for SetScene.
// Zero values fall back to the SceneServiceBase defaults.
type SetSceneOpts struct {
	TickHz       float64
	UUIDStrategy string
	ParentFrame  string
}

// Close shuts down the tick goroutine and closes all subscriber
// channels.
func (s *SceneServiceBase) Close(_ context.Context) error {
	s.mu.Lock()
	if s.tickStop != nil {
		close(s.tickStop)
		s.tickStop = nil
	}
	for _, ch := range s.subscribers {
		close(ch)
	}
	s.subscribers = nil
	s.mu.Unlock()
	return nil
}

// installItemLocked: caller holds s.mu. Builds the initial Transform
// and stores into s.state. Handles chunked-delivery setup for
// pointcloud items.
func (s *SceneServiceBase) installItemLocked(item Item) error {
	if _, exists := s.state[item.Label]; exists {
		return fmt.Errorf("duplicate item label %q", item.Label)
	}
	basePose := item.Pose
	if basePose.OX == 0 && basePose.OY == 0 && basePose.OZ == 0 {
		basePose.OZ = 1.0
	}
	baseGeom := s.hookBaseGeomForItem(item)
	uuid := InitialUUID(item.Label, s.uuidStrategy)

	var chunks map[string]any
	var cstate *ChunkedState

	if item.Type == "pointcloud" && item.Chunked {
		reader, ok := s.Hooks.(AssetReader)
		if !ok {
			return fmt.Errorf("pointcloud %q requires Hooks to implement AssetReader", item.Label)
		}
		full, err := reader.ReadAsset(item.PointcloudPath)
		if err != nil {
			return fmt.Errorf("read pointcloud %s: %w", item.PointcloudPath, err)
		}
		header, body, stride, total, err := ParsePCDBinary(full)
		if err != nil {
			return fmt.Errorf("parse PCD %s: %w", item.PointcloudPath, err)
		}
		chunkSize := item.ChunkSize
		if chunkSize <= 0 {
			chunkSize = s.defaultChunkSizeOr()
		}
		nChunks := (total + chunkSize - 1) / chunkSize
		firstChunk, err := BuildPCDChunk(header, body, stride, 0, chunkSize)
		if err != nil {
			return err
		}
		baseGeom.PCDBytesOverride = firstChunk
		chunks = map[string]any{
			"chunk_size":   float64(chunkSize),
			"total":        float64(nChunks),
			"total_points": float64(total),
			"stride":       float64(stride),
		}
		cstate = &ChunkedState{
			HeaderBytes:     header,
			BodyBytes:       body,
			Stride:          stride,
			TotalPoints:     total,
			ChunkSizePoints: chunkSize,
			NChunks:         nChunks,
		}
	}

	geom, err := s.hookBuildGeometry(item, baseGeom)
	if err != nil {
		return err
	}
	tf, err := s.buildTransform(item, basePose, geom, uuid, chunks)
	if err != nil {
		return err
	}
	s.state[item.Label] = &ItemState{
		Item:            item,
		BasePose:        basePose,
		BaseGeom:        baseGeom,
		UUID:            uuid,
		Transform:       tf,
		VisibleToViewer: true,
		ChunkedState:    cstate,
	}
	return nil
}

// buildTransform assembles the *commonpb.Transform from an item +
// pose + geom. Handles vertex-color transcoding to metadata.colors.
func (s *SceneServiceBase) buildTransform(
	item Item, pose Pose, geom *commonpb.Geometry, uuid []byte,
	chunks map[string]any,
) (*commonpb.Transform, error) {
	parent := item.ParentFrame
	if parent == "" {
		parent = s.parentFrame
	}
	var vertexColors [][3]int
	if item.Color == nil && geom != nil && geom.GetMesh() != nil {
		vertexColors = ExtractPLYVertexColors(geom.GetMesh().Mesh)
	}
	md := BuildMetadata(MetadataOpts{
		Color:          item.Color,
		Opacity:        item.Opacity,
		ShowAxesHelper: item.ShowAxesHelper,
		Invisible:      item.Invisible,
		VertexColors:   vertexColors,
		Chunks:         chunks,
	})
	return &commonpb.Transform{
		Uuid:           uuid,
		ReferenceFrame: item.Label,
		PoseInObserverFrame: &commonpb.PoseInFrame{
			ReferenceFrame: parent,
			Pose:           poseToProto(pose),
		},
		PhysicalObject: geom,
		Metadata:       md,
	}, nil
}

// poseToProto: simple conversion (kept private; module doesn't need
// to provide pose-to-proto because the library can do it).
func poseToProto(p Pose) *commonpb.Pose {
	oz := p.OZ
	if p.OX == 0 && p.OY == 0 && p.OZ == 0 {
		oz = 1.0
	}
	return &commonpb.Pose{
		X:     p.X,
		Y:     p.Y,
		Z:     p.Z,
		OX:    p.OX,
		OY:    p.OY,
		OZ:    oz,
		Theta: p.Theta,
	}
}

// ---- WorldStateStore API ---------------------------------------------
//
// The three methods below satisfy the worldstatestore.Service
// interface. Services embedding SceneServiceBase get the
// implementation for free — no override is needed unless you're
// adding cross-cutting behavior (auth, instrumentation, etc.).

// ListUUIDs returns the current UUID of every entity that is visible
// to the viewer right now. Items whose flicker/lifecycle animations
// have them temporarily off-screen are skipped. UUIDs are copied
// before return so the caller can hold them past the next state
// mutation.
func (s *SceneServiceBase) ListUUIDs(_ context.Context, _ map[string]any) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, 0, len(s.state))
	for _, st := range s.state {
		if !st.VisibleToViewer {
			continue
		}
		out = append(out, append([]byte(nil), st.UUID...))
	}
	return out, nil
}

// GetTransform looks up a single entity by UUID and returns the
// cached Transform proto the viewer should display. Returns an
// error if the UUID isn't in the state map, or if the entity is
// currently scene-graph-removed by a flicker/lifecycle animation
// (the entity will reappear with a fresh UUID).
func (s *SceneServiceBase) GetTransform(_ context.Context, uuid []byte, _ map[string]any) (*commonpb.Transform, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.state {
		if string(st.UUID) == string(uuid) {
			if !st.VisibleToViewer {
				return nil, fmt.Errorf("uuid %q is currently not in the scene (flicker/lifecycle animation has it temporarily removed)",
					string(uuid))
			}
			return st.Transform, nil
		}
	}
	return nil, fmt.Errorf("unknown uuid %q", string(uuid))
}

// StreamTransformChanges opens a subscriber channel and streams
// transform changes to the caller. Behavior on join:
//
//  1. The subscriber is added to the fanout list.
//  2. An initial-burst of ADDED events is sent — one per currently-
//     visible entity, with that entity's current UUID and Transform.
//     This brings the new subscriber up to the present moment.
//  3. Subsequent broadcasts (from the animation tick, applyEvents,
//     or DoCommand mutations) flow through the same channel.
//
// The channel has cap=256. If the consumer falls behind and the
// queue fills, subsequent broadcasts non-blocking-drop with a
// warning rather than stalling the tick. Long-running animations
// that respawn fast (high-rate metadata changes — see SnapStep) can
// outrun a slow consumer; snap the source values or accept the drop.
//
// The stream auto-closes when ctx is cancelled; the subscriber is
// removed from the fanout list at that point.
func (s *SceneServiceBase) StreamTransformChanges(ctx context.Context, _ map[string]any) (*worldstatestore.TransformChangeStream, error) {
	ch := make(chan worldstatestore.TransformChange, 256)
	s.mu.Lock()
	s.subscribers = append(s.subscribers, ch)
	for _, st := range s.state {
		if !st.VisibleToViewer {
			continue
		}
		select {
		case ch <- worldstatestore.TransformChange{
			ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_ADDED,
			Transform:  st.Transform,
		}:
		default:
			if s.Logger != nil {
				s.Logger.Warn("subscriber queue full at initial burst; some ADDED events dropped")
			}
		}
	}
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		for i, c := range s.subscribers {
			if c == ch {
				s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
	}()
	return worldstatestore.NewTransformChangeStreamFromChannel(ctx, ch), nil
}

func (s *SceneServiceBase) broadcastLocked(c worldstatestore.TransformChange) {
	s.broadcastsTotal++
	if c.ChangeType == wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_UPDATED {
		s.updatesTotal++
	}
	label := ""
	if c.Transform != nil {
		label = c.Transform.GetReferenceFrame()
	}
	s.lastBroadcast = broadcastDebug{
		Kind:         c.ChangeType.String(),
		Label:        label,
		Paths:        append([]string(nil), c.UpdatedFields...),
		HasTransform: c.Transform != nil,
	}
	for _, ch := range s.subscribers {
		select {
		case ch <- c:
		default:
			if s.Logger != nil {
				s.Logger.Warn("subscriber queue full; dropping event")
			}
		}
	}
}

// ---- animation tick --------------------------------------------------

func (s *SceneServiceBase) tickLoop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	period := time.Duration(float64(time.Second) / math.Max(0.01, s.tickHz))
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := s.tickOnce(); err != nil {
				if s.Logger != nil {
					s.Logger.Warnw("tick failed", "err", err)
				}
			}
		}
	}
}

func (s *SceneServiceBase) tickOnce() error {
	t := time.Since(s.animT0).Seconds()

	// Scene-centric tick path: if the Hooks instance also implements
	// SceneTicker, call SceneTick(scene, t) and apply the returned
	// events through the same machinery applyEvents uses. The
	// subclass gets the typed Scene API; the library handles wire
	// format, subscriber broadcasts, and the metadata-only-respawn
	// intercept.
	if st, ok := s.Hooks.(SceneTicker); ok && s.Scene != nil {
		events := st.SceneTick(s.Scene, t)
		if len(events) > 0 {
			cmd := map[string]any{
				"command": "apply_events",
				"events":  EventsToWire(events),
			}
			if _, err := s.applyEvents(cmd); err != nil && s.Logger != nil {
				s.Logger.Warnw("SceneTick apply failed", "err", err)
			}
		}
		return nil
	}

	// Legacy ComputeTick path: only run if the module implements
	// LegacyAnimator. Modules using only the new SceneTicker API
	// (or no animation at all) don't need to stub these methods.
	legacy, hasLegacy := s.Hooks.(LegacyAnimator)
	if !hasLegacy {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.state {
		if !legacy.IsAnimated(st.Item) {
			continue
		}
		res := legacy.ComputeTick(st.Item, st.BasePose, st.BaseGeom, t)
		if res.Overrides != nil && res.Overrides.InScene != nil {
			wantIn := *res.Overrides.InScene
			was := st.VisibleToViewer
			if wantIn && !was {
				rotate := true
				if st.Item.Animation.RotateUUIDOnReadd != nil {
					rotate = *st.Item.Animation.RotateUUIDOnReadd
				}
				if rotate {
					st.UUID = VersionedUUID(st.Item.Label)
				}
				itemForAdd := st.Item
				if res.Overrides.Color != nil {
					c := *res.Overrides.Color
					itemForAdd.Color = &c
				}
				if res.Overrides.Opacity != nil {
					op := *res.Overrides.Opacity
					itemForAdd.Opacity = &op
				}
				geom, err := s.hookBuildGeometry(st.Item, res.Geom)
				if err != nil {
					return err
				}
				newTF, err := s.buildTransform(itemForAdd, res.Pose, geom, st.UUID, nil)
				if err != nil {
					return err
				}
				st.Transform = newTF
				st.VisibleToViewer = true
				s.broadcastLocked(worldstatestore.TransformChange{
					ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_ADDED,
					Transform:  newTF,
				})
				continue
			}
			if !wantIn && was {
				st.VisibleToViewer = false
				s.broadcastLocked(worldstatestore.TransformChange{
					ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_REMOVED,
					Transform:  st.Transform,
				})
				continue
			}
			if !wantIn && !was {
				continue
			}
			// Both on — fall through to UPDATED.
		}
		if len(res.Paths) == 0 {
			continue
		}
		geom, err := s.hookBuildGeometry(st.Item, res.Geom)
		if err != nil {
			return err
		}
		itemForTF := st.Item
		if res.Overrides != nil {
			if res.Overrides.Color != nil {
				c := *res.Overrides.Color
				itemForTF.Color = &c
			}
			if res.Overrides.Opacity != nil {
				op := *res.Overrides.Opacity
				itemForTF.Opacity = &op
			}
		}
		if s.uuidStrategy == "stable" {
			newTF, err := s.buildTransform(itemForTF, res.Pose, geom, st.UUID, nil)
			if err != nil {
				return err
			}
			st.Transform = newTF
			s.broadcastLocked(worldstatestore.TransformChange{
				ChangeType:    wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_UPDATED,
				Transform:     newTF,
				UpdatedFields: append([]string(nil), res.Paths...),
			})
		} else {
			oldTF := st.Transform
			st.UUID = VersionedUUID(st.Item.Label)
			newTF, err := s.buildTransform(itemForTF, res.Pose, geom, st.UUID, nil)
			if err != nil {
				return err
			}
			st.Transform = newTF
			s.broadcastLocked(worldstatestore.TransformChange{
				ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_REMOVED,
				Transform:  oldTF,
			})
			s.broadcastLocked(worldstatestore.TransformChange{
				ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_ADDED,
				Transform:  newTF,
			})
		}
	}
	return nil
}

// ---- DoCommand -------------------------------------------------------

// DoCommand handles the standard set of WSS verbs. Unknown verbs fall
// through to the hooks' HandleCustomCommand; if that returns
// handled=false, returns a debug snapshot.
func (s *SceneServiceBase) DoCommand(ctx context.Context, command map[string]any) (map[string]any, error) {
	cmd, _ := command["command"].(string)

	switch cmd {
	case "list":
		s.mu.Lock()
		defer s.mu.Unlock()
		items := make([]map[string]any, 0, len(s.state))
		labels := make([]string, 0, len(s.state))
		for k := range s.state {
			labels = append(labels, k)
		}
		sort.Strings(labels)
		for _, label := range labels {
			st := s.state[label]
			items = append(items, map[string]any{
				"label":          label,
				"type":           st.Item.Type,
				"uuid":           string(st.UUID),
				"animation_mode": st.Item.Animation.Mode,
			})
		}
		return map[string]any{"items": items}, nil

	case "remove":
		label, _ := command["label"].(string)
		if label == "" {
			return nil, errors.New("remove requires a 'label'")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		st, ok := s.state[label]
		if !ok {
			return map[string]any{"removed": false}, nil
		}
		delete(s.state, label)
		s.broadcastLocked(worldstatestore.TransformChange{
			ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_REMOVED,
			Transform:  st.Transform,
		})
		return map[string]any{"removed": true}, nil

	case "clear":
		s.mu.Lock()
		defer s.mu.Unlock()
		count := len(s.state)
		for _, st := range s.state {
			s.broadcastLocked(worldstatestore.TransformChange{
				ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_REMOVED,
				Transform:  st.Transform,
			})
		}
		s.state = map[string]*ItemState{}
		return map[string]any{"removed_count": count}, nil

	case "preset":
		name, _ := command["name"].(string)
		if name == "" {
			return nil, errors.New("preset requires a 'name'")
		}
		loader, ok := s.Hooks.(PresetLoader)
		if !ok {
			return nil, errors.New("this service does not support presets")
		}
		items, err := loader.LoadPreset(name)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		prior := make([]*commonpb.Transform, 0, len(s.state))
		for _, st := range s.state {
			prior = append(prior, st.Transform)
		}
		s.state = map[string]*ItemState{}
		for _, it := range items {
			if err := s.installItemLocked(it); err != nil {
				if s.Logger != nil {
					s.Logger.Warnw("preset install failed", "label", it.Label, "err", err)
				}
			}
		}
		for _, tf := range prior {
			s.broadcastLocked(worldstatestore.TransformChange{
				ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_REMOVED,
				Transform:  tf,
			})
		}
		for _, st := range s.state {
			s.broadcastLocked(worldstatestore.TransformChange{
				ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_ADDED,
				Transform:  st.Transform,
			})
		}
		return map[string]any{"loaded": name, "count": len(s.state)}, nil

	case "set_uuid_strategy":
		strategy, _ := command["strategy"].(string)
		ok := false
		for _, v := range ValidStrategies {
			if v == strategy {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("strategy must be one of %v, got %q", ValidStrategies, strategy)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.uuidStrategy = strategy
		return map[string]any{"strategy": strategy}, nil

	case "snapshot":
		s.mu.Lock()
		defer s.mu.Unlock()
		items := []map[string]any{}
		labels := make([]string, 0, len(s.state))
		for k := range s.state {
			labels = append(labels, k)
		}
		sort.Strings(labels)
		for _, l := range labels {
			items = append(items, itemAsJSON(s.state[l].Item))
		}
		return map[string]any{"config": map[string]any{
			"tick_hz":       s.tickHz,
			"uuid_strategy": s.uuidStrategy,
			"parent_frame":  s.parentFrame,
			"items":         items,
		}}, nil

	case "apply_events":
		return s.applyEvents(command)
	}

	// Custom verbs through the optional CustomCommandHandler hook.
	if handler, ok := s.Hooks.(CustomCommandHandler); ok {
		resp, handled, err := handler.HandleCustomCommand(ctx, command)
		if err != nil {
			return nil, err
		}
		if handled {
			return resp, nil
		}
	}

	// Default: debug snapshot.
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{
		"tick_hz":          s.tickHz,
		"uuid_strategy":    s.uuidStrategy,
		"parent_frame":     s.parentFrame,
		"item_count":       len(s.state),
		"subscriber_count": len(s.subscribers),
		"tick_running":     s.tickStop != nil,
		"broadcasts_total": s.broadcastsTotal,
		"updates_total":    s.updatesTotal,
		"last_broadcast": map[string]any{
			"kind":          s.lastBroadcast.Kind,
			"label":         s.lastBroadcast.Label,
			"paths":         s.lastBroadcast.Paths,
			"has_transform": s.lastBroadcast.HasTransform,
		},
	}, nil
}

// coerceEventsSlice normalizes the events payload. May arrive as
// []map[string]any (in-process Go: what EventsToWire emits) or as
// []any (cross-process gRPC via structpb).
func coerceEventsSlice(v any) []any {
	switch tv := v.(type) {
	case []map[string]any:
		out := make([]any, len(tv))
		for i, m := range tv {
			out[i] = m
		}
		return out
	case []any:
		return tv
	}
	return nil
}

// coerceStringSlice extracts a []string from a map value that may be
// typed []string (in-process Go), []any-of-string (gRPC via structpb),
// or nil/missing. Anything else returns nil.
func coerceStringSlice(v any) []string {
	switch tv := v.(type) {
	case []string:
		return tv
	case []any:
		out := make([]string, 0, len(tv))
		for _, x := range tv {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// applyEvents handles the apply_events DoCommand verb — the batched
// wire-format input the driver→visualizer pipeline sends. Mirrors
// the Python implementation. Errors are recorded per-event so a
// single bad event doesn't abort the batch.
func (s *SceneServiceBase) applyEvents(command map[string]any) (map[string]any, error) {
	rawEvents := coerceEventsSlice(command["events"])
	namespace, _ := command["namespace"].(string)
	prefix := ""
	if namespace != "" {
		prefix = namespace + "/"
	}

	var added, updated, removed int
	errors := []string{}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i, raw := range rawEvents {
		evt, ok := raw.(map[string]any)
		if !ok {
			errors = append(errors, fmt.Sprintf("event[%d]: not a dict", i))
			continue
		}
		kind, _ := evt["kind"].(string)
		rawLabel, _ := evt["label"].(string)
		if rawLabel == "" {
			errors = append(errors, fmt.Sprintf("event[%d]: missing 'label'", i))
			continue
		}
		label := prefix + rawLabel

		switch kind {
		case "added":
			itemMap, _ := evt["item"].(map[string]any)
			item, err := ItemFromMap(itemMap)
			if err != nil {
				errors = append(errors, fmt.Sprintf("event[%d] (%q): %v", i, rawLabel, err))
				continue
			}
			item.Label = label
			if _, exists := s.state[label]; exists {
				errors = append(errors, fmt.Sprintf("event[%d] (%q): label already exists", i, rawLabel))
				continue
			}
			if err := s.installItemLocked(item); err != nil {
				errors = append(errors, fmt.Sprintf("event[%d] (%q): %v", i, rawLabel, err))
				continue
			}
			s.broadcastLocked(worldstatestore.TransformChange{
				ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_ADDED,
				Transform:  s.state[label].Transform,
			})
			added++

		case "updated":
			st, ok := s.state[label]
			if !ok {
				errors = append(errors, fmt.Sprintf("event[%d] (%q): unknown label", i, rawLabel))
				continue
			}
			itemMap, _ := evt["item"].(map[string]any)
			newItem, err := ItemFromMap(itemMap)
			if err != nil {
				errors = append(errors, fmt.Sprintf("event[%d] (%q): %v", i, rawLabel, err))
				continue
			}
			newItem.Label = label
			// "paths" may arrive as []string (in-process Go→Go call)
			// or []any (cross-process gRPC where structpb erases the
			// concrete element type). Handle both.
			paths := coerceStringSlice(evt["paths"])
			basePose := newItem.Pose
			if basePose.OX == 0 && basePose.OY == 0 && basePose.OZ == 0 {
				basePose.OZ = 1.0
			}

			if len(paths) == 0 {
				// Empty paths means a metadata-only change (color /
				// opacity / show_axes_helper / invisible). The
				// renderer's UPDATED handler drops metadata.* paths,
				// so a plain UPDATED would be a no-op at the viewer.
				// Respawn: REMOVE the entity with its current UUID,
				// then ADD it back with a fresh UUID so the renderer
				// re-reads metadata at spawn.
				oldTF := st.Transform
				s.broadcastLocked(worldstatestore.TransformChange{
					ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_REMOVED,
					Transform:  oldTF,
				})
				newUUID := VersionedUUID(label)
				st.Item = newItem
				st.BasePose = basePose
				st.BaseGeom = s.hookBaseGeomForItem(newItem)
				geom, err := s.hookBuildGeometry(newItem, st.BaseGeom)
				if err != nil {
					errors = append(errors, fmt.Sprintf("event[%d] (%q): build geom: %v", i, rawLabel, err))
					continue
				}
				newTF, err := s.buildTransform(newItem, basePose, geom, newUUID, nil)
				if err != nil {
					errors = append(errors, fmt.Sprintf("event[%d] (%q): build transform: %v", i, rawLabel, err))
					continue
				}
				st.UUID = newUUID
				st.Transform = newTF
				s.broadcastLocked(worldstatestore.TransformChange{
					ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_ADDED,
					Transform:  newTF,
				})
				updated++
				continue
			}

			st.Item = newItem
			st.BasePose = basePose
			st.BaseGeom = s.hookBaseGeomForItem(newItem)
			geom, err := s.hookBuildGeometry(newItem, st.BaseGeom)
			if err != nil {
				errors = append(errors, fmt.Sprintf("event[%d] (%q): build geom: %v", i, rawLabel, err))
				continue
			}
			tf, err := s.buildTransform(newItem, basePose, geom, st.UUID, nil)
			if err != nil {
				errors = append(errors, fmt.Sprintf("event[%d] (%q): build transform: %v", i, rawLabel, err))
				continue
			}
			st.Transform = tf
			s.broadcastLocked(worldstatestore.TransformChange{
				ChangeType:    wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_UPDATED,
				Transform:     tf,
				UpdatedFields: paths,
			})
			updated++

		case "removed":
			st, ok := s.state[label]
			if !ok {
				continue // idempotent
			}
			delete(s.state, label)
			s.broadcastLocked(worldstatestore.TransformChange{
				ChangeType: wsspb.TransformChangeType_TRANSFORM_CHANGE_TYPE_REMOVED,
				Transform:  st.Transform,
			})
			removed++

		default:
			errors = append(errors, fmt.Sprintf("event[%d] (%q): unknown kind %q", i, rawLabel, kind))
		}
	}
	return map[string]any{
		"applied": added + updated + removed,
		"added":   added,
		"updated": updated,
		"removed": removed,
		"errors":  errors,
	}, nil
}

// ---- helpers --------------------------------------------------------

// State returns the read-locked state map for module access (e.g.,
// custom DoCommand verbs that need to look up an item by label).
// Caller must hold s.Mu() while iterating; returned map is the
// same underlying map.
func (s *SceneServiceBase) State() map[string]*ItemState {
	return s.state
}

// Mu returns the base's mutex so module-side custom verbs can hold
// it across multi-step operations on the state map.
func (s *SceneServiceBase) Mu() *sync.Mutex {
	return &s.mu
}

// TickStopChan returns the current tick goroutine's stop channel,
// or nil if no tick task is running. Module code shouldn't need
// this directly — exposed mostly for debug introspection.
func (s *SceneServiceBase) TickStopChan() chan struct{} { return s.tickStop }

func (s *SceneServiceBase) defaultTickHzOr() float64 {
	if s.DefaultTickHz > 0 {
		return s.DefaultTickHz
	}
	return 30.0
}

func (s *SceneServiceBase) defaultUUIDStrategyOr() string {
	if s.DefaultUUIDStrategy != "" {
		return s.DefaultUUIDStrategy
	}
	return "stable"
}

func (s *SceneServiceBase) defaultParentFrameOr() string {
	if s.DefaultParentFrame != "" {
		return s.DefaultParentFrame
	}
	return "world"
}

func (s *SceneServiceBase) defaultChunkSizeOr() int {
	if s.DefaultChunkSizePoints > 0 {
		return s.DefaultChunkSizePoints
	}
	return 1000
}

// itemAsJSON: best-effort round-trip back to a JSON-ish item dict
// for the snapshot DoCommand verb.
func itemAsJSON(it Item) map[string]any {
	m := map[string]any{
		"type":  it.Type,
		"label": it.Label,
	}
	if it.ParentFrame != "" {
		m["parent_frame"] = it.ParentFrame
	}
	m["pose"] = map[string]any{
		"x": it.Pose.X, "y": it.Pose.Y, "z": it.Pose.Z,
		"ox": it.Pose.OX, "oy": it.Pose.OY, "oz": it.Pose.OZ,
		"theta": it.Pose.Theta,
	}
	if it.HasDims {
		m["dims_mm"] = map[string]any{"x": it.DimsMM.X, "y": it.DimsMM.Y, "z": it.DimsMM.Z}
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
	if it.PointcloudPath != "" {
		m["pointcloud_path"] = it.PointcloudPath
	}
	if it.Color != nil {
		m["color"] = map[string]any{"r": it.Color.R, "g": it.Color.G, "b": it.Color.B}
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
	if it.Chunked {
		m["chunked"] = true
		if it.ChunkSize > 0 {
			m["chunk_size"] = it.ChunkSize
		}
	}
	if IsAnimated(it.Animation) {
		am := map[string]any{"mode": it.Animation.Mode}
		if it.Animation.PeriodS != 0 {
			am["period_s"] = it.Animation.PeriodS
		}
		if it.Animation.AmplitudeMM != 0 {
			am["amplitude_mm"] = it.Animation.AmplitudeMM
		}
		if it.Animation.Axis != "" {
			am["axis"] = it.Animation.Axis
		}
		m["animation"] = am
	}
	return m
}

// EncodeBase64 is exported because module-side custom DoCommand
// verbs (e.g., get_entity_chunk) commonly need to base64-encode
// bytes for the response, and importing encoding/base64 directly is
// boilerplate.
func EncodeBase64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
