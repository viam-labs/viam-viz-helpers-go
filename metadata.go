package visuals

import (
	"encoding/base64"
	"math"

	structpb "google.golang.org/protobuf/types/known/structpb"
)

// MetadataOpts controls what BuildMetadata emits. Each field maps
// to a top-level key in the Transform.metadata Struct that the
// viewer reads.
type MetadataOpts struct {
	Color          *Color
	Opacity        *float64
	ShowAxesHelper bool
	Invisible      bool
	VertexColors   [][3]int
	Chunks         map[string]any
}

// BuildMetadata encodes Transform.metadata in the viewer's schema.
//
// Schema comes from viamrobotics/visualization (draw/transform.go).
// All five required keys (colors, color_format, opacities,
// show_axes_helper, invisible) are always emitted — omitting any
// of them produces an invisible entity. Optional Chunks declares
// chunked delivery of a large entity (experimental wire contract).
func BuildMetadata(opts MetadataOpts) *structpb.Struct {
	fields := map[string]any{}

	if len(opts.VertexColors) > 0 {
		packed := make([]byte, 0, len(opts.VertexColors)*3)
		for _, c := range opts.VertexColors {
			packed = append(packed,
				byte(clampU8(c[0])), byte(clampU8(c[1])), byte(clampU8(c[2])))
		}
		fields["colors"] = base64.StdEncoding.EncodeToString(packed)
	} else if opts.Color != nil {
		rgb := []byte{
			byte(clampU8(opts.Color.R)),
			byte(clampU8(opts.Color.G)),
			byte(clampU8(opts.Color.B)),
		}
		fields["colors"] = base64.StdEncoding.EncodeToString(rgb)
	} else {
		fields["colors"] = ""
	}
	fields["color_format"] = 1.0 // COLOR_FORMAT_RGB

	alpha := 255
	if opts.Opacity != nil {
		alpha = clampU8(int(math.Round(*opts.Opacity * 255)))
	}
	fields["opacities"] = base64.StdEncoding.EncodeToString([]byte{byte(alpha)})
	fields["show_axes_helper"] = opts.ShowAxesHelper
	fields["invisible"] = opts.Invisible

	if opts.Chunks != nil {
		fields["chunks"] = opts.Chunks
	}

	s, err := structpb.NewStruct(fields)
	if err != nil {
		// Shouldn't happen with map[string]any inputs we control.
		return nil
	}
	return s
}

// clampU8 clamps any integer into 0..255. Helper for the byte
// encoding inside BuildMetadata.
func clampU8(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
