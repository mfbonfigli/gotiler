"""Generates plugins/ramps/data.go from authoritative colormap sources:
- matplotlib (installed, sampled directly): terrain, gist_earth, coolwarm, seismic, cubehelix
- seaborn cm.py (downloaded source): mako, rocket
- cmocean rgb tables (downloaded): haline, amp, balance, topo
- Scientific Colour Maps via cmcrameri (downloaded): batlow, roma, berlin, nuuk, oleron
- ColorBrewer (downloaded axismaps export): YlGnBu, Blues, RdBu, BrBG, Spectral, PiYG,
  Dark2, Paired, Set2, Accent
"""
import ast
import json
import os
import re

import numpy as np
import matplotlib

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "data.go")

def to255(rows):
    return [tuple(int(round(float(v) * 255)) for v in r[:3]) for r in rows]

ramps = {}       # goVarName -> list of (r,g,b)
categories = {}  # goVarName -> list of (r,g,b)

# --- matplotlib, sampled at 256 points from the canonical implementation ---
x = np.linspace(0, 1, 256)
for name, var in [("terrain", "terrain"), ("gist_earth", "gistEarth"),
                  ("coolwarm", "coolwarm"), ("seismic", "seismic"),
                  ("cubehelix", "cubehelix")]:
    cmap = matplotlib.colormaps[name]
    ramps[var] = to255(cmap(x))

# --- seaborn mako / rocket from the published LUTs in cm.py ---
src = open("seaborn-cm.py", encoding="utf-8").read()
for lut, var in [("_mako_lut", "mako"), ("_rocket_lut", "rocket")]:
    m = re.search(re.escape(lut) + r"\s*=\s*(\[.*?\n\])", src, re.S)
    rows = ast.literal_eval(m.group(1))
    assert len(rows) == 256, (lut, len(rows))
    ramps[var] = to255(rows)

# --- cmocean rgb tables ---
for name, var in [("haline", "haline"), ("amp", "amp"),
                  ("balance", "balance"), ("topo", "topo")]:
    rows = [line.split() for line in open(f"cmocean-{name}.txt") if line.strip()]
    assert len(rows) == 256, (name, len(rows))
    ramps[var] = to255(rows)

# --- Scientific Colour Maps (Fabio Crameri) via cmcrameri data files ---
for name, var in [("batlow", "batlow"), ("roma", "roma"), ("berlin", "berlin"),
                  ("nuuk", "nuuk"), ("oleron", "oleron")]:
    rows = [line.split() for line in open(f"crameri-{name}.txt") if line.strip()]
    assert len(rows) == 256, (name, len(rows))
    ramps[var] = to255(rows)

# --- ColorBrewer ---
brewer = json.load(open("colorbrewer.json"))

def brewer_colors(name, classes):
    out = []
    for s in brewer[name][str(classes)]:
        m = re.match(r"rgb\((\d+),\s*(\d+),\s*(\d+)\)", s)
        out.append((int(m.group(1)), int(m.group(2)), int(m.group(3))))
    return out

for name, classes, var in [("YlGnBu", 9, "ylGnBu"), ("Blues", 9, "blues"),
                           ("RdBu", 11, "rdBu"), ("BrBG", 11, "brBG"),
                           ("Spectral", 11, "spectral"), ("PiYG", 11, "piYG")]:
    ramps[var] = brewer_colors(name, classes)

for name, classes, var in [("Dark2", 8, "dark2"), ("Paired", 12, "paired"),
                           ("Set2", 8, "set2"), ("Accent", 8, "accent")]:
    categories[var] = brewer_colors(name, classes)

# --- emit Go ---
def emit_array(f, var, colors):
    f.write(f"\nvar {var}Colors = []mutator.Color{{\n")
    for i, (r, g, b) in enumerate(colors):
        if i % 6 == 0:
            f.write("\t")
        f.write(f"{{R: {r}, G: {g}, B: {b}}}, ")
        if i % 6 == 5:
            f.write("\n")
    if len(colors) % 6 != 0:
        f.write("\n")
    f.write("}\n")

with open(OUT, "w", newline="\n") as f:
    f.write("""package ramps

// Code generated from authoritative colormap sources. DO NOT EDIT by hand.
// Sources and licenses (see THIRD-PARTY-LICENSES.md):
//   - matplotlib (terrain, gist_earth, coolwarm, seismic, cubehelix): matplotlib license
//   - seaborn (mako, rocket): BSD-3-Clause
//   - cmocean (haline, amp, balance, topo): MIT
//   - Scientific Colour Maps by Fabio Crameri (batlow, roma, berlin, nuuk, oleron): MIT
//   - ColorBrewer (YlGnBu, Blues, RdBu, BrBG, Spectral, PiYG, Dark2, Paired, Set2, Accent):
//     Apache-style ColorBrewer license

import "github.com/mfbonfigli/gotiler/v3/tiler/mutator"
""")
    for var in sorted(ramps):
        emit_array(f, var, ramps[var])
    for var in sorted(categories):
        emit_array(f, var, categories[var])

# spot-check values for the Go test
for var in sorted(list(ramps) + list(categories)):
    colors = ramps.get(var) or categories[var]
    print(f"{var}: n={len(colors)} first={colors[0]} last={colors[-1]}")
print("written", OUT)
