---
title: Options
description: Configure the tiler with option functions, place ungeoreferenced input, choose the output encoder and select attributes.
sidebar:
  order: 3
---

Create options with `tiler.NewDefaultTilerOptions()`, or with `tiler.NewTilerOptions(...)` and option functions.

| Option                          | Default                   | Description |
| ------------------------------- | ------------------------- | ----------- |
| `WithWorkerNumber(n)`           | `runtime.NumCPU()`        | Number of worker goroutines used by loading, tree building, and writing |
| `WithPointsPerTile(n)`          | `50000`                   | Target maximum points per output tile |
| `WithEncoder(id)`               | `plugin.EncoderGLB`       | Selects the registered output encoder; the encoder controls tileset version and content filename |
| `WithEightBitColors(true)`      | `false`                   | Treat LAS/LAZ RGB values as 8-bit instead of 16-bit |
| `WithMutators(m)`               | none                      | Applies point transformations or filtering before tree insertion, see [Mutators](/gotiler/library/mutators/) |
| `WithRefineMode(r)`             | `model.RefineAdd`         | Sets tileset refine mode: `ADD` or `REPLACE` |
| `WithGECorrection(c)`           | `1.0`                     | Multiplies geometric error values written to `tileset.json` |
| `WithMaxTilesPerTileset(n)`     | `2000`                    | Most tiles a tileset JSON file holds; larger tilesets move subtrees into external tilesets in the data folder, see [Large tilesets](/gotiler/guides/output/#large-tilesets); `0` writes a single `tileset.json` |
| `WithInitialGeometricError(ge)` | `0`                       | Sets a minimum root geometric error target; `0` uses the automatic value |
| `WithAttributes(attrs)`         | intensity, classification | Selects optional per-point attributes to export |
| `WithProgressCallback(cb)`      | none                      | Receives progress events while tiling, see [Progress reporting](/gotiler/library/progress/) |
| `WithWriterProvider(wp)`        | disk writer               | Allows injecting a custom writer |
| `WithWriterMiddleware(mw...)`   | none                      | Wraps writer around other writers for custom behaviors |
| `WithWriterFinalizer(f...)`     | none                      | Runs hooks after all content and `tileset.json` are written |
| `WithTreeProvider(provider)`    | internal KD tree          | Allows injecting the tree logic |
| `WithPlacement(p)`              | none                      | Georeferences ungeoreferenced input, see [Ungeoreferenced input](#ungeoreferenced-input) |

## Ungeoreferenced input

Input files without a CRS normally fail. `WithPlacement` accepts them instead: the point coordinates are treated as a local Z-up cartesian system in meters and placed on the WGS84 ellipsoid at the given position and orientation. The zero value places the model origin on the ellipsoid surface at longitude 0, latitude 0, height 0, axes aligned east-north-up, scale 1:

```go
opts := tiler.NewTilerOptions(
	tiler.WithPlacement(tiler.Placement{
		Longitude: 12.492,      // degrees, EPSG:4326
		Latitude:  41.890,      // degrees, EPSG:4326
		Height:    76,          // meters above the WGS84 ellipsoid
		Heading:   45,          // degrees from local north, positive eastward
		Pitch:     0,           // degrees from the east-north plane, positive up
		Roll:      0,           // degrees about the local east axis
		Scale:     1,           // uniform scale (0 means 1)
		UpAxis:    tiler.AxisZ, // input up axis: Z (default), Y, or X
	}),
)
```

Heading, pitch and roll follow the CesiumJS convention. Internally the tiler builds the local-to-EPSG:4978 matrix from these parameters and applies it in place of the CRS conversion, so every downstream stage (tiling, precision recentering, the tileset root transform) works exactly as for georeferenced data.

- A placement and a source CRS are mutually exclusive: placement applies to ungeoreferenced input only.
- Units are assumed to be meters (after the optional uniform scale).
- The tiler's own converter performs no CRS conversions in placement mode: it only applies the placement transform. Mutators that bring their own converter, like an orthophoto colorizer, keep working: they receive a genuine local-to-EPSG:4978 transform, so their coordinates are real ECEF positions, provided the placement puts the cloud at its true geographic location.

## Output encoder

```go
import "github.com/mfbonfigli/gotiler/v3/tiler/plugin"

opts := tiler.NewTilerOptions(
	tiler.WithEncoder(plugin.EncoderGLB),
)
```

Available encoders are:

| Encoder ID                                                    | Output                                                                           | Tileset version |
| ------------------------------------------------------------- | -------------------------------------------------------------------------------- | --------------- |
| `plugin.EncoderPNTS` / `"pnts"`                               | `.pnts`                                                                          | `1.0`           |
| `plugin.EncoderGLB` / `"glb"`                                 | `.glb`                                                                           | `1.1`           |
| `compression.EncoderCompressedGLB` / `"glb-compressed"`       | `.glb` with `KHR_mesh_quantization` + `EXT_meshopt_compression`                  | `1.1`           |
| `compression.EncoderCompressedGLBKHR` / `"glb-compressed-khr"` | `.glb` with `KHR_mesh_quantization` + `KHR_meshopt_compression` (version 1 codec) | `1.1`           |

The compressed encoders are registered by the `plugins/compression` package. The KHR variant produces smaller tiles but needs a loader supporting `KHR_meshopt_compression`, such as CesiumJS 1.143 or later; `compression.New(attrs, compression.WithKHRMeshopt())` builds it directly. To write legacy `.pnts` content:

```go
opts := tiler.NewTilerOptions(
	tiler.WithEncoder(plugin.EncoderPNTS),
)
```

## Attributes

Any per-point attribute exposed by the input files can be requested by name, matched case-insensitively and ignoring whitespace:

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

Constants exist for the standard names: `model.AttrIntensity`, `model.AttrClassification`, `model.AttrReturnNumber`, `model.AttrNumberOfReturns`. The LAS/LAZ reader additionally exposes the other standard point record fields (`gps_time`, `scan_angle`, `point_source_id`, `user_data`, the classification flag bits, `nir`, …) and every extra-byte attribute declared in the file, including common vendor spellings (e.g. requesting `incidence_angle` matches OPALS `_IncidenceAngle` or GeoCue `True View Incidence Angle`). Extra bytes declared with a scale and/or offset are exported as `float64` physical values (`raw*scale+offset`).

- Attributes requested but not found in the source (or missing from some points) are skipped silently.
- Attribute data types not representable by the selected encoder are omitted from that output: 64-bit integers fit neither format, and the GLB encoder stores `float64` values (e.g. `gps_time`) as lossy `float32` and drops 32-bit integers. The PNTS encoder preserves `float64` exactly.
- Attribute names appear uppercased in the output tiles (e.g. `INTENSITY`, `GPS_TIME`).

Use an empty attribute set to omit optional attributes:

```go
opts := tiler.NewTilerOptions(
	tiler.WithAttributes(model.NewAttributes()),
)
```

### Attribute ranges in tileset.json

For every exported attribute, the dataset-global minimum and maximum values are written to `tileset.json`, so clients can build shaders and color gradients without scanning the tiles:

- **3D Tiles 1.1** uses the core metadata mechanism: a tileset `schema` plus a `metadata` entity with `MIN_`/`MAX_`-prefixed properties (e.g. `MIN_INTENSITY`, `MAX_INTENSITY`), exposed by CesiumJS as `tileset.metadata`.
- **3D Tiles 1.0** uses the top-level `properties` dictionary keyed by the per-point property name (e.g. `"INTENSITY": {"minimum": 12, "maximum": 833}`), exposed by CesiumJS as `tileset.properties`.

The ranges reflect the values after read-time mutators and are emitted even for attributes whose per-point data the tile format cannot carry (e.g. 64-bit integers), where the metadata is the only surviving trace of the attribute. Note that 3D Tiles 1.1 stores per-point `float64` values as lossy `float32` while the metadata keeps full precision: clients should clamp when normalizing. [Attributes](/gotiler/guides/attributes/#attribute-ranges) shows the JSON both versions write.
