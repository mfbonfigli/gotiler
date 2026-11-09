---
title: Plugins
description: The optional packages that add E57 and PLY input, compression, color ramps, orthophoto colorization, subsampling, 3TZ archives and S3 uploads.
sidebar:
  order: 5
---

The packages under `plugins/` extend the engine through the registries in `tiler/plugin` and the mutator and writer interfaces. Readers, encoders and color ramps register themselves when the package is imported, so import them for side effects when you don't reference them otherwise:

```go
import (
	_ "github.com/mfbonfigli/gotiler/v3/plugins/compression" // "glb-compressed" encoder
	_ "github.com/mfbonfigli/gotiler/v3/plugins/e57"         // .e57 reader
	_ "github.com/mfbonfigli/gotiler/v3/plugins/ply"         // .ply reader
	_ "github.com/mfbonfigli/gotiler/v3/plugins/ramps"       // extended color ramps
)
```

| Package                     | Purpose |
| --------------------------- | ------- |
| `plugins/compression`       | Registers the compressed GLB encoder (`KHR_mesh_quantization` + `EXT_meshopt_compression`), with generic per-point attribute support |
| `plugins/e57`               | Registers the `.e57` point-cloud reader. Exposes the standard E57 fields (intensity, timestamp, normals, invalid flags, row/column indices, …) and any extension field declared in the scan prototypes as requestable per-point attributes |
| `plugins/ply`               | Registers the `.ply` point-cloud reader for ASCII and binary files. Tiles the vertex element, reading colors from `red`/`green`/`blue` (or `diffuse_*`) and exposing every other vertex property, with its PLY type, as a requestable per-point attribute. `ply.NewReader` opens a file directly |
| `plugins/ramps`             | Registers an extended library of 27 colorizer gradients, listed in [Color ramps](/gotiler/reference/color-ramps/) |
| `plugins/geotiff-colorizer` | A mutator that colors points from an RGB/RGBA GeoTIFF orthophoto, reprojecting on the fly; includes a pure-Go GeoTIFF reader |
| `plugins/subsampler`        | A mutator that randomly subsamples the cloud |
| `plugins/threetz`           | A `.3tz` (OGC 3D Tiles Archive) writer provider and finalizer |
| `plugins/s3upload`          | An S3-backed `plugin.WriterProvider` |

## Mutators and writers

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

## 3TZ archives

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
