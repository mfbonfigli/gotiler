---
title: Basic usage
description: Tile one file, several files as one tileset, or every file of a folder from Go code.
sidebar:
  order: 2
---

A `GoTiler` processes either a list of files, merged into one tileset, or a folder, tiling each file into its own tileset.

## Tile one file

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

## Tile several files as one tileset

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

## Tile every file of a folder separately

```go
err := t.ProcessFolder(
	`C:\data\point-clouds`,
	`C:\data\tiles`,
	"EPSG:32633",
	tiler.NewDefaultTilerOptions(),
	context.Background(),
)
```

`ProcessFolder` writes one tileset per input file, using the input file name as the output subfolder name.

## Input and CRS notes

The built-in input readers are `.las` and `.laz`; importing the `plugins/e57` and `plugins/ply` packages registers the `.e57` and `.ply` readers too. LAS and LAZ inputs can autodetect the CRS from supported GeoTIFF or WKT metadata when `sourceCRS` is empty; E57 and PLY files carry no CRS, so they need a `sourceCRS` or a [placement](/gotiler/library/options/#ungeoreferenced-input).

All points are converted to EPSG:4978 internally and stored in a local Z-up coordinate frame before writing 3D Tiles.
