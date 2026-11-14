package splat

import (
	"math"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
)

// maxSHDegree is the highest spherical-harmonic degree a splat can carry.
const maxSHDegree = 3

// maxSHBandSize is the number of coefficients of the highest degree, 2·3+1.
const maxSHBandSize = 2*maxSHDegree + 1

// shBandMatrix maps the coefficients of one spherical-harmonic degree, using
// only its top-left (2·degree+1)² elements.
type shBandMatrix [maxSHBandSize][maxSHBandSize]float64

// Normalization constants of the real spherical-harmonic basis functions.
var (
	shBasis1   = math.Sqrt(3 / (4 * math.Pi))
	shBasis2xy = math.Sqrt(15 / (4 * math.Pi))
	shBasis2z  = math.Sqrt(5 / (16 * math.Pi))
	shBasis2xx = math.Sqrt(15 / (16 * math.Pi))
	shBasis3a  = math.Sqrt(35 / (32 * math.Pi))
	shBasis3b  = math.Sqrt(105 / (4 * math.Pi))
	shBasis3c  = math.Sqrt(21 / (32 * math.Pi))
	shBasis3d  = math.Sqrt(7 / (16 * math.Pi))
	shBasis3e  = math.Sqrt(105 / (16 * math.Pi))
)

// shBasis evaluates the real spherical-harmonic basis functions of one degree
// at the unit direction d. The order and the signs of the functions are those
// of the reference 3D Gaussian Splatting implementation, which renderers and
// the splat formats follow.
func shBasis(degree int, d geom.Vec3) [maxSHBandSize]float64 {
	x, y, z := d.X, d.Y, d.Z
	xx, yy, zz := x*x, y*y, z*z
	switch degree {
	case 1:
		return [maxSHBandSize]float64{-shBasis1 * y, shBasis1 * z, -shBasis1 * x}
	case 2:
		return [maxSHBandSize]float64{
			shBasis2xy * x * y,
			-shBasis2xy * y * z,
			shBasis2z * (2*zz - xx - yy),
			-shBasis2xy * x * z,
			shBasis2xx * (xx - yy),
		}
	default:
		return [maxSHBandSize]float64{
			-shBasis3a * y * (3*xx - yy),
			shBasis3b * x * y * z,
			-shBasis3c * y * (4*zz - xx - yy),
			shBasis3d * z * (2*zz - 3*xx - 3*yy),
			-shBasis3c * x * (4*zz - xx - yy),
			shBasis3e * z * (xx - yy),
			-shBasis3a * x * (xx - 3*yy),
		}
	}
}

// shSampleDirections are directions spread evenly over the sphere, on a
// Fibonacci spiral.
var shSampleDirections = func() []geom.Vec3 {
	const count = 32
	goldenAngle := math.Pi * (3 - math.Sqrt(5))
	directions := make([]geom.Vec3, count)
	for i := range directions {
		z := 1 - (2*float64(i)+1)/count
		r := math.Sqrt(1 - z*z)
		phi := float64(i) * goldenAngle
		directions[i] = geom.Vec3{X: r * math.Cos(phi), Y: r * math.Sin(phi), Z: z}
	}
	return directions
}()

// shRotationMatrix returns the matrix M that maps the coefficients c of one
// spherical-harmonic degree to the coefficients M·c of the same function
// turned by the rotation matrix r.
//
// The turned function is g(d) = f(rᵀ·d). It belongs to the same degree, so
// with Y and Yr holding the basis functions evaluated at sample directions d
// and at rᵀ·d, M solves Y·M = Yr. It is found by least squares, which is exact
// here: the many well-spread directions only keep the solve well conditioned.
func shRotationMatrix(degree int, r geom.Mat3) shBandMatrix {
	n := 2*degree + 1
	inverse := r.Transpose()
	var normal, rhs shBandMatrix
	for _, d := range shSampleDirections {
		basis := shBasis(degree, d)
		turned := shBasis(degree, inverse.MulVec(d))
		for i := range n {
			for j := range n {
				normal[i][j] += basis[i] * basis[j]
				rhs[i][j] += basis[i] * turned[j]
			}
		}
	}
	return solveLinearSystem(normal, rhs, n)
}

// solveLinearSystem solves a·x = b for the n×n matrix x by Gaussian
// elimination with partial pivoting.
func solveLinearSystem(a, b shBandMatrix, n int) shBandMatrix {
	for col := range n {
		pivot := col
		for row := col + 1; row < n; row++ {
			if math.Abs(a[row][col]) > math.Abs(a[pivot][col]) {
				pivot = row
			}
		}
		a[col], a[pivot] = a[pivot], a[col]
		b[col], b[pivot] = b[pivot], b[col]
		for row := col + 1; row < n; row++ {
			factor := a[row][col] / a[col][col]
			for k := col; k < n; k++ {
				a[row][k] -= factor * a[col][k]
			}
			for k := range n {
				b[row][k] -= factor * b[col][k]
			}
		}
	}
	var x shBandMatrix
	for row := n - 1; row >= 0; row-- {
		for k := range n {
			sum := b[row][k]
			for j := row + 1; j < n; j++ {
				sum -= a[row][j] * x[j][k]
			}
			x[row][k] = sum / a[row][row]
		}
	}
	return x
}
