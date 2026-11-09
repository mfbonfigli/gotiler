---
title: Mutators
description: Transform, filter or colorize points while tiling, with the built-in mutators or your own.
sidebar:
  order: 4
---

Mutators can transform or discard points after coordinate conversion and before tree insertion.

## Built-in mutators

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

- `NewZOffset` shifts local Z coordinates.
- `NewWithheldFilter` discards points whose `withheld` attribute is true.
- `NewColorizer` maps a numeric attribute or local point coordinate (`x`, `y`, or `z`) to point RGB values using a registered gradient alias. Built-in gradients are the perceptually uniform `viridis`, `magma`, `inferno`, `plasma`, `cividis` and `turbo` (embedded as their canonical 256-entry lookup tables, see [THIRD-PARTY-LICENSES.md](https://github.com/mfbonfigli/gotiler/blob/v3/THIRD-PARTY-LICENSES.md)), plus `grayscale`, `heat`, and `las-classification`. Importing the `plugins/ramps` package registers 27 more: see [Color ramps](/gotiler/reference/color-ramps/).

No scale bounds are needed: by default the gradient is stretched between the 2nd and 98th percentile of the observed values (estimated in constant memory with the P² algorithm), so outliers and skewed distributions (common for intensity or amplitude fields) do not wash out the ramp. Rendering is configurable through options:

```go
colorizer, err := mutator.NewColorizer(model.AttrIntensity, "viridis",
	mutator.WithStretch(5, 95), // percentile stretch; (0, 100) = plain min/max
	mutator.WithReverse(),      // reverse the color order
	mutator.WithSteps(8),       // quantize into 8 discrete bands
	mutator.WithBlend(0.5),     // mix gradient and original color evenly
)
```

Gradients whose stops encode absolute values can opt out of the automatic scaling by declaring a fixed range in their definition (`FixedRange`, `RangeMin`, `RangeMax` on `ColorGradientScale`). The built-in `las-classification` gradient is pinned to `[0, 255]`, so class codes keep their colors regardless of which classes appear in the data. Custom gradients registered through `RegisterColorGradient` can do the same to pin any absolute scale.

## Write-time mutators

Mutators normally run while points are loaded, and their changes are baked into the tree. A mutator that also implements `mutator.WriteMutator` additionally runs while tiles are written:

```go
MutateChunkOnWrite(chunk mutator.PointChunk, localToGlobal model.Transform) []model.Point
```

At write time a mutator can rely on statistics gathered over the whole dataset during loading (this is how the `Colorizer` derives its range), and its changes are not persisted in the tree. Write-time mutation follows a stricter contract: points must not be added or dropped, and the mutation must be a deterministic function of the point data, because the same point can be processed more than once. See the `WriteMutator` documentation for details.

## Custom mutators

Mutators receive the point in local coordinates plus a typed view over the point's optional attributes, and return the (possibly modified) point and whether to keep it. Attribute changes made through the view's setters are applied in place; they flow into output tiles when the same attribute is also selected through `WithAttributes`:

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

Then pass it through `WithMutators`. `RequiredAttributes` controls which attributes are requested from the reader for the mutator's input, so a mutator can force-request the attributes it needs. It still has to handle the case where an attribute doesn't exist in the input dataset. `WithAttributes` still controls which attributes are exported to tiles.
