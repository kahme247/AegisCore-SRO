package navmeshv2

import (
	"fmt"

	"github.com/pkg/errors"
)

var ErrCellNotInObject = errors.New("cell not in object")
var ErrNoObjectCellForPosition = errors.New("no cell found for position")
var ErrEmptyCollisionList = errors.New("collision list is empty")
var ErrCellCast = errors.New("cell is not of that type")
var ErrInvalidNavmeshData = errors.New("invalid navmesh data")
var ErrInvalidNavmeshHeader = errors.New("invalid navmesh header")
var ErrObjectMeshNotFound = errors.New("navmesh object mesh not found")
var ErrUnsupportedObjectMesh = errors.New("unsupported navmesh object mesh")
var ErrNavmeshNotImplemented = errors.New("navmesh operation is not implemented")

type NavmeshLoadError struct {
	Operation string
	Path      string
	Err       error
}

func (e *NavmeshLoadError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("navmesh %s: %v", e.Operation, e.Err)
	}
	return fmt.Sprintf("navmesh %s %q: %v", e.Operation, e.Path, e.Err)
}

func (e *NavmeshLoadError) Unwrap() error {
	return e.Err
}

func recoverNavmeshLoad(operation, path string, err *error) {
	if recovered := recover(); recovered != nil {
		cause, ok := recovered.(error)
		if !ok {
			cause = fmt.Errorf("%v", recovered)
		}
		*err = &NavmeshLoadError{
			Operation: operation,
			Path:      path,
			Err:       fmt.Errorf("%w: %v", ErrInvalidNavmeshData, cause),
		}
	}
}
