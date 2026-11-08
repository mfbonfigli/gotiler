// Package viewer serves a local web page that shows a tileset on a CesiumJS
// globe and lets the user move it by rewriting its root transform.
package viewer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

const defaultTilesetFile = "tileset.json"

var (
	errNotMovable       = errors.New("the tileset cannot be moved")
	errInvalidTransform = errors.New("invalid transform")
)

var identityTransform = [16]float64{
	1, 0, 0, 0,
	0, 1, 0, 0,
	0, 0, 1, 0,
	0, 0, 0, 1,
}

// tilesetFile is a tileset.json opened to be shown and moved on the globe.
// Transforms are 4x4 matrices in column-major order, as in 3D Tiles.
type tilesetFile struct {
	path string

	mu sync.Mutex
	// document and root keep every field of the file and of its root tile
	// as raw JSON, so saving rewrites only the root transform.
	document  map[string]json.RawMessage
	root      map[string]json.RawMessage
	transform [16]float64
	// readOnlyReason explains why the tileset cannot be moved, empty when it
	// can.
	readOnlyReason string
}

// openTileset opens a tileset.json, given its path or the folder holding it.
func openTileset(path string) (*tilesetFile, error) {
	path, err := resolveTilesetPath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t := &tilesetFile{path: path, transform: identityTransform}
	if err := t.parse(data); err != nil {
		return nil, fmt.Errorf("%s is not a valid tileset: %w", path, err)
	}
	return t, nil
}

func resolveTilesetPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return path, nil
	}
	candidate := filepath.Join(path, defaultTilesetFile)
	if _, err := os.Stat(candidate); errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("the folder %s contains no %s", path, defaultTilesetFile)
	} else if err != nil {
		return "", err
	}
	return candidate, nil
}

func (t *tilesetFile) parse(data []byte) error {
	if err := json.Unmarshal(data, &t.document); err != nil {
		return err
	}
	rawRoot, ok := t.document["root"]
	if !ok {
		return errors.New("it has no root tile")
	}
	if err := json.Unmarshal(rawRoot, &t.root); err != nil {
		return fmt.Errorf("invalid root tile: %w", err)
	}
	if rawTransform, ok := t.root["transform"]; ok {
		var values []float64
		if err := json.Unmarshal(rawTransform, &values); err != nil || len(values) != 16 {
			return errors.New("the root transform is not an array of 16 numbers")
		}
		copy(t.transform[:], values)
	}

	var volumes tileVolumes
	if err := json.Unmarshal(rawRoot, &volumes); err != nil {
		return fmt.Errorf("invalid root tile: %w", err)
	}
	switch {
	case volumes.usesRegion():
		t.readOnlyReason = "it uses region bounding volumes, which are geographic and not moved by the root transform"
	case !isInvertibleAffine(t.transform):
		t.readOnlyReason = "its root transform cannot be inverted"
	}
	return nil
}

// rootTransform returns the current root transform, the identity when the
// file declares none.
func (t *tilesetFile) rootTransform() [16]float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.transform
}

// saveRootTransform writes transform as the new root transform. It accepts
// only transforms that move and rotate the tileset relative to its current
// root transform. The first save keeps a copy of the original file as
// tileset.json.bak.
func (t *tilesetFile) saveRootTransform(transform [16]float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.readOnlyReason != "" {
		return fmt.Errorf("%w: %s", errNotMovable, t.readOnlyReason)
	}
	if err := checkRigidChange(t.transform, transform); err != nil {
		return err
	}

	encodedTransform, err := json.Marshal(transform)
	if err != nil {
		return err
	}
	root := maps.Clone(t.root)
	root["transform"] = encodedTransform
	encodedRoot, err := encodeJSON(root)
	if err != nil {
		return err
	}
	document := maps.Clone(t.document)
	document["root"] = encodedRoot
	data, err := encodeJSON(document)
	if err != nil {
		return err
	}

	if err := backupOnce(t.path); err != nil {
		return fmt.Errorf("backing up %s: %w", t.path, err)
	}
	if err := writeFileAtomically(t.path, data); err != nil {
		return fmt.Errorf("writing %s: %w", t.path, err)
	}
	t.root, t.document, t.transform = root, document, transform
	return nil
}

// encodeJSON marshals v compactly without escaping <, > and &, so string
// values copied from the original file keep their spelling.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func backupOnce(path string) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	backup, err := os.OpenFile(path+".bak", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := backup.Write(original); err != nil {
		backup.Close() //nolint:errcheck
		return err
	}
	return backup.Close()
}

func writeFileAtomically(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tileset-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// tileVolumes decodes only the bounding volumes of a tile tree.
type tileVolumes struct {
	BoundingVolume      *boundingVolume `json:"boundingVolume"`
	ViewerRequestVolume *boundingVolume `json:"viewerRequestVolume"`
	Content             *tileContent    `json:"content"`
	Contents            []tileContent   `json:"contents"`
	Children            []tileVolumes   `json:"children"`
}

type tileContent struct {
	BoundingVolume *boundingVolume `json:"boundingVolume"`
}

type boundingVolume struct {
	Region json.RawMessage `json:"region"`
}

func (t tileVolumes) usesRegion() bool {
	volumes := []*boundingVolume{t.BoundingVolume, t.ViewerRequestVolume}
	if t.Content != nil {
		volumes = append(volumes, t.Content.BoundingVolume)
	}
	for _, content := range t.Contents {
		volumes = append(volumes, content.BoundingVolume)
	}
	for _, v := range volumes {
		if v != nil && v.Region != nil {
			return true
		}
	}
	return slices.ContainsFunc(t.Children, tileVolumes.usesRegion)
}

// matrix3 is a row-major 3x3 matrix.
type matrix3 [3][3]float64

// linearPart returns the rotation and scale part of a column-major 4x4
// transform.
func linearPart(m [16]float64) matrix3 {
	var out matrix3
	for row := range 3 {
		for col := range 3 {
			out[row][col] = m[col*4+row]
		}
	}
	return out
}

func (a matrix3) mul(b matrix3) matrix3 {
	var out matrix3
	for row := range 3 {
		for col := range 3 {
			out[row][col] = a[row][0]*b[0][col] + a[row][1]*b[1][col] + a[row][2]*b[2][col]
		}
	}
	return out
}

func (a matrix3) transpose() matrix3 {
	var out matrix3
	for row := range 3 {
		for col := range 3 {
			out[row][col] = a[col][row]
		}
	}
	return out
}

func (a matrix3) determinant() float64 {
	return a[0][0]*(a[1][1]*a[2][2]-a[1][2]*a[2][1]) -
		a[0][1]*(a[1][0]*a[2][2]-a[1][2]*a[2][0]) +
		a[0][2]*(a[1][0]*a[2][1]-a[1][1]*a[2][0])
}

// inverse returns the inverse of a, which must have a non-zero determinant.
func (a matrix3) inverse() matrix3 {
	det := a.determinant()
	return matrix3{
		{(a[1][1]*a[2][2] - a[1][2]*a[2][1]) / det, (a[0][2]*a[2][1] - a[0][1]*a[2][2]) / det, (a[0][1]*a[1][2] - a[0][2]*a[1][1]) / det},
		{(a[1][2]*a[2][0] - a[1][0]*a[2][2]) / det, (a[0][0]*a[2][2] - a[0][2]*a[2][0]) / det, (a[0][2]*a[1][0] - a[0][0]*a[1][2]) / det},
		{(a[1][0]*a[2][1] - a[1][1]*a[2][0]) / det, (a[0][1]*a[2][0] - a[0][0]*a[2][1]) / det, (a[0][0]*a[1][1] - a[0][1]*a[1][0]) / det},
	}
}

func (a matrix3) isRotation() bool {
	const tolerance = 1e-6
	gram := a.transpose().mul(a)
	for row := range 3 {
		for col := range 3 {
			want := 0.0
			if row == col {
				want = 1
			}
			if math.Abs(gram[row][col]-want) > tolerance {
				return false
			}
		}
	}
	return a.determinant() > 0
}

func isAffine(m [16]float64) bool {
	for _, v := range m {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return m[3] == 0 && m[7] == 0 && m[11] == 0 && m[15] == 1
}

func isInvertibleAffine(m [16]float64) bool {
	return isAffine(m) && linearPart(m).determinant() != 0
}

// checkRigidChange verifies that going from the current transform to next
// only moves and rotates the tileset: the linear part of next times the
// inverse of the current one must be a rotation. Comparing the change rather
// than next itself keeps tilesets whose root transform already carries a
// scale editable.
func checkRigidChange(current, next [16]float64) error {
	if !isAffine(next) {
		return fmt.Errorf("%w: it must hold finite numbers and end with the row 0 0 0 1", errInvalidTransform)
	}
	change := linearPart(next).mul(linearPart(current).inverse())
	if !change.isRotation() {
		return fmt.Errorf("%w: it must only move and rotate the tileset, not scale, skew or mirror it", errInvalidTransform)
	}
	return nil
}
