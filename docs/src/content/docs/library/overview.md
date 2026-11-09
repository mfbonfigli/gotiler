---
title: Go library overview
description: The tiling engine behind the gotiler CLI as importable Go packages, what they offer and what they need to build.
sidebar:
  label: Overview
  order: 1
---

The out-of-core point cloud tiling engine behind the `gotiler` CLI is a set of importable Go packages of the `github.com/mfbonfigli/gotiler/v3` module. It reads point clouds and converts them into OGC 3D Tiles 1.0 or 1.1.

The library is released under the GNU AGPLv3 like the rest of the repository; see [License](/gotiler/project/license/) for alternative licensing. The engine requires CGO and PROJ: see [Development](/gotiler/project/development/) for the build setup.

## Public API

The public API is intentionally small and centered on the `tiler` package:

| Package                           | Purpose                                                                                                 |
| --------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `tiler`                           | Main tiling API, options, progress callbacks                                                            |
| `tiler/model`                     | Point, transform, attributes, refine mode                                                               |
| `tiler/mutator`                   | Built-in point mutators and mutator interface                                                           |
| `tiler/pointcloud`                | Public point-cloud reader interface                                                                     |
| `tiler/plugin`                    | Registries and hooks                                                                                    |
| `tiler/tree`                      | Public tree interfaces                                                                                  |
| `tiler/encoding`                  | Shared helpers for tile encoders: attribute column resolution, output naming, per-format type mappings |
| `tiler/coord`, `tiler/coord/proj` | Coordinate converter interface and its PROJ implementation                                              |
| `version`                         | Tileset version constants and parsing                                                                   |

Optional features live in the `plugins` packages, see [Plugins](/gotiler/library/plugins/). The [API reference](https://pkg.go.dev/github.com/mfbonfigli/gotiler/v3) on pkg.go.dev documents every package.

## Importing the module

```go
import "github.com/mfbonfigli/gotiler/v3/tiler"
```

[Basic usage](/gotiler/library/basic-usage/) shows how to tile files and folders.
