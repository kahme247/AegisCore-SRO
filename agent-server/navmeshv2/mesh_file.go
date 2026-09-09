package navmeshv2

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"

	"github.com/Harvi8/AegisCore/framework/pk2"
	"github.com/Harvi8/AegisCore/framework/utils"
	"github.com/g3n/engine/math32"
	"github.com/sirupsen/logrus"
)

type MeshFile struct {
	FileName              string
	OffsetVertex          uint32
	OffsetSkin            uint32
	OffsetFace            uint32
	OffsetClothVertex     uint32
	OffsetClothEdge       uint32
	OffsetBoundingBox     uint32
	OffsetOcclusionPortal uint32
	OffsetNavMeshObj      uint32
	OffsetSkinnedNavMesh  uint32
	Offset9               uint32
	Int1                  uint32
	StructOption          RtNavmeshStructOption
	FileContent           []byte
	fileReadIndex         int
}

func LoadMeshFile(filename string, reader *pk2.Pk2Reader) (*MeshFile, error) {
	filename = strings.ReplaceAll(filename, "\\", string(os.PathSeparator))
	filename = strings.ReplaceAll(filename, "/", string(os.PathSeparator))
	logrus.Tracef("loading mesh %s", filename)
	if reader == nil {
		return nil, &NavmeshLoadError{Operation: "read object mesh", Path: filename, Err: fmt.Errorf("%w: PK2 reader is nil", ErrInvalidNavmeshData)}
	}
	fileContent, err := reader.ReadFile(filename)
	if err != nil {
		return nil, &NavmeshLoadError{Operation: "read object mesh", Path: filename, Err: err}
	}

	if len(fileContent) < 60 {
		return nil, &NavmeshLoadError{Operation: "read object mesh header", Path: filename, Err: ErrInvalidNavmeshHeader}
	}
	header := string(fileContent[:12])

	if header != "JMXVBMS 0110" {
		return nil, &NavmeshLoadError{Operation: "read object mesh header", Path: filename, Err: ErrInvalidNavmeshHeader}
	}

	meshFile := MeshFile{
		FileName:              filename,
		OffsetVertex:          utils.ByteArrayToUint32(fileContent[12:16]),
		OffsetSkin:            utils.ByteArrayToUint32(fileContent[16:20]),
		OffsetFace:            utils.ByteArrayToUint32(fileContent[20:24]),
		OffsetClothVertex:     utils.ByteArrayToUint32(fileContent[24:28]),
		OffsetClothEdge:       utils.ByteArrayToUint32(fileContent[28:32]),
		OffsetBoundingBox:     utils.ByteArrayToUint32(fileContent[32:36]),
		OffsetOcclusionPortal: utils.ByteArrayToUint32(fileContent[36:40]),
		OffsetNavMeshObj:      utils.ByteArrayToUint32(fileContent[40:44]),
		OffsetSkinnedNavMesh:  utils.ByteArrayToUint32(fileContent[44:48]),
		Offset9:               utils.ByteArrayToUint32(fileContent[48:52]),
		Int1:                  utils.ByteArrayToUint32(fileContent[52:56]),
		StructOption:          RtNavmeshStructOption(utils.ByteArrayToUint32(fileContent[56:60])),
		FileContent:           fileContent,
		fileReadIndex:         60,
	}

	if err := validateMeshNavmeshOffset(filename, meshFile.OffsetNavMeshObj, len(fileContent)); err != nil {
		return nil, err
	}

	return &meshFile, nil
}

func validateMeshNavmeshOffset(filename string, offset uint32, fileSize int) error {
	if offset == 0 {
		return &NavmeshLoadError{Operation: "read object mesh navmesh offset", Path: filename, Err: ErrObjectMeshNotFound}
	}
	if offset < 60 || uint64(offset) > uint64(fileSize) {
		return &NavmeshLoadError{
			Operation: "read object mesh navmesh offset",
			Path:      filename,
			Err:       fmt.Errorf("%w: offset %d is outside file size %d", ErrInvalidNavmeshData, offset, fileSize),
		}
	}
	return nil
}

func (m *MeshFile) LoadMeshObject() (RtNavmeshObj, error) {
	if m == nil {
		return RtNavmeshObj{}, &NavmeshLoadError{Operation: "load object mesh", Err: fmt.Errorf("%w: mesh file is nil", ErrInvalidNavmeshData)}
	}
	logrus.Tracef("loading object mesh: %s", m.FileName)
	m.fileReadIndex = int(m.OffsetNavMeshObj)
	object := NewNavmeshObj(m.FileName)
	vertices, err := m.loadObjectVertices()
	if err != nil {
		return RtNavmeshObj{}, err
	}
	if err := m.loadObjectCells(vertices, &object); err != nil {
		return RtNavmeshObj{}, err
	}
	if err := m.loadObjectGlobalEdges(vertices, &object); err != nil {
		return RtNavmeshObj{}, err
	}
	if err := m.loadObjectInternalEdges(vertices, &object); err != nil {
		return RtNavmeshObj{}, err
	}
	if err := m.loadObjectEvents(&object); err != nil {
		return RtNavmeshObj{}, err
	}
	if err := m.loadObjectGrid(&object); err != nil {
		return RtNavmeshObj{}, err
	}
	if err := validateObjectTopology(&object, m.FileName); err != nil {
		return RtNavmeshObj{}, err
	}

	return object, nil
}

func (m *MeshFile) invalid(section, format string, args ...any) error {
	return &NavmeshLoadError{
		Operation: "read object mesh " + section,
		Path:      m.FileName,
		Err:       fmt.Errorf("%w: %s", ErrInvalidNavmeshData, fmt.Sprintf(format, args...)),
	}
}

func (m *MeshFile) require(needed int, section string) error {
	if needed < 0 || m.fileReadIndex < 0 || m.fileReadIndex > len(m.FileContent) || needed > len(m.FileContent)-m.fileReadIndex {
		remaining := 0
		if m.fileReadIndex >= 0 && m.fileReadIndex <= len(m.FileContent) {
			remaining = len(m.FileContent) - m.fileReadIndex
		}
		return m.invalid(section, "need %d bytes at offset %d, have %d", needed, m.fileReadIndex, remaining)
	}
	return nil
}

func (m *MeshFile) validateCount(count uint64, recordBytes int, section string) error {
	if recordBytes <= 0 {
		return m.invalid(section, "invalid record size %d", recordBytes)
	}
	if m.fileReadIndex < 0 || m.fileReadIndex > len(m.FileContent) {
		return m.invalid(section, "offset %d is outside file size %d", m.fileReadIndex, len(m.FileContent))
	}
	maximum := uint64((len(m.FileContent) - m.fileReadIndex) / recordBytes)
	if count > maximum {
		return m.invalid(section, "record count %d exceeds maximum %d", count, maximum)
	}
	return nil
}

func (m *MeshFile) loadObjectVertices() ([]*math32.Vector3, error) {
	// Vertices
	if err := m.require(4, "vertex count"); err != nil {
		return nil, err
	}
	vertexCount := utils.ByteArrayToUint32(m.FileContent[m.fileReadIndex : m.fileReadIndex+4])
	m.fileReadIndex += 4
	if err := m.validateCount(uint64(vertexCount), 13, "vertices"); err != nil {
		return nil, err
	}
	vertices := make([]*math32.Vector3, vertexCount)
	for i := 0; i < int(vertexCount); i++ {
		vertices[i] = &math32.Vector3{
			X: utils.Float32FromByteArray(m.FileContent[m.fileReadIndex : m.fileReadIndex+4]),
			Y: utils.Float32FromByteArray(m.FileContent[m.fileReadIndex+4 : m.fileReadIndex+8]),
			Z: utils.Float32FromByteArray(m.FileContent[m.fileReadIndex+8 : m.fileReadIndex+12]),
		}
		// 1 byte sin/cos cache
		m.fileReadIndex += 13
	}

	return vertices, nil
}

func (m *MeshFile) loadObjectCells(vertices []*math32.Vector3, object *RtNavmeshObj) error {
	// Cells
	if err := m.require(4, "cell count"); err != nil {
		return err
	}
	cellCount := utils.ByteArrayToUint32(m.FileContent[m.fileReadIndex : m.fileReadIndex+4])
	m.fileReadIndex += 4
	recordBytes := 8
	if m.StructOption.IsCell() {
		recordBytes++
	}
	if err := m.validateCount(uint64(cellCount), recordBytes, "cells"); err != nil {
		return err
	}
	object.Cells = make([]RtNavmeshCellTri, cellCount)
	for i := 0; i < int(cellCount); i++ {
		v1Idx := binary.LittleEndian.Uint16(m.FileContent[m.fileReadIndex : m.fileReadIndex+2])
		v2Idx := binary.LittleEndian.Uint16(m.FileContent[m.fileReadIndex+2 : m.fileReadIndex+4])
		v3Idx := binary.LittleEndian.Uint16(m.FileContent[m.fileReadIndex+4 : m.fileReadIndex+6])
		flag := utils.ByteArrayToUint16(m.FileContent[m.fileReadIndex+6 : m.fileReadIndex+8])
		if int(v1Idx) >= len(vertices) || int(v2Idx) >= len(vertices) || int(v3Idx) >= len(vertices) {
			return m.invalid("cell vertices", "cell %d references vertices %d/%d/%d with %d loaded", i, v1Idx, v2Idx, v3Idx, len(vertices))
		}

		tri := Triangle{
			A: vertices[v1Idx],
			B: vertices[v2Idx],
			C: vertices[v3Idx],
		}
		object.Cells[i] = RtNavmeshCellTri{
			RtNavmeshCellBase: RtNavmeshCellBase{
				Index: i,
				Mesh:  nil,
			},
			Triangle: tri,
			Flag:     int16(flag),
		}
		m.fileReadIndex += 8
		if m.StructOption.IsCell() {
			// Event Zone
			// eventZone := m.FileContent[m.fileReadIndex] // byte
			m.fileReadIndex++
		}
	}
	return nil
}

func (m *MeshFile) loadObjectGlobalEdges(vertices []*math32.Vector3, object *RtNavmeshObj) error {
	// Global Edges
	if err := m.require(4, "global edge count"); err != nil {
		return err
	}
	globalEdgeCount := utils.ByteArrayToUint32(m.FileContent[m.fileReadIndex : m.fileReadIndex+4])
	m.fileReadIndex += 4
	recordBytes := 9
	if m.StructOption.IsEdge() {
		recordBytes++
	}
	if err := m.validateCount(uint64(globalEdgeCount), recordBytes, "global edges"); err != nil {
		return err
	}
	object.GlobalEdges = make([]RtNavmeshEdgeGlobal, globalEdgeCount)
	for i := 0; i < int(globalEdgeCount); i++ {
		v1Idx := utils.ByteArrayToUint16(m.FileContent[m.fileReadIndex : m.fileReadIndex+2])
		v2Idx := utils.ByteArrayToUint16(m.FileContent[m.fileReadIndex+2 : m.fileReadIndex+4])
		srcCellIdx := utils.ByteArrayToUint16(m.FileContent[m.fileReadIndex+4 : m.fileReadIndex+6])
		destCellIdx := utils.ByteArrayToUint16(m.FileContent[m.fileReadIndex+6 : m.fileReadIndex+8])
		flag := m.FileContent[m.fileReadIndex+8] | 1<<3
		m.fileReadIndex += 9
		if int(v1Idx) >= len(vertices) || int(v2Idx) >= len(vertices) {
			return m.invalid("global edge vertices", "edge %d references vertices %d/%d with %d loaded", i, v1Idx, v2Idx, len(vertices))
		}
		if int(srcCellIdx) >= len(object.Cells) || (destCellIdx != ^uint16(0) && int(destCellIdx) >= len(object.Cells)) {
			return m.invalid("global edge cells", "edge %d references cells %d/%d with %d loaded", i, srcCellIdx, destCellIdx, len(object.Cells))
		}

		gEdge := RtNavmeshEdgeGlobal{
			RtNavmeshEdgeBase: RtNavmeshEdgeBase{
				RtNavmeshEdgeMeshType: RtNavmeshEdgeMeshTypeObject,
				Mesh:                  object,
				Index:                 i,
				Line: LineSegment{
					A: vertices[v1Idx],
					B: vertices[v2Idx],
				},
				Flag:         RtNavmeshEdgeFlag(flag),
				SrcDirection: -1,
				DstDirection: -1,
				SrcCellIndex: int(srcCellIdx),
				DstCellIndex: int(destCellIdx),
				SrcCell:      nil,
				DstCell:      nil,
			},
		}

		if m.StructOption.IsEdge() {
			// TODO
			// eventZoneFlag := m.FileContent[m.fileReadIndex+10]
			// gEdge.EventZoneFlag = eventZoneFlag
			m.fileReadIndex++
		}

		object.GlobalEdges[i] = gEdge
	}
	return nil
}

func (m *MeshFile) loadObjectInternalEdges(vertices []*math32.Vector3, object *RtNavmeshObj) error {
	// Internal Edges
	if err := m.require(4, "internal edge count"); err != nil {
		return err
	}
	internalEdgeCount := utils.ByteArrayToUint32(m.FileContent[m.fileReadIndex : m.fileReadIndex+4])
	m.fileReadIndex += 4
	recordBytes := 9
	if m.StructOption.IsEdge() {
		recordBytes++
	}
	if err := m.validateCount(uint64(internalEdgeCount), recordBytes, "internal edges"); err != nil {
		return err
	}
	object.InternalEdges = make([]RtNavmeshEdgeInternal, internalEdgeCount)
	for i := 0; i < int(internalEdgeCount); i++ {
		v1Idx := utils.ByteArrayToUint16(m.FileContent[m.fileReadIndex : m.fileReadIndex+2])
		v2Idx := utils.ByteArrayToUint16(m.FileContent[m.fileReadIndex+2 : m.fileReadIndex+4])
		srcCellIdx := utils.ByteArrayToUint16(m.FileContent[m.fileReadIndex+4 : m.fileReadIndex+6])
		destCellIdx := utils.ByteArrayToUint16(m.FileContent[m.fileReadIndex+6 : m.fileReadIndex+8])
		flag := m.FileContent[m.fileReadIndex+8] | 1<<2
		m.fileReadIndex += 9
		if int(v1Idx) >= len(vertices) || int(v2Idx) >= len(vertices) {
			return m.invalid("internal edge vertices", "edge %d references vertices %d/%d with %d loaded", i, v1Idx, v2Idx, len(vertices))
		}
		if int(srcCellIdx) >= len(object.Cells) || int(destCellIdx) >= len(object.Cells) {
			return m.invalid("internal edge cells", "edge %d references cells %d/%d with %d loaded", i, srcCellIdx, destCellIdx, len(object.Cells))
		}

		iEdge := RtNavmeshEdgeInternal{
			RtNavmeshEdgeBase: RtNavmeshEdgeBase{
				RtNavmeshEdgeMeshType: RtNavmeshEdgeMeshTypeObject,
				Mesh:                  object,
				Index:                 i,
				Line: LineSegment{
					A: vertices[v1Idx],
					B: vertices[v2Idx],
				},
				Flag:         RtNavmeshEdgeFlag(flag),
				SrcDirection: -1,
				DstDirection: -1,
				SrcCellIndex: int(srcCellIdx),
				DstCellIndex: int(destCellIdx),
				SrcCell:      nil,
				DstCell:      nil,
			},
		}

		if m.StructOption.IsEdge() {
			// TODO
			eventZoneFlag := m.FileContent[m.fileReadIndex]
			iEdge.EventData = eventZoneFlag
			m.fileReadIndex++
		}

		object.InternalEdges[i] = iEdge
	}
	return nil
}

func (m *MeshFile) loadObjectEvents(object *RtNavmeshObj) error {
	// Events
	if m.StructOption.IsEvent() {
		if err := m.require(4, "event count"); err != nil {
			return err
		}
		eventCount := utils.ByteArrayToUint32(m.FileContent[m.fileReadIndex : m.fileReadIndex+4])
		m.fileReadIndex += 4
		if err := m.validateCount(uint64(eventCount), 4, "events"); err != nil {
			return err
		}
		object.Events = make([]string, eventCount)
		for i := 0; i < int(eventCount); i++ {
			if err := m.require(4, "event length"); err != nil {
				return err
			}
			strLen := utils.ByteArrayToUint32(m.FileContent[m.fileReadIndex : m.fileReadIndex+4])
			if uint64(strLen) > uint64(len(m.FileContent)-m.fileReadIndex-4) {
				return m.invalid("event data", "event %d length %d exceeds remaining %d", i, strLen, len(m.FileContent)-m.fileReadIndex-4)
			}
			object.Events[i] = string(m.FileContent[m.fileReadIndex+4 : m.fileReadIndex+4+int(strLen)])

			m.fileReadIndex += 4 + int(strLen)
		}
	}
	return nil
}

func (m *MeshFile) loadObjectGrid(object *RtNavmeshObj) error {
	// Grid
	// Grid origin
	if err := m.require(20, "grid header"); err != nil {
		return err
	}
	originX := utils.Float32FromByteArray(m.FileContent[m.fileReadIndex : m.fileReadIndex+4])
	originZ := utils.Float32FromByteArray(m.FileContent[m.fileReadIndex+4 : m.fileReadIndex+8])
	// tile width
	width := utils.ByteArrayToUint32(m.FileContent[m.fileReadIndex+8 : m.fileReadIndex+12])
	height := utils.ByteArrayToUint32(m.FileContent[m.fileReadIndex+12 : m.fileReadIndex+16])
	gridRect := NewRectangle(originX, originZ, float32(width)*RtNavmeshObjGridTileWidth, float32(height)*RtNavmeshObjGridTileHeight)
	gridTileCount := utils.ByteArrayToUint32(m.FileContent[m.fileReadIndex+16 : m.fileReadIndex+20])
	if width == 0 || height == 0 {
		if gridTileCount != 0 {
			return m.invalid("grid dimensions", "zero dimensions %dx%d with %d tiles", width, height, gridTileCount)
		}
	} else {
		expectedTiles := uint64(width) * uint64(height)
		if uint64(gridTileCount) != expectedTiles {
			return m.invalid("grid dimensions", "tile count %d does not match dimensions %dx%d", gridTileCount, width, height)
		}
	}
	grid := RtNavmeshObjGrid{
		object:    object,
		X:         originX,
		Y:         originZ,
		Width:     int(width),
		Height:    int(height),
		Rectangle: gridRect,
	}
	m.fileReadIndex += 20
	if err := m.validateCount(uint64(gridTileCount), 4, "grid tiles"); err != nil {
		return err
	}

	gridTiles := make([]RtNavmeshObjGridTile, gridTileCount)
	for i := 0; i < int(gridTileCount); i++ {
		if err := m.require(4, "grid tile edge count"); err != nil {
			return err
		}
		gEdgeCount := utils.ByteArrayToUint32(m.FileContent[m.fileReadIndex : m.fileReadIndex+4])
		m.fileReadIndex += 4
		if err := m.validateCount(uint64(gEdgeCount), 2, "grid tile edges"); err != nil {
			return err
		}
		gridTile := RtNavmeshObjGridTile{
			Index:         i,
			X:             i % int(width),
			Y:             i / int(width),
			grid:          grid,
			GlobalEdges:   make([]RtNavmeshEdgeGlobal, gEdgeCount),
			InternalEdges: make([]RtNavmeshEdgeInternal, 0),
			Cells:         make([]RtNavmeshCellTri, 0),
		}
		gridTile.Rectangle = NewRectangle(grid.X+(float32(gridTile.X)*RtNavmeshObjGridTileWidth), grid.Y+(float32(gridTile.Y)*RtNavmeshObjGridTileHeight), RtNavmeshObjGridTileWidth, RtNavmeshObjGridTileHeight)
		for j := 0; j < int(gEdgeCount); j++ {
			gEdgeIdx := utils.ByteArrayToUint16(m.FileContent[m.fileReadIndex : m.fileReadIndex+2])
			if int(gEdgeIdx) >= len(object.GlobalEdges) {
				return m.invalid("grid tile edge", "tile %d references global edge %d with %d loaded", i, gEdgeIdx, len(object.GlobalEdges))
			}
			gridTile.GlobalEdges[j] = object.GlobalEdges[gEdgeIdx]
			m.fileReadIndex += 2
		}

		gridTiles[i] = gridTile
	}
	grid.Tiles = gridTiles

	for i := range gridTiles {
		tile := &gridTiles[i]

		for _, cell := range object.Cells {
			if tile.Rectangle.IntersectsTriangle(cell.Triangle) {
				tile.AddCell(cell)
			}
		}

		for _, edge := range object.InternalEdges {
			if tile.Rectangle.IntersectsLine(edge.Line) {
				tile.AddInternalEdge(edge)
			}
		}
	}
	grid.Tiles = gridTiles

	object.Grid = grid
	return nil
}
