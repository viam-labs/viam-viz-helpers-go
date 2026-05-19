package visuals

// Item is the denormalized item representation the service consumes.
//
// It carries every per-shape field as a top-level union; only the
// fields relevant to the item's Type are meaningful. Typical
// construction goes through one of the typed Visual subtypes (Box,
// Sphere, …) and their ToItem method — the union is necessary at
// the service / animation-tick layer but should not be the
// primary author-facing surface.
type Item struct {
	Type           string
	Label          string
	ParentFrame    string
	Pose           Pose
	DimsMM         BoxDims
	HasDims        bool
	RadiusMM       float64
	LengthMM       float64
	MeshPath       string
	RawSTL         bool
	PointcloudPath string
	Color          *Color
	Opacity        *float64
	ShowAxesHelper bool
	Invisible      bool
	Chunked        bool
	ChunkSize      int
	Animation      Animation
}
