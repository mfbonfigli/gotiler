# GoTiler Plugins

Optional features of the GoTiler engine, built on the registries in `tiler/plugin` and the
mutator and writer interfaces:

| Package | Purpose |
|---------|---------|
| `compression` | Compressed GLB encoder (`KHR_mesh_quantization` + `EXT_meshopt_compression`, or `KHR_meshopt_compression`) |
| `e57` | `.e57` point-cloud reader |
| `geotiff-colorizer` | Colors points from an RGB/RGBA GeoTIFF orthophoto |
| `ramps` | Extended library of colorizer gradients |
| `s3upload` | S3-backed writer provider |
| `subsampler` | Random point subsampling mutator |
| `threetz` | `.3tz` (OGC 3D Tiles Archive) writer provider and finalizer |

See [LIBRARY.md](../LIBRARY.md#plugins) for usage; like the rest of the repository they are
released under the GNU AGPLv3.
