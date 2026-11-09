---
title: Progress reporting
description: Follow the tiling phases from Go code with a progress callback.
sidebar:
  order: 6
---

Progress callbacks receive phase-aware events. Callbacks should return quickly: dispatch expensive UI or logging work elsewhere.

```go
opts := tiler.NewTilerOptions(
	tiler.WithProgressCallback(func(e tiler.ProgressEvent) {
		if e.Level == tiler.ProgressMilestone {
			log.Printf("[%s] %s", e.Phase.Name, e.Message)
		}
	}),
)
```

The current pipeline reports preparation, tree phases, and exporting.
