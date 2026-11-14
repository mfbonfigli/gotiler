// Package radix sorts items by integer keys, in time linear in their number.
package radix

// insertionLimit is the most items that Sort orders by insertion: for so few,
// counting the bytes of the keys costs more than comparing them.
const insertionLimit = 64

// Item is a value with the key it is sorted by.
type Item struct {
	Key   uint64
	Value uint64
}

// Sort orders items by Key, keeping items with the same key in their order.
// It uses scratch as a buffer, grown if it is shorter than items, and returns
// it for the next call.
//
// It sorts on a byte of the keys at a time, from the least significant, and
// skips the bytes that all the keys share. Counting the bytes takes a single
// pass over the items, which finds those for free.
func Sort(items, scratch []Item) []Item {
	n := len(items)
	if n <= insertionLimit {
		insertionSort(items)
		return scratch
	}
	if cap(scratch) < n {
		scratch = make([]Item, n)
	}
	var counts [8][256]int
	for _, item := range items {
		k := item.Key
		counts[0][byte(k)]++
		counts[1][byte(k>>8)]++
		counts[2][byte(k>>16)]++
		counts[3][byte(k>>24)]++
		counts[4][byte(k>>32)]++
		counts[5][byte(k>>40)]++
		counts[6][byte(k>>48)]++
		counts[7][byte(k>>56)]++
	}
	from, to := items, scratch[:n]
	for digit := range counts {
		shift := 8 * digit
		offsets := &counts[digit]
		if offsets[byte(items[0].Key>>shift)] == n {
			continue
		}
		next := 0
		for b, count := range offsets {
			offsets[b] = next
			next += count
		}
		for _, item := range from {
			b := byte(item.Key >> shift)
			to[offsets[b]] = item
			offsets[b]++
		}
		from, to = to, from
	}
	if &from[0] != &items[0] {
		copy(items, from)
	}
	return scratch
}

func insertionSort(items []Item) {
	for i := 1; i < len(items); i++ {
		item := items[i]
		j := i
		for ; j > 0 && items[j-1].Key > item.Key; j-- {
			items[j] = items[j-1]
		}
		items[j] = item
	}
}
