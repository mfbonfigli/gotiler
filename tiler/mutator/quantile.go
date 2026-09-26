package mutator

// quantileEstimator estimates a single quantile of a stream of values using
// the P² algorithm (Jain & Chlamtac, 1985). It keeps five markers instead of
// the observed values, so memory use is constant regardless of how many
// values are observed. It is not safe for concurrent use: callers must
// synchronize access.
type quantileEstimator struct {
	q       float64 // target quantile in (0, 1)
	n       int     // number of observed values
	heights [5]float64
	// positions are the actual 1-based marker positions; desired are the
	// ideal positions the markers drift toward.
	positions [5]float64
	desired   [5]float64
	rates     [5]float64
}

func newQuantileEstimator(q float64) *quantileEstimator {
	e := &quantileEstimator{}
	e.init(q)
	return e
}

func (e *quantileEstimator) init(q float64) {
	*e = quantileEstimator{
		q:         q,
		positions: [5]float64{1, 2, 3, 4, 5},
		desired:   [5]float64{1, 1 + 2*q, 1 + 4*q, 3 + 2*q, 5},
		rates:     [5]float64{0, q / 2, q, (1 + q) / 2, 1},
	}
}

// reset discards all observed values, keeping the target quantile.
func (e *quantileEstimator) reset() {
	e.init(e.q)
}

// add observes one value.
func (e *quantileEstimator) add(v float64) {
	if e.n < 5 {
		// initialization phase: insert sorted into the height markers
		i := e.n
		for i > 0 && e.heights[i-1] > v {
			e.heights[i] = e.heights[i-1]
			i--
		}
		e.heights[i] = v
		e.n++
		return
	}

	// find the marker cell the value falls into, extending the extremes
	var k int
	switch {
	case v < e.heights[0]:
		e.heights[0] = v
		k = 0
	case v >= e.heights[4]:
		e.heights[4] = v
		k = 3
	default:
		for k = 0; k < 3; k++ {
			if v < e.heights[k+1] {
				break
			}
		}
	}

	for i := k + 1; i < 5; i++ {
		e.positions[i]++
	}
	for i := 0; i < 5; i++ {
		e.desired[i] += e.rates[i]
	}

	// adjust the three middle markers toward their desired positions
	for i := 1; i <= 3; i++ {
		d := e.desired[i] - e.positions[i]
		if (d >= 1 && e.positions[i+1]-e.positions[i] > 1) || (d <= -1 && e.positions[i-1]-e.positions[i] < -1) {
			if d >= 1 {
				d = 1
			} else {
				d = -1
			}
			h := e.parabolic(i, d)
			if e.heights[i-1] < h && h < e.heights[i+1] {
				e.heights[i] = h
			} else {
				e.heights[i] = e.linear(i, d)
			}
			e.positions[i] += d
		}
	}
	e.n++
}

// parabolic computes the piecewise-parabolic height adjustment for marker i.
func (e *quantileEstimator) parabolic(i int, d float64) float64 {
	return e.heights[i] + d/(e.positions[i+1]-e.positions[i-1])*
		((e.positions[i]-e.positions[i-1]+d)*(e.heights[i+1]-e.heights[i])/(e.positions[i+1]-e.positions[i])+
			(e.positions[i+1]-e.positions[i]-d)*(e.heights[i]-e.heights[i-1])/(e.positions[i]-e.positions[i-1]))
}

// linear computes the fallback linear height adjustment for marker i.
func (e *quantileEstimator) linear(i int, d float64) float64 {
	j := i + int(d)
	return e.heights[i] + d*(e.heights[j]-e.heights[i])/(e.positions[j]-e.positions[i])
}

// estimate returns the current quantile estimate. ok is false until at least
// five values have been observed, since the estimate is meaningless before.
func (e *quantileEstimator) estimate() (float64, bool) {
	if e.n < 5 {
		return 0, false
	}
	return e.heights[2], true
}
