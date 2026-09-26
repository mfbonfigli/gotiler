# GoTiler as a Go Library

The **high quality**, **extra fast**, **out-of-core** point-cloud tiling engine behind the `gotiler`
CLI is a set of importable Go packages of the `github.com/mfbonfigli/gotiler/v3` module. It reads
point clouds and converts them into OGC 3D Tiles 1.0 or 1.1.

- ⚡ **Extremely fast**, **out of core**, tiling algorithm. Tile XXL point clouds in minutes with minimal memory usage.
- 🌟 **High quality output**, with approximately uniform and predictable tile sizes.
- 🌐 **Embedded PROJ-based coordinate reprojection** for state of the art coordinate conversion.
- 🚀 Supports the newest **OGC 3D Tiles 1.1** format (.glb), optionally compressed, in addition to the legacy 3D Tiles 1.0 (.pnts).
- 🎛️ **Customizable**: choose between Refine mode Add or Replace, define the expected tile size, choose the attributes you want to export and more.
- 🔍 Extract **CRS metadata directly from LAS/LAZ VLRs**, or pass in a EPSG code, Proj4 or WKT string.
- 🧩 **Pluggable**: readers, encoders, mutators and writers are registries and interfaces, with ready-made plugins for E57 input, meshopt compression, subsampling, orthophoto colorization, 3TZ archives and S3 uploads.

The library is released under the GNU AGPLv3 like the rest of the repository; see the [README](README.md#-commercial-licensing) for commercial licensing options. The engine requires
CGO and PROJ: see [DEVELOPMENT.md](DEVELOPMENT.md) for the build setup.

## 🍯 Public API

The public API is intentionally small and centered on the `tiler` package:

| Package | Purpose |
|---------|---------|
| `tiler` | Main tiling API, options, progress callbacks |
| `tiler/model` | Point, transform, attributes, refine mode |
| `tiler/mutator` | Built-in point mutators and mutator interface |
| `tiler/pointcloud` | Public point-cloud reader interface |
| `tiler/plugin` | Registries and hooks |
| `tiler/tree` | Public tree interfaces |
| `tiler/encoding` | Shared helpers for tile encoders: attribute column resolution, output naming, per-format type mappings |
| `tiler/coord`, `tiler/coord/proj` | Coordinate converter interface and its PROJ implementation |
| `version` | Tileset version constants and parsing |

Optional features live in the `plugins` packages, see [Plugins](#plugins).

## 🛠️ Usage

Import the module from Go code:

```go
import "github.com/mfbonfigli/gotiler/v3/tiler"
```

## Basic Usage

### Tile One File

```go
package main

import (
	"context"
	"log"

	"github.com/mfbonfigli/gotiler/v3/tiler"
)

func main() {
	t, err := tiler.NewGoTiler()
	if err != nil {
		log.Fatal(err)
	}

	opts := tiler.NewDefaultTilerOptions()
	err = t.ProcessFiles(
		[]string{`C:\data\scan.las`},
		`C:\data\tiles`,
		"", // empty means autodetect CRS when the input format supports it
		opts,
		context.Background(),
	)
	if err != nil {
		log.Fatal(err)
	}
}
```

### Tile Several Files As One Tileset

```go
opts := tiler.NewTilerOptions(
	tiler.WithPointsPerTile(75_000),
)

err := t.ProcessFiles(
	[]string{
		`C:\data\part-1.laz`,
		`C:\data\part-2.laz`,
	},
	`C:\data\merged-tiles`,
	"EPSG:32633",
	opts,
	context.Background(),
)
```

### Tile Every File In A Folder Separately

```go
err := t.ProcessFolder(
	`C:\data\point-clouds`,
	`C:\data\tiles`,
	"EPSG:32633",
	tiler.NewDefaultTilerOptions(),
	context.Background(),
)
```

`ProcessFolder` writes one tileset per input file, using the input file name as
the output subfolder name.

## Input And CRS Notes

Built-in input readers are `.las` and `.laz`; importing the `plugins/e57` package registers the
`.e57` reader too. LAS and LAZ inputs can autodetect CRS from supported GeoTIFF or WKT metadata
when `sourceCRS` is empty.

All points are converted to EPSG:4978 internally and stored in a local Z-up
coordinate frame before writing 3D Tiles.

## Options

Create options with `tiler.NewDefaultTilerOptions()` or use
`tiler.NewTilerOptions(...)` with option functions.

| Option | Default | Description |
|--------|---------|-------------|
| `WithWorkerNumber(n)` | `runtime.NumCPU()` | Number of worker goroutines used by loading, tree building, and writing |
| `WithPointsPerTile(n)` | `50000` | Target maximum points per output tile |
| `WithEncoder(id)` | `plugin.EncoderGLB` | Selects the registered output encoder; the encoder controls tileset version and content filename |
| `WithEightBitColors(true)` | `false` | Treat LAS/LAZ RGB values as 8-bit instead of 16-bit |
| `WithMutators(m)` | none | Applies point transformations or filtering before tree insertion |
| `WithRefineMode(r)` | `model.RefineAdd` | Sets tileset refine mode: `ADD` or `REPLACE` |
| `WithGECorrection(c)` | `1.0` | Multiplies geometric error values written to `tileset.json` |
| `WithInitialGeometricError(ge)` | `0` | Sets a minimum root geometric error target; `0` uses the automatic value |
| `WithAttributes(attrs)` | intensity, classification | Selects optional per-point attributes to export |
| `WithProgressCallback(cb)` | none | Receives progress events while tiling |
| `WithWriterProvider(wp)` | disk writer | Allows injecting a custom writer |
| `WithWriterMiddleware(mw...)` | none | Wraps writer around other writers for custom behaviors |
| `WithWriterFinalizer(f...)` | none | Runs hooks after all content and `tileset.json` are written |
| `WithTreeProvider(provider)` | internal KD tree | Allows injecting the tree logic |
| `WithPlacement(p)` | none | Georeferences ungeoreferenced input: treats coordinates as a local cartesian system in meters and places them on the globe (see [Ungeoreferenced Input](#ungeoreferenced-input)) |

### Ungeoreferenced Input

Input files without a CRS normally fail. `WithPlacement` accepts them instead:
the point coordinates are treated as a local Z-up cartesian system in meters
and placed on the WGS84 ellipsoid at the given position and orientation. The
zero value places the model origin on the ellipsoid surface at longitude 0,
latitude 0, height 0, axes aligned east-north-up, scale 1:

```go
opts := tiler.NewTilerOptions(
	tiler.WithPlacement(tiler.Placement{
		Longitude: 12.492,  // degrees, EPSG:4326
		Latitude:  41.890,  // degrees, EPSG:4326
		Height:    76,      // meters above the WGS84 ellipsoid
		Heading:   45,      // degrees from local north, positive eastward
		Pitch:     0,       // degrees from the east-north plane, positive up
		Roll:      0,       // degrees about the local east axis
		Scale:     1,       // uniform scale (0 means 1)
		UpAxis:    tiler.AxisZ, // input up axis: Z (default), Y, or X
	}),
)
```

Heading, pitch and roll follow the CesiumJS convention. Internally the tiler
builds the local-to-EPSG:4978 matrix from these parameters and applies it in
place of the CRS conversion, so every downstream stage (tiling, precision
recentering, the tileset root transform) works exactly as for georeferenced
data. Notes:

- A placement and a source CRS are mutually exclusive: placement applies to
  ungeoreferenced input only.
- Units are assumed to be meters (after the optional uniform scale).
- The tiler's own converter performs no CRS conversions in placement mode
  (it only applies the placement transform). Mutators that bring their own
  converter, like an orthophoto colorizer, keep working: they receive a
  genuine local-to-EPSG:4978 transform, so their coordinates are real ECEF
  positions, provided the placement puts the cloud at its true geographic
  location.

### Output Encoder

```go
import "github.com/mfbonfigli/gotiler/v3/tiler/plugin"

opts := tiler.NewTilerOptions(
	tiler.WithEncoder(plugin.EncoderGLB),
)
```

Available encoders are:

| Encoder ID | Output | Tileset version |
|------------|--------|-----------------|
| `plugin.EncoderPNTS` / `"pnts"` | `.pnts` | `1.0` |
| `plugin.EncoderGLB` / `"glb"` | `.glb` | `1.1` |
| `compression.EncoderCompressedGLB` / `"glb-compressed"` | `.glb` with `KHR_mesh_quantization` + `EXT_meshopt_compression` | `1.1` |
| `compression.EncoderCompressedGLBKHR` / `"glb-compressed-khr"` | `.glb` with `KHR_mesh_quantization` + `KHR_meshopt_compression` (version 1 codec) | `1.1` |

The compressed encoders are registered by the `plugins/compression` package. The KHR variant
produces smaller tiles but needs a loader supporting `KHR_meshopt_compression`, such as CesiumJS
1.143 or later; `compression.New(attrs, compression.WithKHRMeshopt())` builds it directly. To
write legacy `.pnts` content:

```go
opts := tiler.NewTilerOptions(
	tiler.WithEncoder(plugin.EncoderPNTS),
)
```

### Attributes

Any per-point attribute exposed by the input files can be requested by name,
matched case-insensitively and ignoring whitespace:

```go
import "github.com/mfbonfigli/gotiler/v3/tiler/model"

opts := tiler.NewTilerOptions(
	tiler.WithAttributes(model.NewAttributes(
		model.AttrIntensity,
		"gps_time",
		"my_custom_field", // e.g. a LAS extra-byte attribute
	)),
)
```

Constants exist for the standard names: `model.AttrIntensity`,
`model.AttrClassification`, `model.AttrReturnNumber`,
`model.AttrNumberOfReturns`. The LAS/LAZ reader additionally exposes the other
standard point record fields (`gps_time`, `scan_angle`, `point_source_id`,
`user_data`, the classification flag bits, `nir`, ...) and every extra-byte
attribute declared in the file, including common vendor spellings (e.g.
requesting `incidence_angle` matches OPALS `_IncidenceAngle` or GeoCue
`True View Incidence Angle`). Extra bytes declared with a scale and/or offset
are exported as `float64` physical values (`raw*scale+offset`).

Notes:

- Attributes requested but not found in the source (or missing from some
  points) are skipped silently.
- Attribute data types not representable by the selected encoder are omitted
  from that output: 64-bit integers fit neither format, and the GLB encoder
  stores `float64` values (e.g. `gps_time`) as lossy `float32` and drops
  32-bit integers. The PNTS encoder preserves `float64` exactly.
- Attribute names appear uppercased in the output tiles (e.g. `INTENSITY`,
  `GPS_TIME`).

Use an empty attribute set to omit optional attributes:

```go
opts := tiler.NewTilerOptions(
	tiler.WithAttributes(model.NewAttributes()),
)
```

### Tileset attribute metadata

For every exported attribute, the dataset-global minimum and maximum values
are written to `tileset.json`, so clients can build shaders and color
gradients without scanning the tiles:

- **3D Tiles 1.1** uses the core metadata mechanism: a tileset `schema` plus a
  `metadata` entity with `MIN_`/`MAX_`-prefixed properties (e.g.
  `MIN_INTENSITY`, `MAX_INTENSITY`), exposed by CesiumJS as
  `tileset.metadata`.
- **3D Tiles 1.0** uses the top-level `properties` dictionary keyed by the
  per-point property name (e.g. `"INTENSITY": {"minimum": 12, "maximum":
  833}`), exposed by CesiumJS as `tileset.properties`.

The ranges reflect the values after read-time mutators and are emitted even
for attributes whose per-point data the tile format cannot carry (e.g. 64-bit
integers), where the metadata is the only surviving trace of the attribute.
Note that 3D Tiles 1.1 stores per-point `float64` values as lossy `float32`
while the metadata keeps full precision: clients should clamp when
normalizing.

## Mutators

Mutators can transform or discard points after coordinate conversion and before
tree insertion.

### Built-In Mutators

```go
import (
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
)

colorizer, err := mutator.NewColorizer(model.AttrIntensity, "grayscale")
if err != nil {
	return err
}

opts := tiler.NewTilerOptions(
	tiler.WithMutators([]mutator.Mutator{
		mutator.NewZOffset(1.5),
		mutator.NewWithheldFilter(),
		colorizer,
	}),
)
```

`NewZOffset` shifts local Z coordinates.
`NewWithheldFilter` discards points whose `withheld` attribute is true.
`NewColorizer` maps a numeric attribute or local point coordinate (`x`, `y`,
or `z`) to point RGB values using a registered gradient alias. Built-in
gradients are the perceptually uniform `viridis`, `magma`, `inferno`,
`plasma`, `cividis` and `turbo` (embedded as their canonical 256-entry lookup
tables, see THIRD-PARTY-LICENSES.md), plus `grayscale`, `heat`, and
`las-classification`. Importing the `plugins/ramps` package registers 27 more.

No scale bounds are needed: by default the gradient is stretched between the
2nd and 98th percentile of the observed values (estimated in constant memory
with the P² algorithm), so outliers and skewed distributions (common for
intensity or amplitude fields) do not wash out the ramp. Rendering is configurable
through options:

```go
colorizer, err := mutator.NewColorizer(model.AttrIntensity, "viridis",
	mutator.WithStretch(5, 95), // percentile stretch; (0, 100) = plain min/max
	mutator.WithReverse(),      // reverse the color order
	mutator.WithSteps(8),       // quantize into 8 discrete bands
	mutator.WithBlend(0.5),     // mix gradient and original color evenly
)
```

Gradients whose stops encode absolute values can opt out of the automatic
scaling by declaring a fixed range in their definition (`FixedRange`,
`RangeMin`, `RangeMax` on `ColorGradientScale`). The built-in
`las-classification` gradient is pinned to `[0, 255]`, so class codes keep
their colors regardless of which classes appear in the data. Custom gradients
registered through `RegisterColorGradient` can do the same to pin any absolute
scale.

### Write-Time Mutators

Mutators normally run while points are loaded, and their changes are baked into
the tree. A mutator that also implements `mutator.WriteMutator` additionally
runs while tiles are written:

```go
MutateChunkOnWrite(chunk mutator.PointChunk, localToGlobal model.Transform) []model.Point
```

At write time a mutator can rely on statistics gathered over the whole dataset
during loading (this is how the `Colorizer` derives its range), and its
changes are not persisted in the tree. Write-time mutation follows a stricter contract:
points must not be added or dropped, and the mutation must be a deterministic
function of the point data because the same point can be processed more than
once. See the `WriteMutator` documentation for details.

### Custom Mutator

Mutators receive the point in local coordinates plus a typed view over the
point's optional attributes, and return the (possibly modified) point and
whether to keep it. Attribute changes made through the view's setters are
applied in place; they flow into output tiles when the same attribute is also
selected through `WithAttributes`:

```go
type ClassificationFilter struct {
	Keep uint8
}

func (f ClassificationFilter) RequiredAttributes() model.Attributes {
	return model.NewAttributes(model.AttrClassification)
}

func (f ClassificationFilter) Mutate(
	pt model.Point,
	attrs model.AttributeView,
	localToGlobal model.Transform,
) (model.Point, bool) {
	if i := attrs.Index(model.AttrClassification); i >= 0 {
		if v, err := attrs.Value(i); err == nil {
			if c, ok := v.(uint8); ok {
				return pt, c == f.Keep
			}
		}
	}
	return pt, true
}
```

Then pass it through `WithMutators`. `RequiredAttributes` controls which
attributes are requested from the reader for mutator input, so that the Mutator can force-requests the attributes 
it needs. However, it still has to handle the case where the attribute doesn't exist in the input dataset. `WithAttributes`
still controls which attributes are exported to tiles.

## Plugins

The packages under `plugins/` extend the engine through the registries in `tiler/plugin` and the
mutator and writer interfaces. Readers, encoders and color ramps register themselves when the
package is imported, so import them for side effects when you don't reference them otherwise:

```go
import (
	_ "github.com/mfbonfigli/gotiler/v3/plugins/compression" // "glb-compressed" encoder
	_ "github.com/mfbonfigli/gotiler/v3/plugins/e57"         // .e57 reader
	_ "github.com/mfbonfigli/gotiler/v3/plugins/ramps"       // extended color ramps
)
```

| Package | Purpose |
|---------|---------|
| `plugins/compression` | Registers the compressed GLB encoder (`KHR_mesh_quantization` + `EXT_meshopt_compression`), with generic per-point attribute support |
| `plugins/e57` | Registers the `.e57` point-cloud reader. Exposes the standard E57 fields (intensity, timestamp, normals, invalid flags, row/column indices, ...) and any extension field declared in the scan prototypes as requestable per-point attributes |
| `plugins/ramps` | Registers an extended library of 27 colorizer gradients: topographic (`terrain`, `gist-earth`, `batlow`, `oleron`, `nuuk`, `topo`), sequential (`cubehelix`, `mako`, `rocket`, `haline`, `amp`, `ylgnbu`, `blues`, `viridis-pastel`), diverging (`rdbu`, `brbg`, `spectral`, `piyg`, `coolwarm`, `seismic`, `balance`, `roma`, `berlin`), and categorical (`dark2`, `paired`, `set2`, `accent`) |
| `plugins/geotiff-colorizer` | A mutator that colors points from an RGB/RGBA GeoTIFF orthophoto, reprojecting on the fly; includes a pure-Go GeoTIFF reader |
| `plugins/subsampler` | A mutator that randomly subsamples the cloud |
| `plugins/threetz` | A `.3tz` (OGC 3D Tiles Archive) writer provider and finalizer |
| `plugins/s3upload` | An S3-backed `plugin.WriterProvider` |

Mutators and writer providers are regular packages used directly:

```go
import (
	coordproj "github.com/mfbonfigli/gotiler/v3/tiler/coord/proj"
	geotiffcolorizer "github.com/mfbonfigli/gotiler/v3/plugins/geotiff-colorizer"
	"github.com/mfbonfigli/gotiler/v3/plugins/compression"
	"github.com/mfbonfigli/gotiler/v3/plugins/s3upload"
	"github.com/mfbonfigli/gotiler/v3/plugins/subsampler"
)

converter, err := coordproj.NewConverter()
if err != nil {
	return err
}
ortho, err := geotiffcolorizer.NewColorizerFromFile(`C:\data\ortho.tif`, converter)
if err != nil {
	return err
}

opts := tiler.NewTilerOptions(
	tiler.WithEncoder(compression.EncoderCompressedGLB),
	tiler.WithMutators([]mutator.Mutator{subsampler.New(0.25), ortho}),
	tiler.WithWriterProvider(s3upload.NewWriterProvider(ctx, client, "bucket", "tiles")),
)
```

To write a single `.3tz` archive:

```go
archive, err := threetz.New(`C:\data\tiles.3tz`)
if err != nil {
	return err
}

opts := tiler.NewTilerOptions(
	tiler.WithWriterProvider(archive.WriterProvider()),
	tiler.WithWriterFinalizer(archive),
)
```

## Progress Reporting

Progress callbacks receive phase-aware events. Callbacks should return quickly:
dispatch expensive UI or logging work elsewhere.

```go
opts := tiler.NewTilerOptions(
	tiler.WithProgressCallback(func(e tiler.ProgressEvent) {
		if e.Level == tiler.ProgressMilestone {
			log.Printf("[%s] %s", e.Phase.Name, e.Message)
		}
	}),
)
```

The current pipeline reports preparation, tree phases, and exporting.

## Tests

```bash
go test ./...
```

Local `go test ./...` requires a working CGO and PROJ setup. If your local
machine is not configured for PROJ, use the Docker-backed build instead, see
[DEVELOPMENT.md](DEVELOPMENT.md).
