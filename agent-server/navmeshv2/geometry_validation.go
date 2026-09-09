package navmeshv2

import (
	"math"

	"github.com/g3n/engine/math32"
)

func validLineSegment(line LineSegment) bool {
	return validVector3(line.A) && validVector3(line.B)
}

func validTriangle(triangle Triangle) bool {
	return validVector3(triangle.A) && validVector3(triangle.B) && validVector3(triangle.C)
}

func validRectangle(rectangle Rectangle) bool {
	return validVector2(rectangle.Min) && validVector2(rectangle.Max) &&
		rectangle.Max.X >= rectangle.Min.X && rectangle.Max.Y >= rectangle.Min.Y
}

func validVector2(vector *math32.Vector2) bool {
	return vector != nil && finiteFloat32(vector.X) && finiteFloat32(vector.Y)
}

func validVector3(vector *math32.Vector3) bool {
	return vector != nil && finiteFloat32(vector.X) && finiteFloat32(vector.Y) && finiteFloat32(vector.Z)
}

func finiteFloat32(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}
