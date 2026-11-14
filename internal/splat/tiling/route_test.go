package tiling

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/store"
)

// routed returns the splats routed to each leaf, by leaf id.
func routed(t *testing.T, plan *node, shCount int) map[string][]splat.Splat {
	t.Helper()
	out := map[string][]splat.Splat{}
	for _, leaf := range plan.leaves(nil) {
		if leaf.count == 0 {
			if _, err := os.Stat(leaf.path); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("leaf %s got no splats, but has a store: %v", leaf.id, err)
			}
			continue
		}
		splats, err := store.ReadAll(leaf.path, shCount)
		if err != nil {
			t.Fatal(err)
		}
		out[leaf.id] = splats
	}
	return out
}

func mustRoute(t *testing.T, in *input, plan *node, workers int) {
	t.Helper()
	if err := route(context.Background(), in, plan, t.TempDir(), workers, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRouteSendsEachSplatToItsLeaf(t *testing.T) {
	splats := randomSplats(1, 30_000, 1, geom.Vec3{X: 4.5e6, Y: 1e5, Z: 20}, geom.Vec3{X: 60, Y: 30, Z: 8})
	in := mustReadInput(t, splats, 1, keepAll)
	var results []map[string][]splat.Splat
	for _, workers := range []int{1, 8} {
		plan := buildPlan(slices.Clone(in.samples), in.count, 2000)
		mustRoute(t, in, plan, workers)
		leaves := routed(t, plan, in.shCount)
		results = append(results, leaves)

		seen := make([]bool, len(splats))
		for _, leaf := range plan.leaves(nil) {
			got := leaves[leaf.id]
			if len(got) != leaf.count {
				t.Fatalf("%d workers: leaf %s holds %d splats but counted %d", workers, leaf.id, len(got), leaf.count)
			}
			var bounds geom.Bounds
			for _, s := range got {
				if seen[s.SourceIndex] {
					t.Fatalf("%d workers: splat %d was routed twice", workers, s.SourceIndex)
				}
				seen[s.SourceIndex] = true
				want := stored(splats[s.SourceIndex])
				want.Center = want.Center.Sub(in.origin)
				if !reflect.DeepEqual(s, want) {
					t.Fatalf("%d workers: leaf %s holds %+v, want %+v", workers, leaf.id, s, want)
				}
				if plan.leafOf(s.Center) != leaf {
					t.Fatalf("%d workers: splat %d went to leaf %s, not %s", workers, s.SourceIndex, leaf.id, plan.leafOf(s.Center).id)
				}
				bounds.Include(s.Bounds(boundsSigmas))
			}
			if leaf.bounds != bounds {
				t.Errorf("%d workers: leaf %s has bounds %+v, want %+v", workers, leaf.id, leaf.bounds, bounds)
			}
			// One worker keeps the input order.
			if workers == 1 && !slices.IsSortedFunc(got, func(a, b splat.Splat) int { return cmp.Compare(a.SourceIndex, b.SourceIndex) }) {
				t.Errorf("leaf %s does not hold its splats in input order", leaf.id)
			}
		}
		if i := slices.Index(seen, false); i >= 0 {
			t.Fatalf("%d workers: splat %d was not routed", workers, i)
		}
	}
	// Many workers route the same splats as one, in another order.
	for id, splats := range results[1] {
		slices.SortFunc(splats, func(a, b splat.Splat) int { return cmp.Compare(a.SourceIndex, b.SourceIndex) })
		if !reflect.DeepEqual(splats, results[0][id]) {
			t.Errorf("leaf %s holds other splats with 8 workers than with 1", id)
		}
	}
}

func TestRouteSkipsEmptyLeaves(t *testing.T) {
	// The plan spans X from 0 to 10, but the splats lie below 4.
	plan := buildPlan(uniformSamples(1, 4000, geom.Vec3{X: 10, Y: 1, Z: 1}), 4000, 1000)
	splats := randomSplats(2, 500, 0, geom.Vec3{X: 2, Y: 0.5, Z: 0.5}, geom.Vec3{X: 4, Y: 1, Z: 1})
	in := mustReadInput(t, splats, 0, keepAll)
	in.origin = geom.Vec3{} // Keep the splats where the plan expects them.
	mustRoute(t, in, plan, 4)
	var empty, full int
	for _, leaf := range plan.leaves(nil) {
		if leaf.count == 0 {
			empty++
			if !leaf.bounds.IsEmpty() {
				t.Errorf("empty leaf %s has bounds %+v", leaf.id, leaf.bounds)
			}
		} else {
			full++
		}
	}
	if empty < 2 || full < 1 {
		t.Errorf("%d empty and %d full leaves, want some of each", empty, full)
	}
	routed(t, plan, 0) // Checks that empty leaves have no store.
}

func TestRouteErrors(t *testing.T) {
	splats := randomSplats(3, 1000, 0, geom.Vec3{}, geom.Vec3{X: 10, Y: 10, Z: 10})
	in := mustReadInput(t, splats, 0, keepAll)
	plan := buildPlan(slices.Clone(in.samples), in.count, 100)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	missing := *in
	missing.path = filepath.Join(t.TempDir(), "missing.bin")
	for _, test := range []struct {
		name string
		ctx  context.Context
		in   *input
		dir  string
		want string
	}{
		{"canceled", canceled, in, t.TempDir(), "canceled"},
		{"missing input", context.Background(), &missing, t.TempDir(), "missing.bin"},
		{"missing directory", context.Background(), in, filepath.Join(t.TempDir(), "gone"), "gone"},
	} {
		err := route(test.ctx, test.in, plan, test.dir, 4, nil)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error %v, want one mentioning %q", test.name, err, test.want)
		}
	}
}

// TestWriterPoolReopensClosedStores writes to more leaves than the pool keeps
// open, from several goroutines, and checks that every store holds all its
// splats in the order each goroutine wrote them.
func TestWriterPoolReopensClosedStores(t *testing.T) {
	dir := t.TempDir()
	leaves := make([]*node, 7)
	for i := range leaves {
		leaves[i] = &node{id: string(rune('a' + i)), path: filepath.Join(dir, string(rune('a'+i))+".bin")}
	}
	pool := newWriterPool(2, 3)
	const writers, rounds = 4, 50
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for w := range writers {
		wg.Go(func() {
			for round := range rounds {
				for i, leaf := range leaves {
					s := randomSplats(uint64(w*1000+round*10+i), 1, 1, geom.Vec3{}, geom.Vec3{X: 1, Y: 1, Z: 1})[0]
					// The source index records the writer, round and leaf.
					s.SourceIndex = w*1_000_000 + round*100 + i
					if err := pool.write(leaf, []splat.Splat{s}); err != nil {
						errs[w] = err
						return
					}
				}
			}
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	if err := pool.closeAll(); err != nil {
		t.Fatal(err)
	}
	if pool.lru.Len() != 0 || len(pool.open) != 0 {
		t.Error("the pool keeps writers after closing them all")
	}
	for i, leaf := range leaves {
		splats, err := store.ReadAll(leaf.path, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(splats) != writers*rounds {
			t.Fatalf("store %s holds %d splats, want %d", leaf.id, len(splats), writers*rounds)
		}
		next := make([]int, writers)
		for _, s := range splats {
			w, round, l := s.SourceIndex/1_000_000, s.SourceIndex%1_000_000/100, s.SourceIndex%100
			if l != i || round != next[w] {
				t.Fatalf("store %s holds the splat of writer %d, round %d, leaf %d, after round %d", leaf.id, w, round, l, next[w]-1)
			}
			next[w]++
		}
	}
}

func TestWriterPoolErrors(t *testing.T) {
	pool := newWriterPool(1, 0)
	bad := &node{id: "bad", path: filepath.Join(t.TempDir(), "gone", "bad.bin")}
	if err := pool.write(bad, []splat.Splat{{}}); err == nil {
		t.Error("writing to a store in a missing directory succeeded")
	}
	good := &node{id: "good", path: filepath.Join(t.TempDir(), "good.bin")}
	if err := pool.write(good, []splat.Splat{{SHRest: make([]geom.Vec3, 3)}}); err == nil {
		t.Error("writing a splat of another degree succeeded")
	}
	if err := pool.closeAll(); err != nil {
		t.Fatal(err)
	}
}
