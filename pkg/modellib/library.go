package modellib

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"mindpalace/pkg/logging"
)

// Library exposes a lazily materialized catalog of 3D assets sourced from a tarball.
// Models are extracted and normalized into OBJ files Godot can load on demand.
type Library struct {
	tarPath        string
	worldRoot      string
	cacheDir       string
	resourcePrefix string

	mu        sync.Mutex
	cache     map[string]string
	lockTable map[string]*sync.Mutex
}

// NewLibrary builds a Library that can resolve Thingi10K meshes into Godot-friendly OBJ assets.
// The tarball is not unpacked eagerly; individual meshes are decoded the first time they are requested.
func NewLibrary(tarPath, worldRoot string) (*Library, error) {
	if tarPath == "" {
		return nil, errors.New("modellib: tarPath required")
	}
	if worldRoot == "" {
		return nil, errors.New("modellib: worldRoot required")
	}
	cacheDir := filepath.Join(worldRoot, "models", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("modellib: create cache dir: %w", err)
	}
	return &Library{
		tarPath:        tarPath,
		worldRoot:      worldRoot,
		cacheDir:       cacheDir,
		resourcePrefix: "res://models/cache",
		cache:          make(map[string]string),
		lockTable:      make(map[string]*sync.Mutex),
	}, nil
}

// EnsureModel returns a Godot resource path for the requested model identifier, extracting and converting the asset if needed.
func (l *Library) EnsureModel(modelID string) (string, error) {
	if modelID == "" {
		return "", errors.New("modellib: modelID required")
	}

	// Fast path: already cached in memory.
	l.mu.Lock()
	if resource, ok := l.cache[modelID]; ok {
		l.mu.Unlock()
		return resource, nil
	}

	cachePath := filepath.Join(l.cacheDir, fmt.Sprintf("%s.obj", modelID))
	if _, err := os.Stat(cachePath); err == nil {
		resource := l.resourceFor(cachePath)
		l.cache[modelID] = resource
		l.mu.Unlock()
		return resource, nil
	}

	lock := l.lockFor(modelID)
	l.mu.Unlock()

	lock.Lock()
	defer lock.Unlock()

	// Re-check after acquiring the per-model lock.
	if _, err := os.Stat(cachePath); err == nil {
		resource := l.resourceFor(cachePath)
		l.mu.Lock()
		l.cache[modelID] = resource
		l.mu.Unlock()
		return resource, nil
	}

	if err := l.extractAndConvert(modelID, cachePath); err != nil {
		return "", err
	}
	resource := l.resourceFor(cachePath)
	l.mu.Lock()
	l.cache[modelID] = resource
	l.mu.Unlock()
	return resource, nil
}

func (l *Library) lockFor(modelID string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lock, ok := l.lockTable[modelID]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	l.lockTable[modelID] = lock
	return lock
}

func (l *Library) resourceFor(cachePath string) string {
	rel, err := filepath.Rel(l.worldRoot, cachePath)
	if err != nil {
		return filepath.ToSlash(cachePath)
	}
	return "res://" + filepath.ToSlash(rel)
}

func (l *Library) extractAndConvert(modelID, destPath string) error {
	logging.Debug("MODEL: extracting %s to %s", modelID, destPath)

	f, err := os.Open(l.tarPath)
	if err != nil {
		return fmt.Errorf("modellib: open tarball: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("modellib: open gzip reader: %w", err)
	}
	defer gzr.Close()

	targetName := fmt.Sprintf("%s.stl", modelID)
	tr := tar.NewReader(gzr)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("modellib: read tar: %w", err)
		}

		if filepath.Ext(hdr.Name) != ".stl" {
			continue
		}
		if filepath.Base(hdr.Name) != targetName {
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return fmt.Errorf("modellib: read STL payload: %w", err)
		}

		mesh, err := parseSTL(data)
		if err != nil {
			return fmt.Errorf("modellib: parse STL %s: %w", hdr.Name, err)
		}

		if err := writeOBJ(destPath, mesh); err != nil {
			return fmt.Errorf("modellib: write OBJ: %w", err)
		}
		return nil
	}

	return fmt.Errorf("modellib: model %s not found", modelID)
}

type triangle struct {
	Normal   [3]float64
	Vertices [3][3]float64
}

type meshData struct {
	Triangles []triangle
}

func parseSTL(data []byte) (*meshData, error) {
	if len(data) < 84 {
		return nil, errors.New("stl payload too small")
	}

	isASCII := bytes.HasPrefix(data, []byte("solid "))
	if isASCII {
		if mesh, err := parseASCIISTL(data); err == nil {
			return mesh, nil
		}
		// Fall back to binary interpretation if ASCII parsing failed.
	}
	count := binary.LittleEndian.Uint32(data[80:84])
	expected := 84 + int(count)*50
	if expected <= len(data) {
		return parseBinarySTL(data, int(count))
	}
	// Last resort attempt ASCII.
	return parseASCIISTL(data)
}

func parseBinarySTL(data []byte, count int) (*meshData, error) {
	triangles := make([]triangle, 0, count)
	offset := 84
	for i := 0; i < count; i++ {
		if offset+50 > len(data) {
			return nil, errors.New("unexpected EOF parsing binary STL")
		}
		var tri triangle
		for j := 0; j < 3; j++ {
			fbits := binary.LittleEndian.Uint32(data[offset+(j*4) : offset+(j*4)+4])
			tri.Normal[j] = float64(math.Float32frombits(fbits))
		}
		offset += 12
		for v := 0; v < 3; v++ {
			for c := 0; c < 3; c++ {
				fbits := binary.LittleEndian.Uint32(data[offset+(c*4) : offset+(c*4)+4])
				tri.Vertices[v][c] = float64(math.Float32frombits(fbits))
			}
			offset += 12
		}
		offset += 2 // attribute byte count
		triangles = append(triangles, tri)
	}
	return &meshData{Triangles: triangles}, nil
}

func parseASCIISTL(data []byte) (*meshData, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Split(bufio.ScanLines)
	triangles := make([]triangle, 0, 1024)
	var current *triangle
	vertexIdx := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "facet normal") {
			parts := strings.Fields(line)
			if len(parts) < 5 {
				continue
			}
			current = &triangle{}
			for i := 0; i < 3; i++ {
				val, err := strconv.ParseFloat(parts[2+i], 64)
				if err != nil {
					return nil, fmt.Errorf("invalid normal value %q: %w", parts[2+i], err)
				}
				current.Normal[i] = val
			}
			vertexIdx = 0
		} else if strings.HasPrefix(line, "vertex") && current != nil && vertexIdx < 3 {
			parts := strings.Fields(line)
			if len(parts) < 4 {
				continue
			}
			for i := 0; i < 3; i++ {
				val, err := strconv.ParseFloat(parts[1+i], 64)
				if err != nil {
					return nil, fmt.Errorf("invalid vertex value %q: %w", parts[1+i], err)
				}
				current.Vertices[vertexIdx][i] = val
			}
			vertexIdx++
		} else if strings.HasPrefix(line, "endfacet") && current != nil {
			triangles = append(triangles, *current)
			current = nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan ASCII STL: %w", err)
	}
	if len(triangles) == 0 {
		return nil, errors.New("no triangles parsed")
	}
	return &meshData{Triangles: triangles}, nil
}

func writeOBJ(dest string, mesh *meshData) error {
	if len(mesh.Triangles) == 0 {
		return errors.New("empty mesh")
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	normalized := normalizeMesh(mesh)

	tmpFile, err := os.CreateTemp(filepath.Dir(dest), "model-*.obj")
	if err != nil {
		return err
	}
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}()

	writer := bufio.NewWriter(tmpFile)
	for _, tri := range normalized.Triangles {
		for _, vertex := range tri.Vertices {
			if _, err := fmt.Fprintf(writer, "v %.6f %.6f %.6f\n", vertex[0], vertex[1], vertex[2]); err != nil {
				return err
			}
		}
	}
	for _, tri := range normalized.Triangles {
		n := normalizeVector(tri.Normal)
		if _, err := fmt.Fprintf(writer, "vn %.6f %.6f %.6f\n", n[0], n[1], n[2]); err != nil {
			return err
		}
	}
	vertexIndex := 0
	for i := range normalized.Triangles {
		vi1 := vertexIndex + 1
		vi2 := vertexIndex + 2
		vi3 := vertexIndex + 3
		vertexIndex += 3
		ni := i + 1
		if _, err := fmt.Fprintf(writer, "f %d//%d %d//%d %d//%d\n", vi1, ni, vi2, ni, vi3, ni); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpFile.Name(), dest); err != nil {
		return err
	}
	return nil
}

func normalizeMesh(mesh *meshData) *meshData {
	var minX, minY, minZ = math.MaxFloat64, math.MaxFloat64, math.MaxFloat64
	var maxX, maxY, maxZ = -math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64

	for _, tri := range mesh.Triangles {
		for _, v := range tri.Vertices {
			if v[0] < minX {
				minX = v[0]
			}
			if v[0] > maxX {
				maxX = v[0]
			}
			if v[1] < minY {
				minY = v[1]
			}
			if v[1] > maxY {
				maxY = v[1]
			}
			if v[2] < minZ {
				minZ = v[2]
			}
			if v[2] > maxZ {
				maxZ = v[2]
			}
		}
	}

	sizeX := maxX - minX
	sizeY := maxY - minY
	sizeZ := maxZ - minZ
	maxSize := math.Max(sizeX, math.Max(sizeY, sizeZ))
	if maxSize == 0 {
		maxSize = 1
	}

	scale := 1.0 / maxSize
	centerX := (minX + maxX) / 2
	centerY := (minY + maxY) / 2
	centerZ := (minZ + maxZ) / 2

	normalized := make([]triangle, len(mesh.Triangles))
	for i, tri := range mesh.Triangles {
		normalized[i].Normal = tri.Normal
		for v := 0; v < 3; v++ {
			normalized[i].Vertices[v][0] = (tri.Vertices[v][0] - centerX) * scale
			normalized[i].Vertices[v][1] = (tri.Vertices[v][1] - centerY) * scale
			normalized[i].Vertices[v][2] = (tri.Vertices[v][2] - centerZ) * scale
		}
	}
	return &meshData{Triangles: normalized}
}

func normalizeVector(normal [3]float64) [3]float64 {
	length := math.Sqrt(normal[0]*normal[0] + normal[1]*normal[1] + normal[2]*normal[2])
	if length == 0 {
		return [3]float64{0, 1, 0}
	}
	return [3]float64{normal[0] / length, normal[1] / length, normal[2] / length}
}
