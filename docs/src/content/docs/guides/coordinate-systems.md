---
title: Coordinate systems
description: How gotiler finds the coordinate system of the input, how to set it with --crs, and how heights are handled.
sidebar:
  order: 2
---

gotiler transforms every point from the coordinate system of the input to the globe with the embedded PROJ library, so the tileset lands in the right place in CesiumJS and other 3D Tiles viewers.

## Setting the CRS

LAS and LAZ files usually declare their coordinate system in their metadata, which gotiler reads on its own. E57 and PLY files declare none, so `--crs` (or `-c`) is required for them. `--crs` also overrides the CRS of a LAS/LAZ file. It accepts:

- an EPSG code such as `EPSG:4326`, or just `4326`
- a compound code with a vertical datum, such as `EPSG:32633+3855`
- a Proj4 string
- a WKT definition

```bash
gotiler pcloud -o ./out --crs EPSG:32633 ./scan.e57
gotiler pcloud -o ./out -c EPSG:32633+3855 ./input.las
```

For clouds that have no geographic coordinate system at all, such as scans in a scanner frame or models from photogrammetry or CAD, use `--crs local`: see [Placing ungeoreferenced assets](/gotiler/guides/placing-ungeoreferenced-clouds/).

## Heights

3D Tiles viewers place points by their height above the WGS84 ellipsoid. When the heights of the input are relative to a geoid instead, name the vertical datum with a compound code, such as `EPSG:32633+3855` for EGM2008 heights, so PROJ converts them.

Such conversions may need datum grids that are not bundled with gotiler. Download the ones you need from the [PROJ CDN](https://cdn.proj.org/) into the `share` folder next to the executable.

`--z-offset` (or `-z`) adds a fixed vertical offset, in meters, to every point. Use it only when the input elevation is already referred to the WGS84 ellipsoid or geoid, for example to correct a known constant shift.

```bash
gotiler pcloud -o ./out -c EPSG:32633 --z-offset 42.5 ./input.las
```
