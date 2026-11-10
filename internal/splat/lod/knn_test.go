package lod

import (
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
)

// bruteNearest is the reference for kdTree.nearest.
func bruteNearest(splats []splat.Splat, query geom.Vec3, self int32, k int, maxDistanceSquared float64) []neighbor {
	var all []neighbor
	for i, s := range splats {
		if d := s.Center.Sub(query).LengthSquared(); int32(i) != self && d <= maxDistanceSquared {
			all = append(all, neighbor{distanceSquared: d, index: int32(i)})
		}
	}
	search := kdSearch{self: self}
	slices.SortFunc(all, func(a, b neighbor) int {
		if search.closer(a, b) {
			return -1
		}
		return 1
	})
	return all[:min(k, len(all))]
}

func TestNearestMatchesBruteForce(t *testing.T) {
	scenes := map[string][]splat.Splat{
		"random": randomSplats(1, 1500, 0),
		// A grid has many neighbors at equal distances, which the index
		// order breaks.
		"grid": gridSplats(30),
		"duplicates": func() []splat.Splat {
			s := randomSplats(2, 300, 0)
			return append(s, s...)
		}(),
		"line": func() []splat.Splat {
			s := randomSplats(3, 500, 0)
			for i := range s {
				s[i].Center.Y, s[i].Center.Z = 0, 0
			}
			return s
		}(),
	}
	rng := rand.New(rand.NewPCG(4, 5))
	for name, splats := range scenes {
		t.Run(name, func(t *testing.T) {
			var tree kdTree
			tree.build(splats)
			var found []neighbor
			for _, k := range []int{1, 3, 8} {
				for _, limit := range []float64{math.Inf(1), 1, 0.25} {
					for i := range splats {
						want := bruteNearest(splats, splats[i].Center, int32(i), k, limit*limit)
						found = tree.nearest(splats[i].Center, int32(i), k, limit*limit, found)
						if !reflect.DeepEqual(append([]neighbor{}, found...), append([]neighbor{}, want...)) {
							t.Fatalf("k %d, limit %v, splat %d: found %v, want %v", k, limit, i, found, want)
						}
					}
					// Queries away from any splat find the same as a brute-force search.
					for range 50 {
						query := geom.Vec3{X: rng.Float64()*12 - 1, Y: rng.Float64()*12 - 1, Z: rng.Float64()*3 - 1}
						want := bruteNearest(splats, query, -1, k, limit*limit)
						found = tree.nearest(query, -1, k, limit*limit, found)
						if !reflect.DeepEqual(append([]neighbor{}, found...), append([]neighbor{}, want...)) {
							t.Fatalf("k %d, limit %v, query %+v: found %v, want %v", k, limit, query, found, want)
						}
					}
				}
			}
		})
	}
}

func TestNearestInTinyTrees(t *testing.T) {
	var tree kdTree
	tree.build(nil)
	if found := tree.nearest(geom.Vec3{}, -1, 8, math.Inf(1), nil); len(found) != 0 {
		t.Errorf("an empty tree found %v", found)
	}
	tree.build([]splat.Splat{roundSplat(0, geom.Vec3{X: 1})})
	if found := tree.nearest(geom.Vec3{X: 1}, 0, 8, math.Inf(1), nil); len(found) != 0 {
		t.Errorf("a tree of one splat found %v besides it", found)
	}
	want := []neighbor{{distanceSquared: 1, index: 0}}
	if found := tree.nearest(geom.Vec3{}, -1, 8, math.Inf(1), nil); !reflect.DeepEqual(found, want) {
		t.Errorf("found %v, want %v", found, want)
	}
}

func TestSelectNth(t *testing.T) {
	rng := rand.New(rand.NewPCG(6, 7))
	for _, n := range []int{1, 2, 3, 10, 101} {
		for trial := range 20 {
			points := make([]kdPoint, n)
			for i := range points {
				// Few distinct values, so that many points tie on the axis.
				points[i] = kdPoint{center: geom.Vec3{Y: float64(rng.IntN(5))}, index: int32(i)}
			}
			if trial%2 == 1 {
				slices.SortFunc(points, func(a, b kdPoint) int { return int(b.index - a.index) })
			}
			sorted := slices.Clone(points)
			slices.SortFunc(sorted, func(a, b kdPoint) int {
				if a.center.Y != b.center.Y {
					return int(a.center.Y - b.center.Y)
				}
				return int(a.index - b.index)
			})
			nth := rng.IntN(n)
			selectNth(points, nth, 1)
			if points[nth] != sorted[nth] {
				t.Fatalf("n %d: point %d is %+v, want %+v", n, nth, points[nth], sorted[nth])
			}
			for i, p := range points {
				before := p.center.Y < points[nth].center.Y || p.center.Y == points[nth].center.Y && p.index < points[nth].index
				if before != (i < nth) && i != nth {
					t.Fatalf("n %d: point %d, %+v, is on the wrong side of %+v", n, i, p, points[nth])
				}
			}
		}
	}
}
