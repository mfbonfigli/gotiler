---
title: Attributes
description: Export per-point attributes such as intensity, classification or LAS extra bytes, and read their value ranges from tileset.json.
sidebar:
  order: 4
---

Per-point attributes are scalar values stored with each point next to its position and color. By default only `intensity` and `classification` are exported. `--attributes` takes a comma-separated list of any attributes the input files expose, matched case-insensitively, or `none`:

```bash
gotiler pcloud -o ./out --attributes intensity,classification,gps_time,my_custom_field ./input.las
gotiler pcloud -o ./out --attributes none ./input.las
```

## LAS and LAZ attributes

Commonly available attributes for LAS/LAZ inputs:

| Attribute name                                                          | Type             | Description                                            | Included by default |
| ----------------------------------------------------------------------- | ---------------- | ------------------------------------------------------ | ------------------- |
| `intensity`                                                             | `uint16`         | Raw laser return intensity (0–65535)                   | Yes                 |
| `classification`                                                        | `uint8`          | LAS point classification code                          | Yes                 |
| `return_number`                                                         | `uint8`          | Return number within the pulse (1-indexed; 0 = unset)  | No                  |
| `number_of_returns`                                                     | `uint8`          | Total number of returns for the pulse                  | No                  |
| `gps_time`                                                              | `float64`        | GPS timestamp of the point                             | No                  |
| `scan_angle`                                                            | `float64`        | Scan angle in degrees                                  | No                  |
| `point_source_id`                                                       | `uint16`         | File source ID the point originated from               | No                  |
| `user_data`                                                             | `uint8`          | User data byte                                         | No                  |
| `classification_flags`, `synthetic`, `key_point`, `withheld`, `overlap` | `uint8` / `bool` | Classification flag bits                               | No                  |
| `scan_direction_flag`, `edge_of_flight_line`, `scanner_channel`, `nir`, … | various        | Other standard LAS point record fields                 | No                  |

Any **extra-byte attribute** declared in a LAS file can also be requested by name; scaled extra bytes are exported as `float64` physical values (`raw*scale+offset`). Common vendor spellings are matched automatically: `incidence_angle` also finds OPALS `_IncidenceAngle` and GeoCue/LP360 `True View Incidence Angle`, and `pulse_width`/`echo_width` find the ASPRS, RIEGL, OPALS (`EchoWidth`) and Terrasolid (`Echo length`) fields. RIEGL, OPALS and Terrasolid `Amplitude` and `Reflectance` match by name.

E57 and PLY inputs expose their own fields: see [Input formats](/gotiler/guides/input-formats/).

## How attributes are stored

- Requested attributes missing from the source, or from some points, are skipped silently.
- Types a tileset version can't store are dropped from that output: neither format stores 64-bit integers, and 3D Tiles 1.1 also drops 32-bit integers and stores `float64` values (e.g. `gps_time`) as `float32`. 3D Tiles 1.0 keeps `float64` exact.
- Attribute names are uppercased in the tiles (`INTENSITY`, `GPS_TIME`).

## Attribute ranges

The dataset-wide minimum and maximum of every exported attribute are written to `tileset.json`, so viewers can normalize values for shaders and color ramps without reading any tile. 3D Tiles 1.1 uses the tileset metadata mechanism, with `MIN_`/`MAX_`-prefixed properties; 3D Tiles 1.0 uses the top-level `properties` dictionary.

**3D Tiles 1.1** writes a metadata `schema` plus a tileset `metadata` entity, with one pair of properties per exported attribute:

```json
{
  "asset": {"version": "1.1"},
  "schema": {
    "id": "gotiler_dataset",
    "classes": {
      "dataset": {
        "properties": {
          "MIN_INTENSITY": {"type": "SCALAR", "componentType": "UINT16"},
          "MAX_INTENSITY": {"type": "SCALAR", "componentType": "UINT16"}
        }
      }
    }
  },
  "metadata": {
    "class": "dataset",
    "properties": {"MIN_INTENSITY": 12, "MAX_INTENSITY": 833}
  }
}
```

**3D Tiles 1.0** writes the top-level `properties` dictionary, keyed by the per-point property name:

```json
{
  "asset": {"version": "1.0"},
  "properties": {
    "INTENSITY": {"minimum": 12, "maximum": 833}
  }
}
```

CesiumJS exposes it as `tileset.properties` and uses it to resolve `${MINIMUM}`-style bounds in declarative styling; in JavaScript, read `tileset.properties.INTENSITY.minimum` and `.maximum`.

- Ranges are written for every attribute selected with `--attributes` and found in the data, including attributes whose per-point values the tile format can't carry (e.g. 64-bit integers): the metadata is then their only trace.
- 3D Tiles 1.1 stores per-point `float64` values as `float32`, while the metadata keeps full `float64` precision: clamp when normalizing, as rounded values can fall just outside the range.
