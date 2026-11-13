package radix

import (
	"cmp"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestSort(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))
	for _, test := range []struct {
		name string
		n    int
		key  func(i int) uint64
	}{
		{"empty", 0, func(int) uint64 { return 0 }},
		{"one", 1, func(int) uint64 { return 7 }},
		{"few", insertionLimit, func(int) uint64 { return random.Uint64N(10) }},
		{"random", 10_000, func(int) uint64 { return random.Uint64() }},
		{"repeated keys", 10_000, func(int) uint64 { return random.Uint64N(100) << 40 }},
		{"one key", 1000, func(int) uint64 { return 42 }},
		{"sorted", 1000, func(i int) uint64 { return uint64(i) }},
		{"reversed", 1000, func(i int) uint64 { return uint64(1000 - i) }},
		{"high byte only", 1000, func(i int) uint64 { return uint64(i%3) << 56 }},
	} {
		items := make([]Item, test.n)
		for i := range items {
			items[i] = Item{Key: test.key(i), Value: uint64(i)}
		}
		want := slices.Clone(items)
		slices.SortStableFunc(want, func(a, b Item) int { return cmp.Compare(a.Key, b.Key) })
		Sort(items, nil)
		if !slices.Equal(items, want) {
			t.Errorf("%s: items are not sorted by key in a stable order", test.name)
		}
	}
}

func TestSortReusesScratch(t *testing.T) {
	items := make([]Item, 1000)
	for i := range items {
		items[i] = Item{Key: uint64(len(items) - i)}
	}
	scratch := Sort(items, nil)
	if len(scratch) < len(items) {
		t.Fatalf("scratch holds %d items, want at least %d", len(scratch), len(items))
	}
	again := Sort(items[:500], scratch)
	if &again[0] != &scratch[0] {
		t.Error("a long enough scratch was not reused")
	}
}

func BenchmarkSort(b *testing.B) {
	random := rand.New(rand.NewPCG(1, 2))
	items := make([]Item, 1_000_000)
	for i := range items {
		items[i] = Item{Key: random.Uint64(), Value: uint64(i)}
	}
	work := make([]Item, len(items))
	var scratch []Item
	for b.Loop() {
		copy(work, items)
		scratch = Sort(work, scratch)
	}
}
