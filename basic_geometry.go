// BuildBasicGeometry — easy-mode geometry builder for services that
// publish only the standard non-asset primitive types.
//
// A SceneServiceBase subclass has to implement a BuildGeometry hook
// that returns a *commonpb.Geometry for each item. For services
// that use only box / sphere / capsule / point / arrow, this
// dispatcher is the one-line implementation:
//
//	func (s *myService) BuildGeometry(item visuals.Item, _ visuals.BaseGeom) (*commonpb.Geometry, error) {
//	    return visuals.BuildBasicGeometry(item)
//	}
//
// Mesh and pointcloud are excluded because they require I/O (a
// ReadAsset call) and a content-type / PLY-conversion decision the
// helper can't make in isolation. Services that publish those need
// their own BuildGeometry hook.
package visuals

import (
	"fmt"

	commonpb "go.viam.com/api/common/v1"
)

// DefaultBaseGeomForItem extracts the shape-specific base fields
// from an Item for the standard non-asset primitives. The library
// uses this when the service's Hooks doesn't implement
// BaseGeomProvider.
func DefaultBaseGeomForItem(item Item) BaseGeom {
	bg := BaseGeom{}
	switch item.Type {
	case "box":
		if item.HasDims {
			bg.Dims = item.DimsMM
			bg.HasDims = true
		}
	case "sphere":
		bg.RadiusMM = item.RadiusMM
	case "capsule", "arrow":
		bg.RadiusMM = item.RadiusMM
		bg.LengthMM = item.LengthMM
	}
	return bg
}

// BuildBasicGeometry builds the commonpb.Geometry proto for the
// standard non-asset primitive types: box, sphere, capsule, point,
// arrow. Returns an error for any other type.
//
// For animation-driven size overrides, services with animation
// should dispatch on item.Type themselves and consult the BaseGeom
// argument before building.
func BuildBasicGeometry(item Item) (*commonpb.Geometry, error) {
	switch item.Type {
	case "box":
		d := item.DimsMM
		return &commonpb.Geometry{
			Label: item.Label,
			GeometryType: &commonpb.Geometry_Box{
				Box: &commonpb.RectangularPrism{
					DimsMm: &commonpb.Vector3{X: d.X, Y: d.Y, Z: d.Z},
				},
			},
		}, nil
	case "sphere":
		return &commonpb.Geometry{
			Label: item.Label,
			GeometryType: &commonpb.Geometry_Sphere{
				Sphere: &commonpb.Sphere{RadiusMm: item.RadiusMM},
			},
		}, nil
	case "capsule":
		return &commonpb.Geometry{
			Label: item.Label,
			GeometryType: &commonpb.Geometry_Capsule{
				Capsule: &commonpb.Capsule{
					RadiusMm: item.RadiusMM,
					LengthMm: item.LengthMM,
				},
			},
		}, nil
	case "point":
		return &commonpb.Geometry{
			Label: item.Label,
			GeometryType: &commonpb.Geometry_Sphere{
				Sphere: &commonpb.Sphere{RadiusMm: PointMarkerRadiusMM},
			},
		}, nil
	case "arrow":
		ply := ArrowPLYBytes(item.LengthMM, item.RadiusMM)
		return &commonpb.Geometry{
			Label: item.Label,
			GeometryType: &commonpb.Geometry_Mesh{
				Mesh: &commonpb.Mesh{ContentType: "ply", Mesh: ply},
			},
		}, nil
	}
	return nil, fmt.Errorf("BuildBasicGeometry doesn't handle item type %q (use a custom BuildGeometry hook for mesh / pointcloud)", item.Type)
}
