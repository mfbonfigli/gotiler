# Test fixtures

Small GeoTIFF/TIFF fixtures used by the self-contained test suite.

## Provenance

- Most `.tif` files (plus `.tfw`/`.aux.xml` sidecars) are vendored from the
  GDAL autotest suite (`autotest/gcore/data` and `autotest/gcore/data/gtiff`),
  MIT licensed, https://github.com/OSGeo/gdal.
- The remaining files were generated with `gdal_translate` from GDAL autotest
  sources (`byte.tif`, `rgbsmall.tif`, `stefan_full_rgba.tif`, `float32.tif`),
  covering PackBits, BigTIFF, NBITS=1/2/4, Int8, Float16, NaN nodata,
  RGBA contig/separate, 16-bit RGB, predictor-2 separate planes, single-strip
  layout and AREA_OR_POINT=Point.

## expectations.json

Reference values (raster shape, geotransform, EPSG code and sample pixel
values) captured from GDAL (`gdalinfo`/`gdallocationinfo`). Tests compare the
Go reader against this file only, so regular test runs do not need GDAL.

To regenerate after adding fixtures (requires GDAL tools on PATH):

    cd geotiff-colorizer/testdata && go run gen_expectations.go
