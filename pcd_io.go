package visuals

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// ParsePCDBinary splits a PCDBinary blob into (header, body, stride,
// totalPoints). Used by chunked-delivery: callers split body on
// stride boundaries to emit individual chunks.
//
// Expects the binary PCD format pointcloud.ToPCD produces (VERSION
// .7, FIELDS x y z rgb, SIZE 4 4 4 4, TYPE F F F I, COUNT 1 1 1 1,
// WIDTH N, HEIGHT 1, VIEWPOINT 0 0 0 1 0 0 0, POINTS N, DATA binary,
// then N records of (float x, float y, float z, int32 rgb)).
//
// Stride is computed from SIZE/COUNT — bytes per point (16 for the
// FFFI layout).
func ParsePCDBinary(pcd []byte) ([]byte, []byte, int, int, error) {
	marker := []byte("DATA binary\n")
	idx := bytes.Index(pcd, marker)
	if idx < 0 {
		return nil, nil, 0, 0, fmt.Errorf("PCD: missing 'DATA binary' marker")
	}
	headerEnd := idx + len(marker)
	header := pcd[:headerEnd]
	body := pcd[headerEnd:]

	headerText := string(header)
	var sizeLine, countLine string
	for _, line := range strings.Split(headerText, "\n") {
		if strings.HasPrefix(line, "SIZE ") {
			sizeLine = line
		}
		if strings.HasPrefix(line, "COUNT ") {
			countLine = line
		}
	}
	if sizeLine == "" || countLine == "" {
		return nil, nil, 0, 0, fmt.Errorf("PCD: missing SIZE or COUNT")
	}
	sizes, err := parseIntFields(strings.TrimPrefix(sizeLine, "SIZE "))
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("PCD: bad SIZE: %w", err)
	}
	counts, err := parseIntFields(strings.TrimPrefix(countLine, "COUNT "))
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("PCD: bad COUNT: %w", err)
	}
	if len(sizes) != len(counts) {
		return nil, nil, 0, 0, fmt.Errorf("PCD: SIZE/COUNT length mismatch (%d vs %d)",
			len(sizes), len(counts))
	}
	stride := 0
	for i := range sizes {
		stride += sizes[i] * counts[i]
	}
	if stride <= 0 {
		return nil, nil, 0, 0, fmt.Errorf("PCD: invalid stride %d", stride)
	}
	total := len(body) / stride
	return header, body, stride, total, nil
}

// BuildPCDChunk builds a self-contained PCDBinary blob containing
// only the chunk at chunkIndex. Rewrites the WIDTH and POINTS
// fields in the header so the result is a valid standalone PCD the
// viewer can render in isolation.
//
// Under chunked delivery the initial Transform's pointcloud bytes
// carry the first chunk (a working PCD all by itself). The viewer
// requests subsequent chunks via the get_entity_chunk DoCommand and
// stitches them in.
func BuildPCDChunk(header, body []byte, stride, chunkIndex, chunkSizePoints int) ([]byte, error) {
	totalPoints := len(body) / stride
	start := chunkIndex * chunkSizePoints
	if start >= totalPoints {
		return nil, fmt.Errorf("chunk_index %d out of range; total_points=%d chunk_size=%d",
			chunkIndex, totalPoints, chunkSizePoints)
	}
	end := start + chunkSizePoints
	if end > totalPoints {
		end = totalPoints
	}
	n := end - start
	bodySlice := body[start*stride : end*stride]

	headerText := string(header)
	var newLines []string
	for _, line := range strings.Split(headerText, "\n") {
		switch {
		case strings.HasPrefix(line, "WIDTH "):
			newLines = append(newLines, fmt.Sprintf("WIDTH %d", n))
		case strings.HasPrefix(line, "POINTS "):
			newLines = append(newLines, fmt.Sprintf("POINTS %d", n))
		default:
			newLines = append(newLines, line)
		}
	}
	newHeader := []byte(strings.Join(newLines, "\n"))
	out := make([]byte, 0, len(newHeader)+len(bodySlice))
	out = append(out, newHeader...)
	out = append(out, bodySlice...)
	return out, nil
}

// parseIntFields splits a whitespace-separated string into ints.
// Internal helper for the PCD header parser.
func parseIntFields(s string) ([]int, error) {
	var out []int
	for _, f := range strings.Fields(s) {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}
