package visuals

import (
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

// RendererMeshContentType — the viewer only renders PLY meshes. STL
// is converted to PLY on the way in via STLToPLY.
const RendererMeshContentType = "ply"

// PointMarkerRadiusMM — radius used for the "point" primitive. The
// Geometry oneof has no Point variant; a radius=0 sphere is
// invisible in the viewer. A small visible radius gives the user
// something to see while reading as a "marker" rather than a sphere.
const PointMarkerRadiusMM = 8.0

// SupportedMeshContentTypes — the renderer is strict here; uppercase
// variants are rejected. See LESSONS.md::mesh-formats.
var SupportedMeshContentTypes = []string{"ply", "stl"}

// ExtractPLYVertexColors parses an ASCII PLY and returns per-vertex
// (R, G, B) tuples if the file carries property uchar
// red/green/blue. Returns nil if the PLY doesn't have vertex colors
// or can't be parsed.
//
// The Viam 3D scene viewer reads Transform.metadata.colors for
// per-vertex coloring, NOT PLY's own embedded vertex colors. This
// is the read half of the transcoding step; BuildMetadata's
// VertexColors field is the write half.
//
// Binary PLY is out of scope — only ASCII PLY.
func ExtractPLYVertexColors(ply []byte) [][3]int {
	text := string(ply)
	if !strings.HasPrefix(text, "ply\n") {
		return nil
	}
	headerEnd := strings.Index(text, "end_header")
	if headerEnd < 0 {
		return nil
	}
	header := text[:headerEnd]
	if !strings.Contains(header, "format ascii") {
		return nil
	}

	lines := strings.Split(text, "\n")
	vertexCount := 0
	var vertexProps []string
	parsingVertex := false
	headerEndLine := -1
	for i, line := range lines {
		if line == "end_header" {
			headerEndLine = i + 1
			break
		}
		if strings.HasPrefix(line, "element vertex ") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "element vertex ")))
			if err != nil {
				return nil
			}
			vertexCount = n
			parsingVertex = true
		} else if strings.HasPrefix(line, "element ") {
			parsingVertex = false
		} else if parsingVertex && strings.HasPrefix(line, "property ") {
			fields := strings.Fields(line)
			vertexProps = append(vertexProps, fields[len(fields)-1])
		}
	}
	if headerEndLine < 0 || vertexCount == 0 {
		return nil
	}
	rIdx, gIdx, bIdx := -1, -1, -1
	for i, p := range vertexProps {
		switch p {
		case "red":
			rIdx = i
		case "green":
			gIdx = i
		case "blue":
			bIdx = i
		}
	}
	if rIdx < 0 || gIdx < 0 || bIdx < 0 {
		return nil
	}
	colors := make([][3]int, 0, vertexCount)
	for i := 0; i < vertexCount; i++ {
		li := headerEndLine + i
		if li >= len(lines) {
			return nil
		}
		parts := strings.Fields(lines[li])
		if len(parts) < len(vertexProps) {
			return nil
		}
		rv, e1 := strconv.Atoi(parts[rIdx])
		gv, e2 := strconv.Atoi(parts[gIdx])
		bv, e3 := strconv.Atoi(parts[bIdx])
		if e1 != nil || e2 != nil || e3 != nil {
			return nil
		}
		colors = append(colors, [3]int{rv, gv, bv})
	}
	if len(colors) == 0 {
		return nil
	}
	return colors
}

// PLYASCIIBytes builds an ASCII PLY buffer from vertices (in mm)
// and faces. Output coordinates are divided by 1000 so the file is
// in meters (the RDK PLY reader's convention).
//
// If vertexColors is non-nil (same length as verts), per-vertex
// `property uchar red/green/blue` are emitted alongside position.
func PLYASCIIBytes(verts [][3]float64, faces [][]int, vertexColors [][3]int) []byte {
	hasColors := vertexColors != nil
	var b strings.Builder
	b.WriteString("ply\n")
	b.WriteString("format ascii 1.0\n")
	fmt.Fprintf(&b, "element vertex %d\n", len(verts))
	b.WriteString("property float x\n")
	b.WriteString("property float y\n")
	b.WriteString("property float z\n")
	if hasColors {
		b.WriteString("property uchar red\n")
		b.WriteString("property uchar green\n")
		b.WriteString("property uchar blue\n")
	}
	fmt.Fprintf(&b, "element face %d\n", len(faces))
	b.WriteString("property list uchar int vertex_indices\n")
	b.WriteString("end_header\n")
	for i, v := range verts {
		if hasColors {
			c := vertexColors[i]
			fmt.Fprintf(&b, "%.6f %.6f %.6f %d %d %d\n",
				v[0]/1000.0, v[1]/1000.0, v[2]/1000.0,
				clampU8(c[0]), clampU8(c[1]), clampU8(c[2]))
		} else {
			fmt.Fprintf(&b, "%.6f %.6f %.6f\n", v[0]/1000.0, v[1]/1000.0, v[2]/1000.0)
		}
	}
	for _, f := range faces {
		fmt.Fprintf(&b, "%d", len(f))
		for _, idx := range f {
			fmt.Fprintf(&b, " %d", idx)
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// ArrowPLYBytes generates a procedural arrow mesh along local +Z,
// returned as ASCII PLY bytes. tipRadius defaults to 2× the shaft
// radius and tipLength defaults to 28% of total length —
// proportions chosen so the arrow head reads clearly without
// overwhelming the shaft.
func ArrowPLYBytes(lengthMM, shaftRadiusMM float64) []byte {
	tipRadiusMM := 2.0 * shaftRadiusMM
	tipLengthMM := math.Max(0.05*lengthMM, 0.28*lengthMM)
	shaftLengthMM := math.Max(0, lengthMM-tipLengthMM)
	sides := 12

	verts := [][3]float64{{0, 0, 0}}
	for i := 0; i < sides; i++ {
		t := 2 * math.Pi * float64(i) / float64(sides)
		verts = append(verts, [3]float64{shaftRadiusMM * math.Cos(t), shaftRadiusMM * math.Sin(t), 0})
	}
	for i := 0; i < sides; i++ {
		t := 2 * math.Pi * float64(i) / float64(sides)
		verts = append(verts, [3]float64{shaftRadiusMM * math.Cos(t), shaftRadiusMM * math.Sin(t), shaftLengthMM})
	}
	for i := 0; i < sides; i++ {
		t := 2 * math.Pi * float64(i) / float64(sides)
		verts = append(verts, [3]float64{tipRadiusMM * math.Cos(t), tipRadiusMM * math.Sin(t), shaftLengthMM})
	}
	apexIdx := 1 + 3*sides
	verts = append(verts, [3]float64{0, 0, shaftLengthMM + tipLengthMM})

	botRing := 1
	topRing := 1 + sides
	coneRing := 1 + 2*sides

	var faces [][]int
	for i := 0; i < sides; i++ {
		curr := botRing + i
		next := botRing + (i+1)%sides
		faces = append(faces, []int{0, next, curr})
	}
	for i := 0; i < sides; i++ {
		b := botRing + i
		bn := botRing + (i+1)%sides
		tt := topRing + i
		tn := topRing + (i+1)%sides
		faces = append(faces, []int{b, bn, tt})
		faces = append(faces, []int{bn, tn, tt})
	}
	for i := 0; i < sides; i++ {
		inner := topRing + i
		innerN := topRing + (i+1)%sides
		outer := coneRing + i
		outerN := coneRing + (i+1)%sides
		faces = append(faces, []int{inner, outer, innerN})
		faces = append(faces, []int{innerN, outer, outerN})
	}
	for i := 0; i < sides; i++ {
		b := coneRing + i
		bn := coneRing + (i+1)%sides
		faces = append(faces, []int{b, bn, apexIdx})
	}
	return PLYASCIIBytes(verts, faces, nil)
}

// STLToPLY converts binary STL bytes to ASCII PLY bytes.
//
// The viewer only renders PLY (rdk/spatialmath/mesh.go: "The
// visualizer expects all meshes to be in PLY format"). STL input
// is converted at load time. Output is ASCII PLY with per-triangle
// vertices — fine for small assets; use trimesh offline for large
// STLs you want deduplicated.
func STLToPLY(stl []byte) ([]byte, error) {
	if len(stl) < 84 {
		return nil, fmt.Errorf("STL data too small (need >=84 bytes for header)")
	}
	nTris := binary.LittleEndian.Uint32(stl[80:84])
	expected := 84 + int(nTris)*50
	if len(stl) < expected {
		return nil, fmt.Errorf("STL truncated: expected %d bytes for %d triangles, got %d",
			expected, nTris, len(stl))
	}
	verts := make([][3]float64, 0, nTris*3)
	faces := make([][]int, 0, nTris)
	offset := 84
	for i := uint32(0); i < nTris; i++ {
		offset += 12 // skip per-tri normal
		face := make([]int, 3)
		for v := 0; v < 3; v++ {
			x := math.Float32frombits(binary.LittleEndian.Uint32(stl[offset : offset+4]))
			y := math.Float32frombits(binary.LittleEndian.Uint32(stl[offset+4 : offset+8]))
			z := math.Float32frombits(binary.LittleEndian.Uint32(stl[offset+8 : offset+12]))
			offset += 12
			face[v] = len(verts)
			verts = append(verts, [3]float64{float64(x), float64(y), float64(z)})
		}
		offset += 2 // skip attribute byte count
		faces = append(faces, face)
	}
	// Note: STL coords are already in "STL units" — caller's
	// responsibility to ship STL with meter-scale geometry. We do
	// NOT divide by 1000 here because PLYASCIIBytes does that for
	// its own inputs, and STL inputs are already in the file's
	// native units (which for our shipped assets is meters).
	var b strings.Builder
	b.WriteString("ply\n")
	b.WriteString("format ascii 1.0\n")
	fmt.Fprintf(&b, "element vertex %d\n", len(verts))
	b.WriteString("property float x\n")
	b.WriteString("property float y\n")
	b.WriteString("property float z\n")
	fmt.Fprintf(&b, "element face %d\n", len(faces))
	b.WriteString("property list uchar int vertex_indices\n")
	b.WriteString("end_header\n")
	for _, v := range verts {
		fmt.Fprintf(&b, "%.6f %.6f %.6f\n", v[0], v[1], v[2])
	}
	for _, f := range faces {
		fmt.Fprintf(&b, "3 %d %d %d\n", f[0], f[1], f[2])
	}
	return []byte(b.String()), nil
}

// LoadMeshBytesAsPLY returns PLY bytes regardless of input format.
// Dispatches on the source path's extension.
func LoadMeshBytesAsPLY(asset []byte, sourcePath string) ([]byte, error) {
	fmt2, err := InferMeshContentType(sourcePath)
	if err != nil {
		return nil, err
	}
	if fmt2 == "stl" {
		return STLToPLY(asset)
	}
	return asset, nil
}

// InferMeshContentType maps a file extension to the lowercase
// content_type the renderer expects. Returns an error for
// unsupported extensions.
func InferMeshContentType(p string) (string, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(p), "."))
	for _, ct := range SupportedMeshContentTypes {
		if ext == ct {
			return ext, nil
		}
	}
	return "", fmt.Errorf("mesh content type %q is not supported; only %v are accepted by the viewer",
		ext, SupportedMeshContentTypes)
}
