package geotiffcolorizer

import (
	"bytes"
	"compress/zlib"
	"container/list"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math"
	mathbits "math/bits"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz/lzma"
	xi2xz "github.com/xi2/xz"
	tifflzw "golang.org/x/image/tiff/lzw"
	"golang.org/x/image/webp"
)

const (
	tiffTagNewSubfileType       = 254
	tiffTagImageWidth           = 256
	tiffTagImageLength          = 257
	tiffTagBitsPerSample        = 258
	tiffTagCompression          = 259
	tiffTagPhotometric          = 262
	tiffTagFillOrder            = 266
	tiffTagStripOffsets         = 273
	tiffTagSamplesPerPixel      = 277
	tiffTagRowsPerStrip         = 278
	tiffTagStripByteCounts      = 279
	tiffTagPlanarConfig         = 284
	tiffTagPredictor            = 317
	tiffTagColorMap             = 320
	tiffTagTileWidth            = 322
	tiffTagTileLength           = 323
	tiffTagTileOffsets          = 324
	tiffTagTileByteCounts       = 325
	tiffTagExtraSamples         = 338
	tiffTagSampleFormat         = 339
	tiffTagJPEGTables           = 347
	tiffTagYCbCrCoefficients    = 529
	tiffTagYCbCrSubSampling     = 530
	tiffTagYCbCrPositioning     = 531
	tiffTagReferenceBlackWhite  = 532
	tiffTagModelPixelScale      = 33550
	tiffTagModelTiepoint        = 33922
	tiffTagModelTransformation  = 34264
	tiffTagGeoKeyDirectory      = 34735
	tiffTagGeoDoubleParams      = 34736
	tiffTagGeoAsciiParams       = 34737
	tiffTagGDALMetadata         = 42112
	tiffTagGDALNodata           = 42113
	tiffTagLERCParameters       = 50674
	tiffTagCloudOptimizedGDALMD = 42112
	tiffTagCloudOptimizedNodata = 42113
)

const (
	tiffTypeByte      = 1
	tiffTypeASCII     = 2
	tiffTypeShort     = 3
	tiffTypeLong      = 4
	tiffTypeRational  = 5
	tiffTypeSByte     = 6
	tiffTypeUndefined = 7
	tiffTypeSShort    = 8
	tiffTypeSLong     = 9
	tiffTypeSRational = 10
	tiffTypeFloat     = 11
	tiffTypeDouble    = 12
	tiffTypeLong8     = 16
	tiffTypeSLong8    = 17
	tiffTypeIFD8      = 18
)

const (
	tiffCompressionNone       = 1
	tiffCompressionLZW        = 5
	tiffCompressionOldJPEG    = 6
	tiffCompressionJPEG       = 7
	tiffCompressionDeflate    = 8
	tiffCompressionPackBits   = 32773
	tiffCompressionOldDeflate = 32946
	tiffCompressionLERC       = 34887
	tiffCompressionLZMA       = 34925
	tiffCompressionZSTD       = 50000
	tiffCompressionWebP       = 50001
)

const (
	tiffPhotometricWhiteIsZero = 0
	tiffPhotometricBlackIsZero = 1
	tiffPhotometricRGB         = 2
	tiffPhotometricPalette     = 3
	tiffPhotometricYCbCr       = 6
)

const (
	tiffPlanarChunky   = 1
	tiffPlanarSeparate = 2
)

const (
	tiffSampleFormatUnsigned = 1
	tiffSampleFormatSigned   = 2
	tiffSampleFormatFloat    = 3
)

// SampleFormat is the TIFF SampleFormat value for one raster band.
type SampleFormat uint16

const (
	SampleFormatUnsigned SampleFormat = tiffSampleFormatUnsigned
	SampleFormatSigned   SampleFormat = tiffSampleFormatSigned
	SampleFormatFloat    SampleFormat = tiffSampleFormatFloat
)

// SamplingMode controls how model coordinates are converted to a raster color.
type SamplingMode int

const (
	// SampleNearest uses the containing pixel for PixelIsArea images and the
	// nearest posting for PixelIsPoint images.
	SampleNearest SamplingMode = iota
	// SampleBilinear interpolates four neighbouring pixels in raster space.
	SampleBilinear
)

// RasterType is the value of GTRasterTypeGeoKey.
type RasterType uint16

const (
	RasterTypeUnknown      RasterType = 0
	RasterPixelIsArea      RasterType = 1
	RasterPixelIsPoint     RasterType = 2
	RasterPixelUserDefined RasterType = 32767
)

// ModelType is the value of GTModelTypeGeoKey.
type ModelType uint16

const (
	ModelTypeUnknown     ModelType = 0
	ModelTypeProjected   ModelType = 1
	ModelTypeGeographic  ModelType = 2
	ModelTypeGeocentric  ModelType = 3
	ModelTypeUserDefined ModelType = 32767
)

// Color is an sRGB color decoded from an orthophoto pixel. A is 255 when the
// source image has no usable alpha/extra sample.
type Color struct {
	R uint8
	G uint8
	B uint8
	A uint8
}

// ReaderOption configures OpenOrthophoto.
type ReaderOption func(*readerOptions)

const defaultBlockCacheMemoryBytes int64 = 0.5 * 1024 * 1024 * 1024

// defaultMaxDecodedBlockBytes caps the decoded size of a single strip/tile so
// hostile or corrupt headers cannot trigger multi-gigabyte allocations.
const defaultMaxDecodedBlockBytes int64 = 2 * 1024 * 1024 * 1024

type readerOptions struct {
	cacheBlocks      int
	cacheMemoryBytes int64
	strictGeoTIFF    bool
	maxBlockBytes    int64
	sidecar          *sidecarGeoref
	noSidecars       bool
}

// WithBlockCacheSize sets the number of decoded strips/tiles retained in
// memory. Values <= 0 disable caching. This overrides the memory target.
func WithBlockCacheSize(n int) ReaderOption {
	return func(o *readerOptions) {
		o.cacheBlocks = n
		o.cacheMemoryBytes = -1
	}
}

// WithBlockCacheMemory sets the approximate decoded block cache memory target,
// split evenly between the sample cache and the color cache. The reader
// converts it to block counts after reading the TIFF tile/strip layout.
// Values <= 0 disable caching.
func WithBlockCacheMemory(bytes int64) ReaderOption {
	return func(o *readerOptions) {
		o.cacheMemoryBytes = bytes
	}
}

// WithStrictGeoTIFF makes OpenDataset reject missing CRS keys, missing model
// types, and missing raster-to-model transforms. OpenOrthophoto always uses
// strict GeoTIFF validation because point colorization needs geospatial lookup.
func WithStrictGeoTIFF() ReaderOption {
	return func(o *readerOptions) {
		o.strictGeoTIFF = true
	}
}

// WithMaxDecodedBlockMemory caps the decoded in-memory size of a single TIFF
// strip or tile. Files whose layout requires larger blocks are rejected at
// open time. Values <= 0 disable the cap.
func WithMaxDecodedBlockMemory(bytes int64) ReaderOption {
	return func(o *readerOptions) {
		o.maxBlockBytes = bytes
	}
}

// WithoutSidecarFiles disables reading world files (.tfw/.wld) and GDAL PAM
// files (.aux.xml) stored next to files opened through OpenDatasetFile or
// OpenOrthophotoFile.
func WithoutSidecarFiles() ReaderOption {
	return func(o *readerOptions) {
		o.noSidecars = true
	}
}

func withSidecar(s *sidecarGeoref) ReaderOption {
	return func(o *readerOptions) {
		o.sidecar = s
	}
}

// Band describes one TIFF sample plane.
type Band struct {
	Index         int
	BitsPerSample int
	SampleFormat  SampleFormat
	NoData        *float64
	Scale         float64
	Offset        float64
}

// Dataset is a decoded TIFF/GeoTIFF raster header plus lazy strip/tile sample
// decoding. CRS and Transform are optional in permissive mode; HasTransform is
// false when pixels are readable but model-coordinate lookup is not available.
type Dataset struct {
	Name         string
	Width        int
	Height       int
	Bands        []Band
	CRS          string
	GeoKeys      *GeoTIFFMetadata
	RasterType   RasterType
	ModelType    ModelType
	Transform    RasterTransform
	HasTransform bool

	decoder *rasterDecoder
	close   func() error
}

// Orthophoto is a decoded GeoTIFF image header plus a lazy RGB strip/tile
// decoder. Coordinates in Transform and SampleModel are in the GeoTIFF model
// CRS units, not radians.
type Orthophoto struct {
	Name       string
	Width      int
	Height     int
	CRS        string
	GeoKeys    *GeoTIFFMetadata
	RasterType RasterType
	ModelType  ModelType
	Transform  RasterTransform

	decoder *rasterDecoder
	close   func() error
}

// OpenOrthophoto opens the first full-resolution image IFD from data as an RGB
// GeoTIFF orthophoto. Reduced-resolution overview IFDs are skipped when marked
// through NewSubfileType.
func OpenOrthophoto(name string, data []byte, opts ...ReaderOption) (*Orthophoto, error) {
	strictOpts := make([]ReaderOption, 0, len(opts)+1)
	strictOpts = append(strictOpts, WithStrictGeoTIFF())
	strictOpts = append(strictOpts, opts...)
	ds, err := OpenDataset(name, data, strictOpts...)
	if err != nil {
		return nil, err
	}
	return orthophotoFromDataset(name, ds)
}

// OpenOrthophotoReaderAt opens a GeoTIFF orthophoto from a random-access data
// source without loading the whole TIFF into memory. The caller owns r and must
// keep it open for as long as the returned Orthophoto is used.
func OpenOrthophotoReaderAt(name string, r io.ReaderAt, size int64, opts ...ReaderOption) (*Orthophoto, error) {
	strictOpts := make([]ReaderOption, 0, len(opts)+1)
	strictOpts = append(strictOpts, WithStrictGeoTIFF())
	strictOpts = append(strictOpts, opts...)
	ds, err := OpenDatasetReaderAt(name, r, size, strictOpts...)
	if err != nil {
		return nil, err
	}
	return orthophotoFromDataset(name, ds)
}

// OpenOrthophotoFile opens filename as a file-backed GeoTIFF orthophoto. The
// returned Orthophoto owns the file handle; call Close when done.
func OpenOrthophotoFile(filename string, opts ...ReaderOption) (*Orthophoto, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	fileOpts := append([]ReaderOption{withSidecar(loadSidecarFiles(filename))}, opts...)
	ortho, err := OpenOrthophotoReaderAt(filename, file, info.Size(), fileOpts...)
	if err != nil {
		file.Close()
		return nil, err
	}
	ortho.close = file.Close
	return ortho, nil
}

func orthophotoFromDataset(name string, ds *Dataset) (*Orthophoto, error) {
	if !ds.decoder.canRenderColor() {
		ds.Close()
		return nil, fmt.Errorf("GeoTIFF %q is not directly color-renderable", name)
	}
	ortho, err := ds.AsOrthophoto()
	if err != nil {
		ds.Close()
		return nil, err
	}
	return ortho, nil
}

// OpenDataset opens the first full-resolution image IFD from data as a generic
// typed raster dataset. It is permissive by default: incomplete GeoTIFF metadata
// leaves CRS or Transform unavailable, but pixels can still be read.
func OpenDataset(name string, data []byte, opts ...ReaderOption) (*Dataset, error) {
	return OpenDatasetReaderAt(name, bytes.NewReader(data), int64(len(data)), opts...)
}

// OpenDatasetReaderAt opens a TIFF/GeoTIFF dataset from a random-access data
// source. Only TIFF metadata and requested strips/tiles are read into memory.
// The caller owns r and must keep it open for as long as the returned Dataset is
// used.
func OpenDatasetReaderAt(name string, r io.ReaderAt, size int64, opts ...ReaderOption) (*Dataset, error) {
	cfg := readerOptions{
		cacheMemoryBytes: defaultBlockCacheMemoryBytes,
		maxBlockBytes:    defaultMaxDecodedBlockBytes,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	sidecar := cfg.sidecar
	if cfg.noSidecars {
		sidecar = nil
	}

	source, err := newTIFFSource(r, size)
	if err != nil {
		return nil, err
	}
	ifds, err := parseTIFFSource(source)
	if err != nil {
		return nil, err
	}
	ifd, err := selectImageIFD(ifds)
	if err != nil {
		return nil, err
	}

	geoKeys, err := parseGeoKeysFromIFD(ifd)
	if err != nil {
		if cfg.strictGeoTIFF && !sidecar.hasCRS() {
			return nil, fmt.Errorf("GeoTIFF %q: %w", name, err)
		}
		geoKeys = nil
	}
	modelType := ModelTypeUnknown
	rasterType := RasterTypeUnknown
	crs := ""
	if geoKeys != nil {
		modelType = ModelType(geoKeys.Short(1024))
		rasterType = RasterType(geoKeys.Short(1025))
		crs = geoKeys.CRS()
	}
	if cfg.strictGeoTIFF && modelType == 0 && !sidecar.hasCRS() {
		return nil, fmt.Errorf("GeoTIFF %q missing GTModelTypeGeoKey", name)
	}
	if sidecar.hasCRS() {
		// GDAL precedence: a PAM SRS overrides the internal GeoTIFF keys.
		crs = crsFromWKT(sidecar.pamSRS)
	}
	transform, terr := parseRasterTransform(ifd, rasterType)
	hasTransform := terr == nil
	if sidecar != nil {
		if len(sidecar.pamGeoTransform) == 6 {
			if tr, err := rasterTransformFromGeoTransform(sidecar.pamGeoTransform); err == nil {
				transform, hasTransform = tr, true
			}
		}
		if !hasTransform && len(sidecar.worldGeoTransform) == 6 {
			if tr, err := rasterTransformFromGeoTransform(sidecar.worldGeoTransform); err == nil {
				transform, hasTransform = tr, true
			}
		}
	}
	if !hasTransform {
		if cfg.strictGeoTIFF {
			return nil, fmt.Errorf("GeoTIFF %q: %w", name, terr)
		}
		transform = RasterTransform{}
	}

	decoder, err := newRasterDecoder(ifd, cfg)
	if err != nil {
		return nil, fmt.Errorf("GeoTIFF %q: %w", name, err)
	}
	if sidecar != nil && len(sidecar.pamNoData) > 0 {
		decoder.noData = overrideNoDataValues(decoder.noData, decoder.samples, sidecar.pamNoData)
	}
	bands := buildBands(decoder, ifd)
	if sidecar != nil {
		for i := range bands {
			if v, ok := sidecar.pamScale[i]; ok && v != 0 {
				bands[i].Scale = v
			}
			if v, ok := sidecar.pamOffset[i]; ok {
				bands[i].Offset = v
			}
		}
	}

	return &Dataset{
		Name:         name,
		Width:        decoder.width,
		Height:       decoder.height,
		Bands:        bands,
		CRS:          crs,
		GeoKeys:      geoKeys,
		RasterType:   rasterType,
		ModelType:    modelType,
		Transform:    transform,
		HasTransform: hasTransform,
		decoder:      decoder,
	}, nil
}

// OpenDatasetFile opens filename as a file-backed TIFF/GeoTIFF dataset. The
// returned Dataset owns the file handle; call Close when done.
func OpenDatasetFile(filename string, opts ...ReaderOption) (*Dataset, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	fileOpts := append([]ReaderOption{withSidecar(loadSidecarFiles(filename))}, opts...)
	ds, err := OpenDatasetReaderAt(filename, file, info.Size(), fileOpts...)
	if err != nil {
		file.Close()
		return nil, err
	}
	ds.close = file.Close
	return ds, nil
}

// AsOrthophoto returns a color sampling view over a directly renderable raster.
func (d *Dataset) AsOrthophoto() (*Orthophoto, error) {
	if d == nil || d.decoder == nil {
		return nil, fmt.Errorf("nil dataset")
	}
	if !d.HasTransform {
		return nil, fmt.Errorf("dataset has no raster-to-model transform")
	}
	if !d.decoder.canRenderColor() {
		return nil, fmt.Errorf("dataset is not directly color-renderable")
	}
	return &Orthophoto{
		Name:       d.Name,
		Width:      d.Width,
		Height:     d.Height,
		CRS:        d.CRS,
		GeoKeys:    d.GeoKeys,
		RasterType: d.RasterType,
		ModelType:  d.ModelType,
		Transform:  d.Transform,
		decoder:    d.decoder,
		close:      d.takeClose(),
	}, nil
}

// Close releases resources owned by this Dataset, if any.
func (d *Dataset) Close() error {
	if d == nil || d.close == nil {
		return nil
	}
	closeFn := d.close
	d.close = nil
	return closeFn()
}

func (d *Dataset) takeClose() func() error {
	if d == nil {
		return nil
	}
	closeFn := d.close
	d.close = nil
	return closeFn
}

// Close releases resources owned by this Orthophoto, if any.
func (o *Orthophoto) Close() error {
	if o == nil || o.close == nil {
		return nil
	}
	closeFn := o.close
	o.close = nil
	return closeFn()
}

// RawSample returns the unscaled value at zero-based raster band.
func (d *Dataset) RawSample(x, y, band int) (float64, bool, error) {
	if d == nil || d.decoder == nil {
		return 0, false, fmt.Errorf("nil dataset")
	}
	if x < 0 || y < 0 || x >= d.Width || y >= d.Height {
		return 0, false, nil
	}
	if band < 0 || band >= len(d.Bands) {
		return 0, false, fmt.Errorf("band %d outside dataset", band)
	}
	return d.decoder.sampleAt(x, y, band)
}

// Sample returns the scaled value at zero-based raster band. It returns ok=false
// when the pixel is outside the raster or matches the band's NoData value.
func (d *Dataset) Sample(x, y, band int) (float64, bool, error) {
	v, ok, err := d.RawSample(x, y, band)
	if err != nil || !ok {
		return 0, ok, err
	}
	meta := d.Bands[band]
	if meta.NoData != nil && sampleEqualsNoData(v, *meta.NoData) {
		return 0, false, nil
	}
	return v*meta.Scale + meta.Offset, true, nil
}

// RawPixel returns all unscaled band values at integer raster coordinates.
func (d *Dataset) RawPixel(x, y int) ([]float64, bool, error) {
	if d == nil || d.decoder == nil {
		return nil, false, fmt.Errorf("nil dataset")
	}
	if x < 0 || y < 0 || x >= d.Width || y >= d.Height {
		return nil, false, nil
	}
	return d.decoder.samplesAt(x, y)
}

// Pixel returns all scaled band values at integer raster coordinates.
func (d *Dataset) Pixel(x, y int) ([]float64, bool, error) {
	vals, ok, err := d.RawPixel(x, y)
	if err != nil || !ok {
		return nil, ok, err
	}
	out := make([]float64, len(vals))
	for i, v := range vals {
		meta := d.Bands[i]
		if meta.NoData != nil && sampleEqualsNoData(v, *meta.NoData) {
			return nil, false, nil
		}
		out[i] = v*meta.Scale + meta.Offset
	}
	return out, true, nil
}

// Pixel returns the decoded color at integer raster coordinates.
func (o *Orthophoto) Pixel(x, y int) (Color, bool, error) {
	if o == nil || o.decoder == nil {
		return Color{}, false, fmt.Errorf("nil orthophoto")
	}
	if x < 0 || y < 0 || x >= o.Width || y >= o.Height {
		return Color{}, false, nil
	}
	return o.decoder.colorAt(x, y)
}

// ModelToRaster maps model CRS coordinates to floating-point raster space.
func (o *Orthophoto) ModelToRaster(x, y float64) (float64, float64) {
	return o.Transform.ModelToRaster(x, y)
}

// SampleModel samples the image at model CRS coordinates.
func (o *Orthophoto) SampleModel(x, y float64, mode SamplingMode) (Color, bool, error) {
	if o == nil {
		return Color{}, false, fmt.Errorf("nil orthophoto")
	}
	i, j := o.Transform.ModelToRaster(x, y)
	switch mode {
	case SampleNearest:
		px, py := o.nearestPixel(i, j)
		return o.Pixel(px, py)
	case SampleBilinear:
		return o.bilinearPixel(i, j)
	default:
		return Color{}, false, fmt.Errorf("unsupported sampling mode %d", mode)
	}
}

func (o *Orthophoto) nearestPixel(i, j float64) (int, int) {
	if o.RasterType == RasterPixelIsPoint {
		return int(math.Floor(i + 0.5)), int(math.Floor(j + 0.5))
	}
	// For imagery, unknown raster space is treated as PixelIsArea: the inverse
	// transform returns continuous coordinates in pixel areas.
	return int(math.Floor(i)), int(math.Floor(j))
}

func (o *Orthophoto) bilinearPixel(i, j float64) (Color, bool, error) {
	// A point is sampleable iff its nearest pixel is inside the raster, the
	// same footprint rule nearest sampling uses.
	if px, py := o.nearestPixel(i, j); px < 0 || py < 0 || px >= o.Width || py >= o.Height {
		return Color{}, false, nil
	}
	if o.RasterType != RasterPixelIsPoint {
		i -= 0.5
		j -= 0.5
	}
	x0 := int(math.Floor(i))
	y0 := int(math.Floor(j))
	fx := i - float64(x0)
	fy := j - float64(y0)
	// Clamp the 2x2 neighborhood to the raster. In the outer half-pixel band
	// the duplicated neighbor degrades the interpolation to linear/nearest
	// instead of failing the sample.
	x1 := minInt(x0+1, o.Width-1)
	y1 := minInt(y0+1, o.Height-1)
	x0 = maxInt(x0, 0)
	y0 = maxInt(y0, 0)
	c00, c10, c01, c11, err := o.decoder.colorQuad(x0, y0, x1, y1)
	if err != nil {
		return Color{}, false, err
	}
	return bilerpColor(c00, c10, c01, c11, fx, fy), true, nil
}

func bilerp(c00, c10, c01, c11 uint8, fx, fy float64) uint8 {
	v0 := float64(c00)*(1-fx) + float64(c10)*fx
	v1 := float64(c01)*(1-fx) + float64(c11)*fx
	v := v0*(1-fy) + v1*fy
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func bilerpColor(c00, c10, c01, c11 Color, fx, fy float64) Color {
	a := bilerpFloat(float64(c00.A), float64(c10.A), float64(c01.A), float64(c11.A), fx, fy)
	if a <= 0 {
		return Color{}
	}
	return Color{
		R: unpremultiplyBilerp(c00.R, c10.R, c01.R, c11.R, c00.A, c10.A, c01.A, c11.A, a, fx, fy),
		G: unpremultiplyBilerp(c00.G, c10.G, c01.G, c11.G, c00.A, c10.A, c01.A, c11.A, a, fx, fy),
		B: unpremultiplyBilerp(c00.B, c10.B, c01.B, c11.B, c00.A, c10.A, c01.A, c11.A, a, fx, fy),
		A: clampByte(a),
	}
}

func unpremultiplyBilerp(c00, c10, c01, c11, a00, a10, a01, a11 uint8, a, fx, fy float64) uint8 {
	p := bilerpFloat(
		float64(c00)*float64(a00),
		float64(c10)*float64(a10),
		float64(c01)*float64(a01),
		float64(c11)*float64(a11),
		fx,
		fy,
	)
	return clampByte(p / a)
}

func bilerpFloat(c00, c10, c01, c11, fx, fy float64) float64 {
	v0 := c00*(1-fx) + c10*fx
	v1 := c01*(1-fx) + c11*fx
	return v0*(1-fy) + v1*fy
}

func clampByte(v float64) uint8 {
	if v <= 0 || math.IsNaN(v) {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

type tiffIFD struct {
	tags   map[uint16]tiffValue
	order  binary.ByteOrder
	source *tiffSource
}

type tiffValue struct {
	typ uint16
	raw []byte
}

type tiffSource struct {
	reader io.ReaderAt
	size   int64
}

func newTIFFSource(r io.ReaderAt, size int64) (*tiffSource, error) {
	if r == nil {
		return nil, fmt.Errorf("nil TIFF reader")
	}
	if size < 0 {
		return nil, fmt.Errorf("invalid TIFF size %d", size)
	}
	return &tiffSource{reader: r, size: size}, nil
}

func (s *tiffSource) readAt(offset, count uint64) ([]byte, error) {
	if s == nil || s.reader == nil {
		return nil, fmt.Errorf("nil TIFF source")
	}
	if count == 0 {
		return nil, nil
	}
	size := uint64(s.size)
	if offset > size || count > size-offset {
		return nil, fmt.Errorf("TIFF range offset %d length %d outside file", offset, count)
	}
	if count > uint64(maxIntValue()) {
		return nil, fmt.Errorf("TIFF read range is too large")
	}
	buf := make([]byte, int(count))
	n, err := s.reader.ReadAt(buf, int64(offset))
	if err != nil && err != io.EOF {
		return nil, err
	}
	if n != len(buf) {
		return nil, io.ErrUnexpectedEOF
	}
	return buf, nil
}

func (s *tiffSource) sizeUint64() uint64 {
	if s == nil || s.size <= 0 {
		return 0
	}
	return uint64(s.size)
}

func isTIFF(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return (data[0] == 'I' && data[1] == 'I' || data[0] == 'M' && data[1] == 'M') &&
		(data[2] == 0x2a && data[3] == 0 || data[3] == 0x2a && data[2] == 0 ||
			data[2] == 0x2b && data[3] == 0 || data[3] == 0x2b && data[2] == 0)
}

func parseTIFFSource(source *tiffSource) ([]*tiffIFD, error) {
	header, err := source.readAt(0, 4)
	if err != nil {
		return nil, fmt.Errorf("short TIFF header")
	}
	if !isTIFF(header) {
		return nil, fmt.Errorf("not a TIFF file")
	}
	var order binary.ByteOrder
	switch string(header[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, fmt.Errorf("unsupported TIFF byte order")
	}
	magic := order.Uint16(header[2:4])
	big := magic == 43
	var off uint64
	if big {
		bigHeader, err := source.readAt(0, 16)
		if err != nil || order.Uint16(bigHeader[4:6]) != 8 || order.Uint16(bigHeader[6:8]) != 0 {
			return nil, fmt.Errorf("invalid BigTIFF header")
		}
		off = order.Uint64(bigHeader[8:16])
	} else if magic == 42 {
		classicHeader, err := source.readAt(0, 8)
		if err != nil {
			return nil, fmt.Errorf("short TIFF header")
		}
		off = uint64(order.Uint32(classicHeader[4:8]))
	} else {
		return nil, fmt.Errorf("unsupported TIFF magic %d", magic)
	}

	const maxIFDs = 4096
	seen := make(map[uint64]bool)
	var ifds []*tiffIFD
	for off != 0 {
		if off > source.sizeUint64() {
			return nil, fmt.Errorf("IFD offset %d outside file", off)
		}
		if seen[off] {
			return nil, fmt.Errorf("TIFF IFD chain loops at offset %d", off)
		}
		if len(ifds) >= maxIFDs {
			return nil, fmt.Errorf("TIFF IFD chain exceeds %d entries", maxIFDs)
		}
		seen[off] = true
		ifd, next, err := parseIFD(source, order, big, off)
		if err != nil {
			return nil, err
		}
		ifds = append(ifds, ifd)
		off = next
	}
	if len(ifds) == 0 {
		return nil, fmt.Errorf("TIFF has no image directories")
	}
	return ifds, nil
}

func parseIFD(source *tiffSource, order binary.ByteOrder, big bool, off uint64) (*tiffIFD, uint64, error) {
	var count uint64
	countSize := uint64(2)
	if big {
		countSize = 8
	}
	countBuf, err := source.readAt(off, countSize)
	if err != nil {
		if big {
			return nil, 0, fmt.Errorf("short BigTIFF IFD count")
		}
		return nil, 0, fmt.Errorf("short TIFF IFD count")
	}
	if big {
		count = order.Uint64(countBuf)
	} else {
		count = uint64(order.Uint16(countBuf))
	}
	entrySize := uint64(12)
	valueFieldSize := uint64(4)
	if big {
		entrySize = 20
		valueFieldSize = 8
	}
	if count > 10000 || count > (uint64(maxIntValue())-valueFieldSize)/entrySize {
		return nil, 0, fmt.Errorf("invalid TIFF IFD entry count")
	}
	bodySize := count*entrySize + valueFieldSize
	body, err := source.readAt(off+countSize, bodySize)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid TIFF IFD entry count")
	}

	ifd := &tiffIFD{tags: make(map[uint16]tiffValue, count), order: order, source: source}
	pos := 0
	for i := uint64(0); i < count; i++ {
		entry := body[pos : pos+int(entrySize)]
		tag := order.Uint16(entry[0:2])
		typ := order.Uint16(entry[2:4])
		var n uint64
		var valueField []byte
		if big {
			n = order.Uint64(entry[4:12])
			valueField = entry[12:20]
		} else {
			n = uint64(order.Uint32(entry[4:8]))
			valueField = entry[8:12]
		}
		typeSize := uint64(tiffTypeSize(typ))
		if typeSize == 0 || n == 0 {
			pos += int(entrySize)
			continue
		}
		if n > source.sizeUint64()/typeSize {
			return nil, 0, fmt.Errorf("TIFF tag %d is too large", tag)
		}
		size := n * typeSize
		var raw []byte
		if size <= uint64(valueFieldSize) {
			raw = append([]byte(nil), valueField[:int(size)]...)
		} else {
			var valueOff uint64
			if big {
				valueOff = order.Uint64(valueField)
			} else {
				valueOff = uint64(order.Uint32(valueField[:4]))
			}
			raw, err = source.readAt(valueOff, size)
			if err != nil {
				return nil, 0, fmt.Errorf("TIFF tag %d value outside file", tag)
			}
		}
		ifd.tags[tag] = tiffValue{typ: typ, raw: raw}
		pos += int(entrySize)
	}

	var next uint64
	if big {
		next = order.Uint64(body[pos : pos+8])
	} else {
		next = uint64(order.Uint32(body[pos : pos+4]))
	}
	return ifd, next, nil
}

func tiffTypeSize(typ uint16) int {
	switch typ {
	case tiffTypeByte, tiffTypeASCII, tiffTypeSByte, tiffTypeUndefined:
		return 1
	case tiffTypeShort, tiffTypeSShort:
		return 2
	case tiffTypeLong, tiffTypeSLong, tiffTypeFloat:
		return 4
	case tiffTypeRational, tiffTypeSRational, tiffTypeDouble, tiffTypeLong8, tiffTypeSLong8, tiffTypeIFD8:
		return 8
	default:
		return 0
	}
}

func selectImageIFD(ifds []*tiffIFD) (*tiffIFD, error) {
	var fallback *tiffIFD
	for _, ifd := range ifds {
		if len(ifd.uints(tiffTagImageWidth)) == 0 || len(ifd.uints(tiffTagImageLength)) == 0 {
			continue
		}
		if fallback == nil {
			fallback = ifd
		}
		subfile := ifd.uints(tiffTagNewSubfileType)
		if len(subfile) == 0 || subfile[0]&1 == 0 {
			return ifd, nil
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("TIFF has no image IFD")
}

func (ifd *tiffIFD) raw(tag uint16) ([]byte, bool) {
	v, ok := ifd.tags[tag]
	if !ok {
		return nil, false
	}
	return v.raw, true
}

func (ifd *tiffIFD) uints(tag uint16) []uint64 {
	v, ok := ifd.tags[tag]
	if !ok {
		return nil
	}
	var out []uint64
	switch v.typ {
	case tiffTypeByte, tiffTypeUndefined:
		const maxByteUintValues = 1 << 20
		if len(v.raw) > maxByteUintValues {
			return nil
		}
		out = make([]uint64, len(v.raw))
		for i, b := range v.raw {
			out[i] = uint64(b)
		}
	case tiffTypeShort:
		for i := 0; i+2 <= len(v.raw); i += 2 {
			out = append(out, uint64(ifd.order.Uint16(v.raw[i:i+2])))
		}
	case tiffTypeLong:
		for i := 0; i+4 <= len(v.raw); i += 4 {
			out = append(out, uint64(ifd.order.Uint32(v.raw[i:i+4])))
		}
	case tiffTypeLong8, tiffTypeIFD8:
		for i := 0; i+8 <= len(v.raw); i += 8 {
			out = append(out, ifd.order.Uint64(v.raw[i:i+8]))
		}
	}
	return out
}

func (ifd *tiffIFD) float64s(tag uint16) []float64 {
	v, ok := ifd.tags[tag]
	if !ok {
		return nil
	}
	var out []float64
	switch v.typ {
	case tiffTypeRational:
		for i := 0; i+8 <= len(v.raw); i += 8 {
			num := ifd.order.Uint32(v.raw[i : i+4])
			den := ifd.order.Uint32(v.raw[i+4 : i+8])
			if den == 0 {
				out = append(out, math.NaN())
			} else {
				out = append(out, float64(num)/float64(den))
			}
		}
	case tiffTypeSRational:
		for i := 0; i+8 <= len(v.raw); i += 8 {
			num := int32(ifd.order.Uint32(v.raw[i : i+4]))
			den := int32(ifd.order.Uint32(v.raw[i+4 : i+8]))
			if den == 0 {
				out = append(out, math.NaN())
			} else {
				out = append(out, float64(num)/float64(den))
			}
		}
	case tiffTypeDouble:
		for i := 0; i+8 <= len(v.raw); i += 8 {
			out = append(out, math.Float64frombits(ifd.order.Uint64(v.raw[i:i+8])))
		}
	case tiffTypeFloat:
		for i := 0; i+4 <= len(v.raw); i += 4 {
			out = append(out, float64(math.Float32frombits(ifd.order.Uint32(v.raw[i:i+4]))))
		}
	}
	return out
}

func expandUints(vals []uint64, n int, fallback uint64) []uint64 {
	if len(vals) == 0 {
		vals = []uint64{fallback}
	}
	out := make([]uint64, n)
	for i := range out {
		if i < len(vals) {
			out[i] = vals[i]
		} else {
			out[i] = vals[len(vals)-1]
		}
	}
	return out
}

type RasterTransform struct {
	// Model = [A B C; D E F] * [I J 1].
	A float64
	B float64
	C float64
	D float64
	E float64
	F float64

	invA float64
	invB float64
	invC float64
	invD float64
	invE float64
	invF float64
}

func (t RasterTransform) RasterToModel(i, j float64) (float64, float64) {
	return t.A*i + t.B*j + t.C, t.D*i + t.E*j + t.F
}

func (t RasterTransform) ModelToRaster(x, y float64) (float64, float64) {
	return t.invA*x + t.invB*y + t.invC, t.invD*x + t.invE*y + t.invF
}

func parseRasterTransform(ifd *tiffIFD, rasterType RasterType) (RasterTransform, error) {
	matrix := ifd.float64s(tiffTagModelTransformation)
	scale := ifd.float64s(tiffTagModelPixelScale)
	tie := ifd.float64s(tiffTagModelTiepoint)
	if len(matrix) == 16 {
		if len(scale) > 0 {
			return RasterTransform{}, fmt.Errorf("ModelTransformationTag and ModelPixelScaleTag must not both be present")
		}
		return newRasterTransform(matrix[0], matrix[1], matrix[3], matrix[4], matrix[5], matrix[7])
	}
	if len(scale) > 0 {
		if len(scale) != 3 {
			return RasterTransform{}, fmt.Errorf("ModelPixelScaleTag must contain 3 values")
		}
		if len(tie) < 6 || len(tie)%6 != 0 {
			return RasterTransform{}, fmt.Errorf("ModelPixelScaleTag requires ModelTiepointTag entries of 6 values")
		}
		// GeoTIFF convention: positive ScaleY means model Y decreases as raster J
		// increases. Some non-compliant writers store a negative ScaleY for the
		// same north-up layout; GDAL and libtiff treat it like a positive value,
		// so the magnitude is used regardless of sign.
		i0, j0 := tie[0], tie[1]
		x0, y0 := tie[3], tie[4]
		a := scale[0]
		e := -math.Abs(scale[1])
		c := x0 - i0*a
		f := y0 - j0*e
		return newRasterTransform(a, 0, c, 0, e, f)
	}
	if len(tie) >= 18 {
		return RasterTransform{}, fmt.Errorf("multiple tiepoints without ModelPixelScaleTag are not supported for affine lookup")
	}
	if len(tie) >= 6 {
		return RasterTransform{}, fmt.Errorf("ModelTiepointTag without ModelPixelScaleTag is insufficient for pixel lookup")
	}
	return RasterTransform{}, fmt.Errorf("missing GeoTIFF raster-to-model transform")
}

func newRasterTransform(a, b, c, d, e, f float64) (RasterTransform, error) {
	det := a*e - b*d
	if math.Abs(det) < 1e-30 {
		return RasterTransform{}, fmt.Errorf("raster-to-model transform is singular")
	}
	return RasterTransform{
		A: a, B: b, C: c,
		D: d, E: e, F: f,
		invA: e / det, invB: -b / det, invC: (b*f - e*c) / det,
		invD: -d / det, invE: a / det, invF: (d*c - a*f) / det,
	}, nil
}

type rasterDecoder struct {
	ifd                *tiffIFD
	width              int
	height             int
	samples            int
	bits               []uint64
	formats            []uint64
	photometric        uint64
	compression        uint64
	planarConfig       uint64
	predictor          uint64
	fillOrder          uint64
	extraSamples       []uint64
	colorMap           []uint64
	noData             []*float64
	jpegTables         []byte
	lercAddCompression uint64
	yCbCrSubsample     []uint64
	yCbCrCoeff         []float64
	referenceBW        []float64
	tiled              bool
	tileWidth          int
	tileHeight         int
	rowsPerStrip       int
	tilesAcross        int
	tilesDown          int
	blocksPerPlane     int
	offsets            []uint64
	counts             []uint64
	cache              *blockCache
	colorCache         *colorBlockCache
	flightMu           sync.Mutex
	flights            map[int]*blockFlight
	colorFlightMu      sync.Mutex
	colorFlights       map[int]*colorBlockFlight
}

func newRasterDecoder(ifd *tiffIFD, cfg readerOptions) (*rasterDecoder, error) {
	widths := ifd.uints(tiffTagImageWidth)
	heights := ifd.uints(tiffTagImageLength)
	if len(widths) == 0 || len(heights) == 0 {
		return nil, fmt.Errorf("missing TIFF image dimensions")
	}
	width, height := int(widths[0]), int(heights[0])
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid TIFF image dimensions %dx%d", width, height)
	}
	samples := 1
	if vals := ifd.uints(tiffTagSamplesPerPixel); len(vals) > 0 {
		samples = int(vals[0])
	}
	if samples <= 0 {
		return nil, fmt.Errorf("invalid SamplesPerPixel value")
	}
	photometric := uint64(tiffPhotometricBlackIsZero)
	if vals := ifd.uints(tiffTagPhotometric); len(vals) > 0 {
		photometric = vals[0]
	}
	compression := uint64(tiffCompressionNone)
	if vals := ifd.uints(tiffTagCompression); len(vals) > 0 {
		compression = vals[0]
	}
	planarConfig := uint64(tiffPlanarChunky)
	if vals := ifd.uints(tiffTagPlanarConfig); len(vals) > 0 {
		planarConfig = vals[0]
	}
	if planarConfig != tiffPlanarChunky && planarConfig != tiffPlanarSeparate {
		return nil, fmt.Errorf("unsupported TIFF PlanarConfiguration %d", planarConfig)
	}
	predictor := uint64(1)
	if vals := ifd.uints(tiffTagPredictor); len(vals) > 0 {
		predictor = vals[0]
	}
	if predictor != 1 && predictor != 2 && predictor != 3 {
		return nil, fmt.Errorf("unsupported TIFF Predictor %d", predictor)
	}
	if (compression == tiffCompressionJPEG || compression == tiffCompressionWebP) && planarConfig != tiffPlanarChunky {
		return nil, fmt.Errorf("image-compressed TIFF with separate planes is not supported")
	}
	if compression == tiffCompressionOldJPEG {
		return nil, fmt.Errorf("old-style TIFF JPEG compression is not supported")
	}
	if !supportedTIFFCompression(compression) {
		return nil, fmt.Errorf("unsupported TIFF compression %d", compression)
	}

	bitsRaw := ifd.uints(tiffTagBitsPerSample)
	if len(bitsRaw) == 0 {
		// TIFF6: BitsPerSample defaults to 1 (bilevel images may omit it).
		bitsRaw = []uint64{1}
	}
	bits := expandUints(bitsRaw, samples, bitsRaw[len(bitsRaw)-1])
	fillOrder := uint64(1)
	if vals := ifd.uints(tiffTagFillOrder); len(vals) > 0 {
		fillOrder = vals[0]
	}
	if fillOrder != 1 && fillOrder != 2 {
		return nil, fmt.Errorf("unsupported TIFF FillOrder %d", fillOrder)
	}
	if fillOrder == 2 && (compression == tiffCompressionJPEG || compression == tiffCompressionWebP || compression == tiffCompressionLERC) {
		return nil, fmt.Errorf("TIFF FillOrder 2 with image-compressed blocks is not supported")
	}
	formats := expandUints(ifd.uints(tiffTagSampleFormat), samples, tiffSampleFormatUnsigned)
	extraSamples := ifd.uints(tiffTagExtraSamples)
	colorMap := ifd.uints(tiffTagColorMap)
	jpegTables, _ := ifd.raw(tiffTagJPEGTables)
	yCbCrSubsample := ifd.uints(tiffTagYCbCrSubSampling)
	yCbCrCoeff := ifd.float64s(tiffTagYCbCrCoefficients)
	referenceBW := ifd.float64s(tiffTagReferenceBlackWhite)

	decoder := &rasterDecoder{
		ifd:                ifd,
		width:              width,
		height:             height,
		samples:            samples,
		bits:               bits,
		formats:            formats,
		photometric:        photometric,
		compression:        compression,
		planarConfig:       planarConfig,
		predictor:          predictor,
		fillOrder:          fillOrder,
		extraSamples:       extraSamples,
		colorMap:           colorMap,
		noData:             parseGDALNoData(ifd),
		jpegTables:         append([]byte(nil), jpegTables...),
		lercAddCompression: lercAddCompressionFromParams(ifd.uints(tiffTagLERCParameters)),
		yCbCrSubsample:     yCbCrSubsample,
		yCbCrCoeff:         yCbCrCoeff,
		referenceBW:        referenceBW,
		rowsPerStrip:       height,
	}
	if err := decoder.validateSamples(); err != nil {
		return nil, err
	}
	if err := decoder.initLayout(); err != nil {
		return nil, err
	}
	if err := decoder.validateBlockMemory(cfg.maxBlockBytes); err != nil {
		return nil, err
	}
	sampleBlocks, colorBlocks := decoder.cacheBlockCounts(cfg)
	decoder.cache = newBlockCache(sampleBlocks)
	decoder.colorCache = newColorBlockCache(colorBlocks)
	return decoder, nil
}

func (d *rasterDecoder) validateSamples() error {
	switch d.photometric {
	case tiffPhotometricRGB:
		if d.samples < 3 {
			return fmt.Errorf("RGB TIFF needs at least 3 samples per pixel")
		}
	case tiffPhotometricBlackIsZero, tiffPhotometricWhiteIsZero:
		if d.samples < 1 {
			return fmt.Errorf("grayscale TIFF needs at least 1 sample per pixel")
		}
	case tiffPhotometricPalette:
		if d.samples < 1 {
			return fmt.Errorf("palette TIFF needs at least 1 sample per pixel")
		}
		if d.bits[0] > 16 {
			return fmt.Errorf("unsupported palette TIFF BitsPerSample %d", d.bits[0])
		}
		if len(d.colorMap) == 0 {
			return fmt.Errorf("palette TIFF is missing ColorMap")
		}
	case tiffPhotometricYCbCr:
		if d.samples != 3 {
			return fmt.Errorf("YCbCr TIFF needs exactly 3 samples per pixel")
		}
		if d.compression != tiffCompressionJPEG && d.planarConfig != tiffPlanarChunky {
			return fmt.Errorf("uncompressed YCbCr TIFF with separate planes is not supported")
		}
	default:
		return fmt.Errorf("unsupported TIFF PhotometricInterpretation %d", d.photometric)
	}
	if d.compression == tiffCompressionJPEG || d.compression == tiffCompressionWebP {
		return nil
	}
	for s := 0; s < d.samples; s++ {
		if err := validateSampleEncoding(d.formats[s], d.bits[s]); err != nil {
			return err
		}
	}
	if d.photometric == tiffPhotometricYCbCr && d.compression != tiffCompressionJPEG {
		for s := 0; s < d.samples; s++ {
			if d.formats[s] != tiffSampleFormatUnsigned || d.bits[s] != 8 {
				return fmt.Errorf("uncompressed YCbCr TIFF supports only unsigned 8-bit samples")
			}
		}
		h, v := d.yCbCrSubsampling()
		if (h != 1 && h != 2 && h != 4) || (v != 1 && v != 2 && v != 4) || v > h {
			return fmt.Errorf("unsupported YCbCrSubSampling %dx%d", h, v)
		}
	}
	if d.predictor != 1 {
		for s := 1; s < d.samples; s++ {
			if d.bits[s] != d.bits[0] || d.formats[s] != d.formats[0] {
				return fmt.Errorf("horizontal predictor with mixed sample widths is not supported")
			}
		}
		if d.bits[0]%8 != 0 {
			return fmt.Errorf("horizontal predictor with bit-packed samples is not supported")
		}
	}
	if d.predictor == 2 {
		if d.bits[0] != 8 && d.bits[0] != 16 && d.bits[0] != 32 && d.bits[0] != 64 {
			return fmt.Errorf("unsupported TIFF Predictor 2 BitsPerSample %d", d.bits[0])
		}
	}
	if d.predictor == 3 {
		if d.formats[0] != tiffSampleFormatFloat {
			return fmt.Errorf("TIFF Predictor 3 requires floating point samples")
		}
		if d.bits[0] != 16 && d.bits[0] != 32 && d.bits[0] != 64 {
			return fmt.Errorf("unsupported TIFF Predictor 3 BitsPerSample %d", d.bits[0])
		}
	}
	return nil
}

func validateSampleEncoding(format, bits uint64) error {
	switch format {
	case tiffSampleFormatUnsigned:
		switch bits {
		case 1, 2, 4, 8, 16, 32, 64:
			return nil
		}
	case tiffSampleFormatSigned:
		switch bits {
		case 8, 16, 32, 64:
			return nil
		}
	case tiffSampleFormatFloat:
		switch bits {
		case 16, 32, 64:
			return nil
		}
	}
	return fmt.Errorf("unsupported TIFF SampleFormat %d BitsPerSample %d", format, bits)
}

func (d *rasterDecoder) initLayout() error {
	if offsets := d.ifd.uints(tiffTagTileOffsets); len(offsets) > 0 {
		widths := d.ifd.uints(tiffTagTileWidth)
		heights := d.ifd.uints(tiffTagTileLength)
		counts := d.ifd.uints(tiffTagTileByteCounts)
		if len(widths) == 0 || len(heights) == 0 || len(counts) != len(offsets) {
			return fmt.Errorf("unsupported TIFF tile layout")
		}
		d.tiled = true
		d.tileWidth, d.tileHeight = int(widths[0]), int(heights[0])
		if d.tileWidth <= 0 || d.tileHeight <= 0 {
			return fmt.Errorf("invalid TIFF tile size")
		}
		d.tilesAcross = (d.width + d.tileWidth - 1) / d.tileWidth
		d.tilesDown = (d.height + d.tileHeight - 1) / d.tileHeight
		d.blocksPerPlane = d.tilesAcross * d.tilesDown
		d.offsets, d.counts = offsets, counts
		return d.validateBlockCount()
	}

	offsets := d.ifd.uints(tiffTagStripOffsets)
	counts := d.ifd.uints(tiffTagStripByteCounts)
	if len(offsets) == 0 {
		return fmt.Errorf("missing TIFF strip or tile offsets")
	}
	if vals := d.ifd.uints(tiffTagRowsPerStrip); len(vals) > 0 {
		d.rowsPerStrip = int(vals[0])
	}
	if d.rowsPerStrip <= 0 {
		return fmt.Errorf("invalid RowsPerStrip value")
	}
	d.blocksPerPlane = (d.height + d.rowsPerStrip - 1) / d.rowsPerStrip
	sourceSize := d.ifd.source.sizeUint64()
	countOutsideSource := len(counts) > 0 && offsets[0] <= sourceSize && counts[0] > sourceSize-offsets[0]
	if len(offsets) == 1 && (len(counts) == 0 || counts[0] == 0 || offsets[0] > sourceSize || countOutsideSource) {
		estimate := uint64(0)
		if offsets[0] > sourceSize {
			return fmt.Errorf("TIFF strip offset outside file")
		}
		if d.compression == tiffCompressionNone {
			rowBytes, err := d.rowBytes(d.width, 0, d.samples)
			if err != nil {
				return err
			}
			estimate = uint64(rowBytes * d.height)
		}
		if estimate == 0 || offsets[0]+estimate > sourceSize {
			estimate = sourceSize - offsets[0]
		}
		counts = []uint64{estimate}
	}
	if len(counts) != len(offsets) {
		return fmt.Errorf("unsupported TIFF strip layout")
	}
	d.offsets, d.counts = offsets, counts
	return d.validateBlockCount()
}

// validateBlockMemory rejects layouts whose single decoded block would exceed
// limit bytes. The arithmetic is division-based so hostile block dimensions
// cannot overflow the check itself.
func (d *rasterDecoder) validateBlockMemory(limit int64) error {
	if limit <= 0 {
		return nil
	}
	blockWidth, blockHeight := d.width, minInt(d.rowsPerStrip, d.height)
	if d.tiled {
		blockWidth, blockHeight = d.tileWidth, d.tileHeight
	}
	perPixel := uint64(d.sampleBlockSampleCount()) * 8
	budget := uint64(limit) / perPixel
	if blockWidth <= 0 || blockHeight <= 0 || uint64(blockHeight) > budget/uint64(blockWidth) {
		return fmt.Errorf("decoded TIFF block %dx%d with %d samples exceeds the %d byte limit (raise it with WithMaxDecodedBlockMemory)",
			blockWidth, blockHeight, d.sampleBlockSampleCount(), limit)
	}
	return nil
}

func (d *rasterDecoder) cacheBlockCounts(cfg readerOptions) (sampleBlocks, colorBlocks int) {
	if cfg.cacheMemoryBytes < 0 {
		return cfg.cacheBlocks, cfg.cacheBlocks
	}
	// Split the memory target between the sample and the color cache so a
	// dataset used through both paths stays within the configured budget.
	half := cfg.cacheMemoryBytes / 2
	return cacheBlocksForMemory(half, d.sampleBlockDecodedBytes(), d.blocksPerPlane),
		cacheBlocksForMemory(half, d.colorBlockDecodedBytes(), d.blocksPerPlane)
}

func (d *rasterDecoder) sampleBlockDecodedBytes() int64 {
	pixels := d.maxDecodedBlockPixels()
	samples := d.sampleBlockSampleCount()
	return pixels * int64(samples) * 8
}

func (d *rasterDecoder) colorBlockDecodedBytes() int64 {
	return d.maxDecodedBlockPixels() * 4
}

func (d *rasterDecoder) maxDecodedBlockPixels() int64 {
	if d.tiled {
		return int64(d.tileWidth) * int64(d.tileHeight)
	}
	return int64(d.width) * int64(minInt(d.rowsPerStrip, d.height))
}

func (d *rasterDecoder) sampleBlockSampleCount() int {
	switch {
	case d.compression == tiffCompressionJPEG || d.compression == tiffCompressionWebP:
		return maxInt(d.samples, 3)
	case d.photometric == tiffPhotometricYCbCr:
		return 3
	default:
		return d.samples
	}
}

func cacheBlocksForMemory(targetBytes, decodedBlockBytes int64, maxBlocks int) int {
	if targetBytes <= 0 || decodedBlockBytes <= 0 || maxBlocks <= 0 {
		return 0
	}
	blocks := targetBytes / decodedBlockBytes
	if blocks < 1 {
		blocks = 1
	}
	if blocks > int64(maxBlocks) {
		blocks = int64(maxBlocks)
	}
	if blocks > int64(maxIntValue()) {
		return maxIntValue()
	}
	return int(blocks)
}

func (d *rasterDecoder) validateBlockCount() error {
	expected := d.blocksPerPlane
	if d.planarConfig == tiffPlanarSeparate {
		expected *= d.samples
	}
	if len(d.offsets) != expected {
		return fmt.Errorf("TIFF block count is %d, expected %d", len(d.offsets), expected)
	}
	return nil
}

func (d *rasterDecoder) colorAt(x, y int) (Color, bool, error) {
	if !d.canRenderColor() {
		return Color{}, false, fmt.Errorf("TIFF photometric/sample layout is not directly color-renderable")
	}
	blockIndex, localX, localY := d.blockForPixel(x, y)
	block, err := d.loadColorBlock(blockIndex)
	if err != nil {
		return Color{}, false, err
	}
	if localX < 0 || localY < 0 || localX >= block.width || localY >= block.height {
		return Color{}, false, nil
	}
	return block.values[(localY*block.width)+localX], true, nil
}

// colorQuad returns the colors of the four in-bounds corner pixels
// (x0,y0)-(x1,y1), loading each underlying block only once. The interpolation
// quad usually sits inside a single block, so this takes one cache lookup
// where four Pixel calls would take four.
func (d *rasterDecoder) colorQuad(x0, y0, x1, y1 int) (c00, c10, c01, c11 Color, err error) {
	if !d.canRenderColor() {
		return Color{}, Color{}, Color{}, Color{}, fmt.Errorf("TIFF photometric/sample layout is not directly color-renderable")
	}
	corners := [4][2]int{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}}
	var out [4]Color
	var indexes [4]int
	var blocks [4]*colorBlock
	for n, corner := range corners {
		blockIndex, localX, localY := d.blockForPixel(corner[0], corner[1])
		var block *colorBlock
		for m := 0; m < n; m++ {
			if blocks[m] != nil && indexes[m] == blockIndex {
				block = blocks[m]
				break
			}
		}
		if block == nil {
			block, err = d.loadColorBlock(blockIndex)
			if err != nil {
				return Color{}, Color{}, Color{}, Color{}, err
			}
		}
		indexes[n], blocks[n] = blockIndex, block
		out[n] = block.values[(localY*block.width)+localX]
	}
	return out[0], out[1], out[2], out[3], nil
}

func (d *rasterDecoder) sampleAt(x, y, sample int) (float64, bool, error) {
	vals, ok, err := d.sampleWindowAt(x, y)
	if err != nil || !ok {
		return 0, ok, err
	}
	if sample < 0 || sample >= len(vals) {
		return 0, false, fmt.Errorf("sample %d outside raster", sample)
	}
	return vals[sample], true, nil
}

func (d *rasterDecoder) samplesAt(x, y int) ([]float64, bool, error) {
	vals, ok, err := d.sampleWindowAt(x, y)
	if err != nil || !ok {
		return nil, ok, err
	}
	out := make([]float64, len(vals))
	copy(out, vals)
	return out, true, nil
}

func (d *rasterDecoder) sampleWindowAt(x, y int) ([]float64, bool, error) {
	blockIndex, localX, localY := d.blockForPixel(x, y)
	block, err := d.loadBlock(blockIndex)
	if err != nil {
		return nil, false, err
	}
	if localX < 0 || localY < 0 || localX >= block.width || localY >= block.height {
		return nil, false, nil
	}
	outCount := d.samples
	if outCount <= 0 || outCount > block.samples {
		outCount = block.samples
	}
	start := ((localY * block.width) + localX) * block.samples
	end := start + outCount
	if start < 0 || end > len(block.values) {
		return nil, false, fmt.Errorf("decoded TIFF block sample window outside block")
	}
	return block.values[start:end], true, nil
}

func (d *rasterDecoder) blockForPixel(x, y int) (int, int, int) {
	if d.tiled {
		tileX := x / d.tileWidth
		tileY := y / d.tileHeight
		return tileY*d.tilesAcross + tileX, x - tileX*d.tileWidth, y - tileY*d.tileHeight
	}
	strip := y / d.rowsPerStrip
	return strip, x, y - strip*d.rowsPerStrip
}

func (d *rasterDecoder) loadBlock(blockIndex int) (*rasterBlock, error) {
	if block := d.cache.get(blockIndex); block != nil {
		return block, nil
	}
	flight, leader := d.startBlockFlight(blockIndex)
	if !leader {
		<-flight.done
		return flight.block, flight.err
	}
	if block := d.cache.get(blockIndex); block != nil {
		d.finishBlockFlight(blockIndex, flight, block, nil)
		return block, nil
	}
	block, err := d.decodeSampleBlock(blockIndex)
	if err != nil {
		d.finishBlockFlight(blockIndex, flight, nil, err)
		return nil, err
	}
	block = d.cache.add(blockIndex, block)
	d.finishBlockFlight(blockIndex, flight, block, nil)
	return block, nil
}

func (d *rasterDecoder) loadColorBlock(blockIndex int) (*colorBlock, error) {
	if block := d.colorCache.get(blockIndex); block != nil {
		return block, nil
	}
	flight, leader := d.startColorBlockFlight(blockIndex)
	if !leader {
		<-flight.done
		return flight.block, flight.err
	}
	if block := d.colorCache.get(blockIndex); block != nil {
		d.finishColorBlockFlight(blockIndex, flight, block, nil)
		return block, nil
	}
	block, err := d.decodeColorBlock(blockIndex)
	if err != nil {
		d.finishColorBlockFlight(blockIndex, flight, nil, err)
		return nil, err
	}
	block = d.colorCache.add(blockIndex, block)
	d.finishColorBlockFlight(blockIndex, flight, block, nil)
	return block, nil
}

func lercAddCompressionFromParams(params []uint64) uint64 {
	if len(params) > 1 {
		return params[1]
	}
	return lercAddCompressionNone
}

func (d *rasterDecoder) decodeColorBlock(blockIndex int) (*colorBlock, error) {
	if d.compression == tiffCompressionLERC {
		block, err := d.decodeLERCBlock(blockIndex)
		if err != nil {
			return nil, err
		}
		return d.sampleBlockToColorBlock(block), nil
	}
	if d.compression == tiffCompressionJPEG {
		return d.decodeJPEGColorBlock(blockIndex)
	}
	if d.compression == tiffCompressionWebP {
		return d.decodeWebPColorBlock(blockIndex)
	}
	if d.photometric == tiffPhotometricYCbCr {
		return d.decodeYCbCrColorBlock(blockIndex)
	}
	if d.planarConfig == tiffPlanarSeparate {
		block, err := d.decodeSeparateSampleBlock(blockIndex)
		if err != nil {
			return nil, err
		}
		return d.sampleBlockToColorBlock(block), nil
	}
	raw, blockWidth, blockHeight, err := d.decodeRawBlock(0, blockIndex)
	if err != nil {
		return nil, err
	}
	rowBytes, err := d.rowBytes(blockWidth, 0, d.samples)
	if err != nil {
		return nil, err
	}
	sampleBytes := int(d.bits[0] / 8)
	if err := applyHorizontalPredictor(raw, blockHeight, rowBytes, d.samples, sampleBytes, d.predictor, d.ifd.order, d.formats[0]); err != nil {
		return nil, err
	}
	return d.decodeInterleavedColorBlock(raw, blockWidth, blockHeight, rowBytes)
}

func (d *rasterDecoder) decodeSampleBlock(blockIndex int) (*rasterBlock, error) {
	if d.compression == tiffCompressionLERC {
		return d.decodeLERCBlock(blockIndex)
	}
	if d.compression == tiffCompressionJPEG {
		return d.decodeJPEGBlock(blockIndex)
	}
	if d.compression == tiffCompressionWebP {
		return d.decodeWebPBlock(blockIndex)
	}
	if d.photometric == tiffPhotometricYCbCr {
		return d.decodeYCbCrBlock(blockIndex)
	}
	if d.planarConfig == tiffPlanarSeparate {
		return d.decodeSeparateSampleBlock(blockIndex)
	}
	raw, blockWidth, blockHeight, err := d.decodeRawBlock(0, blockIndex)
	if err != nil {
		return nil, err
	}
	rowBytes, err := d.rowBytes(blockWidth, 0, d.samples)
	if err != nil {
		return nil, err
	}
	sampleBytes := int(d.bits[0] / 8)
	if err := applyHorizontalPredictor(raw, blockHeight, rowBytes, d.samples, sampleBytes, d.predictor, d.ifd.order, d.formats[0]); err != nil {
		return nil, err
	}
	return d.decodeInterleaved(raw, blockWidth, blockHeight, rowBytes)
}

func (d *rasterDecoder) decodeSeparateSampleBlock(blockIndex int) (*rasterBlock, error) {
	blockWidth, blockHeight := d.blockSize(blockIndex)
	block := &rasterBlock{
		width:   blockWidth,
		height:  blockHeight,
		samples: d.samples,
		values:  make([]float64, blockWidth*blockHeight*d.samples),
	}
	for plane := 0; plane < d.samples; plane++ {
		raw, _, _, err := d.decodeRawBlock(plane, blockIndex)
		if err != nil {
			return nil, err
		}
		rowBytes, err := d.rowBytes(blockWidth, plane, 1)
		if err != nil {
			return nil, err
		}
		sampleBytes := int(d.bits[plane] / 8)
		if err := applyHorizontalPredictor(raw, blockHeight, rowBytes, 1, sampleBytes, d.predictor, d.ifd.order, d.formats[plane]); err != nil {
			return nil, err
		}
		if err := d.decodeSeparatePlane(block, raw, blockWidth, blockHeight, rowBytes, plane); err != nil {
			return nil, err
		}
	}
	return block, nil
}

func (d *rasterDecoder) decodeYCbCrBlock(blockIndex int) (*rasterBlock, error) {
	raw, blockWidth, blockHeight, err := d.decodeRawBlock(0, blockIndex)
	if err != nil {
		return nil, err
	}
	hSub, vSub := d.yCbCrSubsampling()
	unitsAcross := ceilDiv(blockWidth, hSub)
	unitsDown := ceilDiv(blockHeight, vSub)
	unitYCount := hSub * vSub
	block := &rasterBlock{
		width:   blockWidth,
		height:  blockHeight,
		samples: 3,
		values:  make([]float64, blockWidth*blockHeight*3),
	}
	pos := 0
	for uy := 0; uy < unitsDown; uy++ {
		for ux := 0; ux < unitsAcross; ux++ {
			if pos+unitYCount+2 > len(raw) {
				return nil, fmt.Errorf("short YCbCr TIFF data unit")
			}
			yVals := raw[pos : pos+unitYCount]
			cb := raw[pos+unitYCount]
			cr := raw[pos+unitYCount+1]
			pos += unitYCount + 2
			for dy := 0; dy < vSub; dy++ {
				for dx := 0; dx < hSub; dx++ {
					x := ux*hSub + dx
					y := uy*vSub + dy
					if x >= blockWidth || y >= blockHeight {
						continue
					}
					r, g, b := d.yCbCrToRGB(yVals[dy*hSub+dx], cb, cr)
					block.set(x, y, 0, r)
					block.set(x, y, 1, g)
					block.set(x, y, 2, b)
				}
			}
		}
	}
	return block, nil
}

func (d *rasterDecoder) decodeYCbCrColorBlock(blockIndex int) (*colorBlock, error) {
	raw, blockWidth, blockHeight, err := d.decodeRawBlock(0, blockIndex)
	if err != nil {
		return nil, err
	}
	hSub, vSub := d.yCbCrSubsampling()
	unitsAcross := ceilDiv(blockWidth, hSub)
	unitsDown := ceilDiv(blockHeight, vSub)
	unitYCount := hSub * vSub
	block := &colorBlock{
		width:  blockWidth,
		height: blockHeight,
		values: make([]Color, blockWidth*blockHeight),
	}
	pos := 0
	for uy := 0; uy < unitsDown; uy++ {
		for ux := 0; ux < unitsAcross; ux++ {
			if pos+unitYCount+2 > len(raw) {
				return nil, fmt.Errorf("short YCbCr TIFF data unit")
			}
			yVals := raw[pos : pos+unitYCount]
			cb := raw[pos+unitYCount]
			cr := raw[pos+unitYCount+1]
			pos += unitYCount + 2
			for dy := 0; dy < vSub; dy++ {
				for dx := 0; dx < hSub; dx++ {
					x := ux*hSub + dx
					y := uy*vSub + dy
					if x >= blockWidth || y >= blockHeight {
						continue
					}
					r, g, b := d.yCbCrToRGB(yVals[dy*hSub+dx], cb, cr)
					block.values[(y*blockWidth)+x] = Color{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
				}
			}
		}
	}
	return block, nil
}

func (d *rasterDecoder) decodeRawBlock(plane, blockIndex int) ([]byte, int, int, error) {
	blockWidth, blockHeight := d.blockSize(blockIndex)
	globalIndex := blockIndex
	if d.planarConfig == tiffPlanarSeparate {
		globalIndex = plane*d.blocksPerPlane + blockIndex
	}
	if globalIndex < 0 || globalIndex >= len(d.offsets) {
		return nil, 0, 0, fmt.Errorf("TIFF block index outside layout")
	}
	rowBytes, err := d.rowBytes(blockWidth, plane, d.samples)
	if err != nil {
		return nil, 0, 0, err
	}
	decodedSize := rowBytes * blockHeight
	if d.photometric == tiffPhotometricYCbCr && d.compression != tiffCompressionJPEG && d.planarConfig == tiffPlanarChunky {
		decodedSize = d.yCbCrBlockBytes(blockWidth, blockHeight)
	}
	if d.offsets[globalIndex] == 0 || d.counts[globalIndex] == 0 {
		return d.blankRawBlock(decodedSize, blockWidth, blockHeight, plane), blockWidth, blockHeight, nil
	}
	offset, count := d.offsets[globalIndex], d.counts[globalIndex]
	if offset > d.ifd.source.sizeUint64() || count > d.ifd.source.sizeUint64()-offset {
		return nil, 0, 0, fmt.Errorf("TIFF block offset outside file")
	}
	raw, err := d.ifd.source.readAt(offset, count)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read TIFF block: %w", err)
	}
	raw, err = decompressTIFF(raw, d.compression, decodedSize)
	if err != nil {
		return nil, 0, 0, err
	}
	if d.fillOrder == 2 {
		// LSB-first fill order: reverse the bits of every decoded byte, the
		// same post-decode step libtiff applies.
		for i, b := range raw {
			raw[i] = mathbits.Reverse8(b)
		}
	}
	if len(raw) < decodedSize {
		// Some writers encode only the valid leading rows of edge blocks.
		// libtiff and GDAL zero-fill the missing tail instead of failing.
		padded := make([]byte, decodedSize)
		copy(padded, raw)
		return padded, blockWidth, blockHeight, nil
	}
	return append([]byte(nil), raw[:decodedSize]...), blockWidth, blockHeight, nil
}

func (d *rasterDecoder) blankRawBlock(decodedSize, blockWidth, blockHeight, plane int) []byte {
	raw := make([]byte, decodedSize)
	noData := d.noData
	if len(noData) == 0 {
		return raw
	}
	if d.photometric == tiffPhotometricYCbCr && d.compression != tiffCompressionJPEG && d.planarConfig == tiffPlanarChunky {
		return raw
	}
	if d.planarConfig == tiffPlanarSeparate {
		value := noDataForBand(noData, plane)
		if value == nil {
			return raw
		}
		rowBytes, err := d.rowBytes(blockWidth, plane, 1)
		if err != nil {
			return raw
		}
		for y := 0; y < blockHeight; y++ {
			row := raw[y*rowBytes : (y+1)*rowBytes]
			for x := 0; x < blockWidth; x++ {
				d.writeSampleValue(row, x*int(d.bits[plane]), plane, *value)
			}
		}
		return raw
	}
	rowBytes, err := d.rowBytes(blockWidth, 0, d.samples)
	if err != nil {
		return raw
	}
	for y := 0; y < blockHeight; y++ {
		row := raw[y*rowBytes : (y+1)*rowBytes]
		bitOffset := 0
		for x := 0; x < blockWidth; x++ {
			for s := 0; s < d.samples; s++ {
				if value := noDataForBand(noData, s); value != nil {
					d.writeSampleValue(row, bitOffset, s, *value)
				}
				bitOffset += int(d.bits[s])
			}
		}
	}
	return raw
}

func (d *rasterDecoder) decodeJPEGBlock(blockIndex int) (*rasterBlock, error) {
	raw, err := d.rawCompressedImageBlock(blockIndex, "JPEG")
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return d.blankBlock(blockIndex), nil
	}
	raw, err = mergeJPEGTables(d.jpegTables, raw)
	if err != nil {
		return nil, err
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode TIFF JPEG block: %w", err)
	}
	return d.imageToBlock(blockIndex, img), nil
}

func (d *rasterDecoder) decodeJPEGColorBlock(blockIndex int) (*colorBlock, error) {
	raw, err := d.rawCompressedImageBlock(blockIndex, "JPEG")
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return d.blankColorBlock(blockIndex), nil
	}
	raw, err = mergeJPEGTables(d.jpegTables, raw)
	if err != nil {
		return nil, err
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode TIFF JPEG block: %w", err)
	}
	return d.imageToColorBlock(blockIndex, img), nil
}

func (d *rasterDecoder) decodeWebPBlock(blockIndex int) (*rasterBlock, error) {
	raw, err := d.rawCompressedImageBlock(blockIndex, "WebP")
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return d.blankBlock(blockIndex), nil
	}
	img, err := webp.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode TIFF WebP block: %w", err)
	}
	return d.imageToBlock(blockIndex, img), nil
}

func (d *rasterDecoder) decodeWebPColorBlock(blockIndex int) (*colorBlock, error) {
	raw, err := d.rawCompressedImageBlock(blockIndex, "WebP")
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return d.blankColorBlock(blockIndex), nil
	}
	img, err := webp.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode TIFF WebP block: %w", err)
	}
	return d.imageToColorBlock(blockIndex, img), nil
}

func (d *rasterDecoder) rawCompressedImageBlock(blockIndex int, label string) ([]byte, error) {
	offset, count := d.offsets[blockIndex], d.counts[blockIndex]
	if offset == 0 || count == 0 {
		return nil, nil
	}
	if offset > d.ifd.source.sizeUint64() || count > d.ifd.source.sizeUint64()-offset {
		return nil, fmt.Errorf("%s TIFF block offset outside file", label)
	}
	raw, err := d.ifd.source.readAt(offset, count)
	if err != nil {
		return nil, fmt.Errorf("read %s TIFF block: %w", label, err)
	}
	return raw, nil
}

func (d *rasterDecoder) blankBlock(blockIndex int) *rasterBlock {
	blockWidth, blockHeight := d.blockSize(blockIndex)
	block := &rasterBlock{
		width:   blockWidth,
		height:  blockHeight,
		samples: d.samples,
		values:  make([]float64, blockWidth*blockHeight*d.samples),
	}
	for y := 0; y < blockHeight; y++ {
		for x := 0; x < blockWidth; x++ {
			if d.samples > 3 && d.isAlphaPlane(3) {
				block.set(x, y, 3, 255)
			}
		}
	}
	return block
}

func (d *rasterDecoder) blankColorBlock(blockIndex int) *colorBlock {
	blockWidth, blockHeight := d.blockSize(blockIndex)
	block := &colorBlock{
		width:  blockWidth,
		height: blockHeight,
		values: make([]Color, blockWidth*blockHeight),
	}
	alpha := uint8(255)
	for i := range block.values {
		block.values[i].A = alpha
	}
	return block
}

func (d *rasterDecoder) imageToBlock(blockIndex int, img image.Image) *rasterBlock {
	bounds := img.Bounds()
	blockWidth, blockHeight := d.blockSize(blockIndex)
	block := &rasterBlock{
		width:   blockWidth,
		height:  blockHeight,
		samples: maxInt(d.samples, 3),
		values:  make([]float64, blockWidth*blockHeight*maxInt(d.samples, 3)),
	}
	maxY := minInt(blockHeight, bounds.Dy())
	maxX := minInt(blockWidth, bounds.Dx())
	for y := 0; y < maxY; y++ {
		for x := 0; x < maxX; x++ {
			r8, g8, b8, a8 := unpremultiplyRGBA16(img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA())
			block.set(x, y, 0, float64(r8))
			block.set(x, y, 1, float64(g8))
			block.set(x, y, 2, float64(b8))
			if block.samples > 3 {
				block.set(x, y, 3, float64(a8))
			}
		}
	}
	return block
}

func (d *rasterDecoder) imageToColorBlock(blockIndex int, img image.Image) *colorBlock {
	bounds := img.Bounds()
	blockWidth, blockHeight := d.blockSize(blockIndex)
	block := &colorBlock{
		width:  blockWidth,
		height: blockHeight,
		values: make([]Color, blockWidth*blockHeight),
	}
	maxY := minInt(blockHeight, bounds.Dy())
	maxX := minInt(blockWidth, bounds.Dx())
	for y := 0; y < maxY; y++ {
		for x := 0; x < maxX; x++ {
			r8, g8, b8, a8 := unpremultiplyRGBA16(img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA())
			block.values[(y*blockWidth)+x] = Color{R: r8, G: g8, B: b8, A: a8}
		}
	}
	return block
}

func mergeJPEGTables(tables, raw []byte) ([]byte, error) {
	if len(tables) == 0 {
		if len(raw) >= 2 && raw[0] == 0xff && raw[1] == 0xd8 {
			if jpegHasEOI(raw) {
				return raw, nil
			}
			out := append([]byte(nil), raw...)
			out = append(out, 0xff, 0xd9)
			return out, nil
		}
		return nil, fmt.Errorf("JPEG TIFF block has no SOI marker and JPEGTables is absent")
	}
	// TIFF strips/tiles are abbreviated JPEG streams that may start with their
	// own SOI marker yet still rely on JPEGTables for quantization/Huffman
	// tables, so the shared tables are always injected first. Table segments
	// repeated by the strip legally override the shared ones.
	out := make([]byte, 0, len(tables)+len(raw)+4)
	out = append(out, 0xff, 0xd8)
	out = append(out, jpegTablesPayload(tables)...)
	out = append(out, jpegScanPayload(raw)...)
	if !jpegHasEOI(raw) {
		out = append(out, 0xff, 0xd9)
	}
	return out, nil
}

func jpegHasEOI(data []byte) bool {
	return len(data) >= 2 && data[len(data)-2] == 0xff && data[len(data)-1] == 0xd9
}

func jpegTablesPayload(tables []byte) []byte {
	if len(tables) >= 2 && tables[0] == 0xff && tables[1] == 0xd8 {
		tables = tables[2:]
	}
	if jpegHasEOI(tables) {
		tables = tables[:len(tables)-2]
	}
	return tables
}

func jpegScanPayload(raw []byte) []byte {
	if len(raw) >= 2 && raw[0] == 0xff && raw[1] == 0xd8 {
		raw = raw[2:]
	}
	return raw
}

func (d *rasterDecoder) blockSize(blockIndex int) (int, int) {
	if d.tiled {
		return d.tileWidth, d.tileHeight
	}
	start := blockIndex * d.rowsPerStrip
	return d.width, minInt(d.rowsPerStrip, d.height-start)
}

func (d *rasterDecoder) rowBytes(width, plane, samples int) (int, error) {
	if d.planarConfig == tiffPlanarSeparate {
		return ceilDiv(width*int(d.bits[plane]), 8), nil
	}
	totalBits := 0
	for s := 0; s < samples; s++ {
		totalBits += int(d.bits[s])
	}
	return ceilDiv(totalBits*width, 8), nil
}

func (d *rasterDecoder) decodeInterleaved(raw []byte, width, height, rowBytes int) (*rasterBlock, error) {
	block := &rasterBlock{
		width:   width,
		height:  height,
		samples: d.samples,
		values:  make([]float64, width*height*d.samples),
	}
	for y := 0; y < height; y++ {
		row := raw[y*rowBytes : (y+1)*rowBytes]
		bitOffset := 0
		for x := 0; x < width; x++ {
			for s := 0; s < d.samples; s++ {
				v, err := d.readSampleValue(row, bitOffset, s)
				if err != nil {
					return nil, err
				}
				block.set(x, y, s, v)
				bitOffset += int(d.bits[s])
			}
		}
	}
	return block, nil
}

func (d *rasterDecoder) decodeInterleavedColorBlock(raw []byte, width, height, rowBytes int) (*colorBlock, error) {
	block := &colorBlock{
		width:  width,
		height: height,
		values: make([]Color, width*height),
	}
	colorSamples := d.colorSampleCount()
	for y := 0; y < height; y++ {
		row := raw[y*rowBytes : (y+1)*rowBytes]
		bitOffset := 0
		for x := 0; x < width; x++ {
			var samples [4]float64
			for s := 0; s < d.samples; s++ {
				if s < colorSamples {
					v, err := d.readSampleValue(row, bitOffset, s)
					if err != nil {
						return nil, err
					}
					samples[s] = v
				}
				bitOffset += int(d.bits[s])
			}
			block.values[(y*width)+x] = d.samplesToColor(samples[:colorSamples])
		}
	}
	return block, nil
}

func (d *rasterDecoder) decodeSeparatePlane(block *rasterBlock, raw []byte, width, height, rowBytes, plane int) error {
	for y := 0; y < height; y++ {
		row := raw[y*rowBytes : (y+1)*rowBytes]
		bitOffset := 0
		for x := 0; x < width; x++ {
			v, err := d.readSampleValue(row, bitOffset, plane)
			if err != nil {
				return err
			}
			block.set(x, y, plane, v)
			bitOffset += int(d.bits[plane])
		}
	}
	return nil
}

func (d *rasterDecoder) sampleBlockToColorBlock(sampleBlock *rasterBlock) *colorBlock {
	block := &colorBlock{
		width:  sampleBlock.width,
		height: sampleBlock.height,
		values: make([]Color, sampleBlock.width*sampleBlock.height),
	}
	for y := 0; y < sampleBlock.height; y++ {
		for x := 0; x < sampleBlock.width; x++ {
			start := ((y * sampleBlock.width) + x) * sampleBlock.samples
			outCount := d.samples
			if outCount <= 0 || outCount > sampleBlock.samples {
				outCount = sampleBlock.samples
			}
			block.values[(y*sampleBlock.width)+x] = d.samplesToColor(sampleBlock.values[start : start+outCount])
		}
	}
	return block
}

func (d *rasterDecoder) canRenderColor() bool {
	if d.compression == tiffCompressionJPEG || d.compression == tiffCompressionWebP {
		return true
	}
	switch d.photometric {
	case tiffPhotometricRGB, tiffPhotometricYCbCr:
		if d.samples < 3 {
			return false
		}
		for s := 0; s < minInt(4, d.samples); s++ {
			if d.formats[s] != tiffSampleFormatUnsigned || d.bits[s] > 16 {
				return false
			}
		}
		return true
	case tiffPhotometricWhiteIsZero, tiffPhotometricBlackIsZero:
		return d.samples >= 1 && d.formats[0] == tiffSampleFormatUnsigned && d.bits[0] <= 16
	case tiffPhotometricPalette:
		return d.samples >= 1 && d.formats[0] == tiffSampleFormatUnsigned && len(d.colorMap) > 0
	default:
		return false
	}
}

func (d *rasterDecoder) yCbCrSubsampling() (int, int) {
	if len(d.yCbCrSubsample) >= 2 {
		return int(d.yCbCrSubsample[0]), int(d.yCbCrSubsample[1])
	}
	return 2, 2
}

func (d *rasterDecoder) yCbCrBlockBytes(width, height int) int {
	hSub, vSub := d.yCbCrSubsampling()
	return ceilDiv(width, hSub) * ceilDiv(height, vSub) * (hSub*vSub + 2)
}

func (d *rasterDecoder) yCbCrToRGB(yCode, cbCode, crCode byte) (float64, float64, float64) {
	coeff := d.yCbCrCoeff
	if len(coeff) < 3 {
		coeff = []float64{0.299, 0.587, 0.114}
	}
	ref := d.referenceBW
	if len(ref) < 6 {
		ref = []float64{0, 255, 128, 255, 128, 255}
	}
	y := expandReference(float64(yCode), ref[0], ref[1], 255)
	cb := expandReference(float64(cbCode), ref[2], ref[3], 127)
	cr := expandReference(float64(crCode), ref[4], ref[5], 127)
	lumaRed, lumaGreen, lumaBlue := coeff[0], coeff[1], coeff[2]
	r := cr*(2-2*lumaRed) + y
	b := cb*(2-2*lumaBlue) + y
	g := y
	if lumaGreen != 0 {
		g = (y - lumaBlue*b - lumaRed*r) / lumaGreen
	}
	return clampByteFloat(r), clampByteFloat(g), clampByteFloat(b)
}

func expandReference(code, black, white, scale float64) float64 {
	if white == black {
		return 0
	}
	return (code - black) * scale / (white - black)
}

func clampByteFloat(v float64) float64 {
	if v <= 0 || math.IsNaN(v) {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return math.Round(v)
}

func (d *rasterDecoder) colorSampleCount() int {
	switch d.photometric {
	case tiffPhotometricRGB, tiffPhotometricYCbCr:
		return minInt(4, d.samples)
	case tiffPhotometricWhiteIsZero, tiffPhotometricBlackIsZero:
		if d.samples > 1 && d.isAlphaPlane(1) {
			return 2
		}
		return 1
	case tiffPhotometricPalette:
		return 1
	default:
		return minInt(4, d.samples)
	}
}

func (d *rasterDecoder) samplesToColor(samples []float64) Color {
	switch d.photometric {
	case tiffPhotometricRGB, tiffPhotometricYCbCr:
		c := Color{R: d.sampleToByte(samples[0], 0), G: d.sampleToByte(samples[1], 1), B: d.sampleToByte(samples[2], 2), A: 255}
		if len(samples) > 3 && d.isAlphaPlane(3) {
			c.A = d.sampleToByte(samples[3], 3)
			if d.alphaIsAssociated(3) {
				c = unpremultiplyColor(c)
			}
		}
		return c
	case tiffPhotometricWhiteIsZero:
		v := 255 - d.sampleToByte(samples[0], 0)
		c := Color{R: v, G: v, B: v, A: 255}
		if len(samples) > 1 && d.isAlphaPlane(1) {
			c.A = d.sampleToByte(samples[1], 1)
			if d.alphaIsAssociated(1) {
				c = unpremultiplyColor(c)
			}
		}
		return c
	case tiffPhotometricBlackIsZero:
		v := d.sampleToByte(samples[0], 0)
		c := Color{R: v, G: v, B: v, A: 255}
		if len(samples) > 1 && d.isAlphaPlane(1) {
			c.A = d.sampleToByte(samples[1], 1)
			if d.alphaIsAssociated(1) {
				c = unpremultiplyColor(c)
			}
		}
		return c
	case tiffPhotometricPalette:
		return d.paletteColor(uint64(math.Round(samples[0])))
	default:
		return Color{A: 255}
	}
}

func (d *rasterDecoder) extraSampleIndex(sample int) int {
	if d.photometric == tiffPhotometricRGB {
		return sample - 3
	}
	return sample - 1
}

func (d *rasterDecoder) isAlphaPlane(sample int) bool {
	extraIndex := d.extraSampleIndex(sample)
	if extraIndex < 0 {
		return false
	}
	if extraIndex >= len(d.extraSamples) {
		// Many real-world RGBA TIFFs omit ExtraSamples. Treat the first extra
		// channel as alpha for colorization, but keep unspecified later extras.
		return extraIndex == 0
	}
	return d.extraSamples[extraIndex] == 1 || d.extraSamples[extraIndex] == 2
}

// alphaIsAssociated reports whether the alpha sample is ExtraSamples=1,
// meaning color components are stored premultiplied (TIFF6 section 18).
func (d *rasterDecoder) alphaIsAssociated(sample int) bool {
	extraIndex := d.extraSampleIndex(sample)
	return extraIndex >= 0 && extraIndex < len(d.extraSamples) && d.extraSamples[extraIndex] == 1
}

// unpremultiplyColor converts an associated-alpha color to the unassociated
// convention used by Color.
func unpremultiplyColor(c Color) Color {
	if c.A == 255 {
		return c
	}
	if c.A == 0 {
		return Color{}
	}
	a := uint32(c.A)
	un := func(v uint8) uint8 {
		x := (uint32(v)*255 + a/2) / a
		if x > 255 {
			x = 255
		}
		return uint8(x)
	}
	return Color{R: un(c.R), G: un(c.G), B: un(c.B), A: c.A}
}

// unpremultiplyRGBA16 converts the alpha-premultiplied 16-bit channels
// returned by image.Color.RGBA into unassociated 8-bit channels.
func unpremultiplyRGBA16(r, g, b, a uint32) (uint8, uint8, uint8, uint8) {
	if a == 0 {
		return 0, 0, 0, 0
	}
	if a >= 0xffff {
		return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 255
	}
	un := func(v uint32) uint8 {
		x := (v*0xffff + a/2) / a
		if x > 0xffff {
			x = 0xffff
		}
		return uint8(x >> 8)
	}
	return un(r), un(g), un(b), uint8(a >> 8)
}

func (d *rasterDecoder) paletteColor(index uint64) Color {
	i := int(index)
	entries := len(d.colorMap) / 3
	if i >= entries {
		return Color{A: 255}
	}
	return Color{
		R: scale16To8(d.colorMap[i]),
		G: scale16To8(d.colorMap[entries+i]),
		B: scale16To8(d.colorMap[2*entries+i]),
		A: 255,
	}
}

func (d *rasterDecoder) sampleToByte(v float64, sample int) uint8 {
	if v <= 0 || math.IsNaN(v) {
		return 0
	}
	if d.formats[sample] == tiffSampleFormatUnsigned && d.bits[sample] > 8 {
		maxValue := math.Exp2(float64(d.bits[sample])) - 1
		if maxValue > 0 && v <= maxValue {
			v = v * 255 / maxValue
		}
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func (d *rasterDecoder) readSampleValue(row []byte, bitOffset int, sample int) (float64, error) {
	bits := int(d.bits[sample])
	format := d.formats[sample]
	if bits < 8 {
		if format != tiffSampleFormatUnsigned {
			return 0, fmt.Errorf("bit-packed signed/float TIFF samples are not supported")
		}
		return float64(readBitsMSB(row, bitOffset, bits)), nil
	}
	if bitOffset%8 != 0 {
		return 0, fmt.Errorf("byte-sized TIFF sample starts at bit offset %d", bitOffset)
	}
	byteOffset := bitOffset / 8
	n := bits / 8
	if byteOffset+n > len(row) {
		return 0, fmt.Errorf("short TIFF sample row")
	}
	raw := row[byteOffset : byteOffset+n]
	switch format {
	case tiffSampleFormatUnsigned:
		switch bits {
		case 8:
			return float64(raw[0]), nil
		case 16:
			return float64(d.ifd.order.Uint16(raw)), nil
		case 32:
			return float64(d.ifd.order.Uint32(raw)), nil
		case 64:
			return float64(d.ifd.order.Uint64(raw)), nil
		}
	case tiffSampleFormatSigned:
		switch bits {
		case 8:
			return float64(int8(raw[0])), nil
		case 16:
			return float64(int16(d.ifd.order.Uint16(raw))), nil
		case 32:
			return float64(int32(d.ifd.order.Uint32(raw))), nil
		case 64:
			return float64(int64(d.ifd.order.Uint64(raw))), nil
		}
	case tiffSampleFormatFloat:
		switch bits {
		case 16:
			return float16ToFloat64(d.ifd.order.Uint16(raw)), nil
		case 32:
			return float64(math.Float32frombits(d.ifd.order.Uint32(raw))), nil
		case 64:
			return math.Float64frombits(d.ifd.order.Uint64(raw)), nil
		}
	default:
		return 0, fmt.Errorf("unsupported TIFF SampleFormat %d", format)
	}
	return 0, fmt.Errorf("unsupported TIFF SampleFormat %d BitsPerSample %d", format, bits)
}

func (d *rasterDecoder) writeSampleValue(row []byte, bitOffset int, sample int, value float64) {
	bits := int(d.bits[sample])
	if bits < 8 {
		writeBitsMSB(row, bitOffset, bits, uint64(value))
		return
	}
	if bitOffset%8 != 0 {
		return
	}
	byteOffset := bitOffset / 8
	n := bits / 8
	if byteOffset+n > len(row) {
		return
	}
	raw := row[byteOffset : byteOffset+n]
	switch d.formats[sample] {
	case tiffSampleFormatUnsigned:
		switch bits {
		case 8:
			raw[0] = byte(uint64(value))
		case 16:
			d.ifd.order.PutUint16(raw, uint16(value))
		case 32:
			d.ifd.order.PutUint32(raw, uint32(value))
		case 64:
			d.ifd.order.PutUint64(raw, uint64(value))
		}
	case tiffSampleFormatSigned:
		switch bits {
		case 8:
			raw[0] = byte(int8(value))
		case 16:
			d.ifd.order.PutUint16(raw, uint16(int16(value)))
		case 32:
			d.ifd.order.PutUint32(raw, uint32(int32(value)))
		case 64:
			d.ifd.order.PutUint64(raw, uint64(int64(value)))
		}
	case tiffSampleFormatFloat:
		switch bits {
		case 32:
			d.ifd.order.PutUint32(raw, math.Float32bits(float32(value)))
		case 64:
			d.ifd.order.PutUint64(raw, math.Float64bits(value))
		}
	}
}

func scale16To8(v uint64) uint8 {
	return uint8((v*255 + 32767) / 65535)
}

func applyHorizontalPredictor(raw []byte, rows, rowBytes, stride, sampleBytes int, predictor uint64, order binary.ByteOrder, sampleFormat uint64) error {
	if predictor == 1 {
		return nil
	}
	if sampleBytes <= 0 {
		return fmt.Errorf("horizontal predictor with bit-packed samples is not supported")
	}
	for r := 0; r < rows; r++ {
		row := raw[r*rowBytes : (r+1)*rowBytes]
		switch predictor {
		case 2:
			if err := applyIntegerPredictorRow(row, stride, sampleBytes, order); err != nil {
				return err
			}
		case 3:
			if sampleFormat != tiffSampleFormatFloat {
				return fmt.Errorf("TIFF Predictor 3 requires floating point samples")
			}
			if err := applyFloatingPointPredictorRow(row, stride, sampleBytes, order); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported TIFF predictor %d", predictor)
		}
	}
	return nil
}

func applyIntegerPredictorRow(row []byte, stride, sampleBytes int, order binary.ByteOrder) error {
	if len(row)%(stride*sampleBytes) != 0 {
		return fmt.Errorf("TIFF predictor row width is not aligned to sample stride")
	}
	switch sampleBytes {
	case 1:
		for i := stride; i < len(row); i++ {
			row[i] += row[i-stride]
		}
	case 2:
		n := len(row) / 2
		for i := stride; i < n; i++ {
			order.PutUint16(row[i*2:], order.Uint16(row[i*2:])+order.Uint16(row[(i-stride)*2:]))
		}
	case 4:
		n := len(row) / 4
		for i := stride; i < n; i++ {
			order.PutUint32(row[i*4:], order.Uint32(row[i*4:])+order.Uint32(row[(i-stride)*4:]))
		}
	case 8:
		n := len(row) / 8
		for i := stride; i < n; i++ {
			order.PutUint64(row[i*8:], order.Uint64(row[i*8:])+order.Uint64(row[(i-stride)*8:]))
		}
	default:
		return fmt.Errorf("unsupported predictor sample width %d", sampleBytes)
	}
	return nil
}

func applyFloatingPointPredictorRow(row []byte, stride, sampleBytes int, order binary.ByteOrder) error {
	if len(row)%(stride*sampleBytes) != 0 {
		return fmt.Errorf("TIFF floating point predictor row width is not aligned to sample stride")
	}
	for i := stride; i < len(row); i++ {
		row[i] += row[i-stride]
	}
	tmp := append([]byte(nil), row...)
	sampleCount := len(row) / sampleBytes
	for sample := 0; sample < sampleCount; sample++ {
		for b := 0; b < sampleBytes; b++ {
			srcByte := b
			if order == binary.LittleEndian {
				srcByte = sampleBytes - b - 1
			}
			row[sample*sampleBytes+b] = tmp[srcByte*sampleCount+sample]
		}
	}
	return nil
}

// xzMagic is the XZ container header magic used by libtiff's LZMA codec.
var xzMagic = []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}

func supportedTIFFCompression(compression uint64) bool {
	switch compression {
	case tiffCompressionNone,
		tiffCompressionLZW,
		tiffCompressionJPEG,
		tiffCompressionDeflate,
		tiffCompressionPackBits,
		tiffCompressionOldDeflate,
		tiffCompressionLERC,
		tiffCompressionLZMA,
		tiffCompressionZSTD,
		tiffCompressionWebP:
		return true
	default:
		return false
	}
}

func decompressTIFF(raw []byte, compression uint64, maxDecoded int) ([]byte, error) {
	switch compression {
	case tiffCompressionNone:
		return raw, nil
	case tiffCompressionDeflate, tiffCompressionOldDeflate:
		return inflateZlib(raw, maxDecoded)
	case tiffCompressionLZW:
		r := tifflzw.NewReader(bytes.NewReader(raw), tifflzw.MSB, 8)
		defer r.Close()
		return readAllLimited(r, maxDecoded)
	case tiffCompressionPackBits:
		return unpackBits(raw, maxDecoded)
	case tiffCompressionLZMA:
		// libtiff writes XZ containers, typically with a Delta+LZMA2 filter
		// chain that only xi2/xz can decode. Raw LZMA payloads from other
		// writers fall through to the LZMA1/LZMA2 readers.
		if len(raw) >= len(xzMagic) && bytes.Equal(raw[:len(xzMagic)], xzMagic) {
			r, err := xi2xz.NewReader(bytes.NewReader(raw), 0)
			if err != nil {
				return nil, fmt.Errorf("decode TIFF LZMA xz container: %w", err)
			}
			return readAllLimited(r, maxDecoded)
		}
		dictCap := maxInt(maxDecoded, 1<<20)
		lzmaReader, lzmaErr := lzma.ReaderConfig{DictCap: dictCap}.NewReader(bytes.NewReader(raw))
		if lzmaErr == nil {
			out, err := readAllLimited(lzmaReader, maxDecoded)
			if err == nil {
				return out, nil
			}
			lzmaErr = err
		}
		lzma2Reader, lzma2Err := lzma.Reader2Config{DictCap: dictCap}.NewReader2(bytes.NewReader(raw))
		if lzma2Err == nil {
			out, err := readAllLimited(lzma2Reader, maxDecoded)
			if err == nil {
				return out, nil
			}
			lzma2Err = err
		}
		return nil, fmt.Errorf("decode TIFF LZMA: lzma=%v lzma2=%v", lzmaErr, lzma2Err)
	case tiffCompressionZSTD:
		r, err := zstd.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return readAllLimited(r, maxDecoded)
	default:
		return nil, fmt.Errorf("unsupported TIFF compression %d", compression)
	}
}

func inflateZlib(raw []byte, maxDecoded int) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return readAllLimited(r, maxDecoded)
}

func readAllLimited(r io.Reader, maxDecoded int) ([]byte, error) {
	if maxDecoded < 0 {
		return nil, fmt.Errorf("invalid decoded TIFF block size")
	}
	lr := &io.LimitedReader{R: r, N: int64(maxDecoded) + 1}
	out, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if len(out) > maxDecoded {
		return nil, fmt.Errorf("decoded TIFF block exceeds expected size %d", maxDecoded)
	}
	return out, nil
}

func unpackBits(raw []byte, maxDecoded int) ([]byte, error) {
	if maxDecoded < 0 {
		return nil, fmt.Errorf("invalid decoded TIFF block size")
	}
	capHint := len(raw) * 2
	if capHint > maxDecoded {
		capHint = maxDecoded
	}
	out := make([]byte, 0, capHint)
	for i := 0; i < len(raw); {
		n := int(int8(raw[i]))
		i++
		switch {
		case n >= 0:
			count := n + 1
			if i+count > len(raw) {
				return nil, fmt.Errorf("short TIFF PackBits literal")
			}
			out = append(out, raw[i:i+count]...)
			if len(out) > maxDecoded {
				return nil, fmt.Errorf("decoded TIFF block exceeds expected size %d", maxDecoded)
			}
			i += count
		case n >= -127:
			if i >= len(raw) {
				return nil, fmt.Errorf("short TIFF PackBits run")
			}
			count := 1 - n
			if len(out)+count > maxDecoded {
				return nil, fmt.Errorf("decoded TIFF block exceeds expected size %d", maxDecoded)
			}
			b := raw[i]
			for j := 0; j < count; j++ {
				out = append(out, b)
			}
			i++
		}
	}
	return out, nil
}

type rasterBlock struct {
	width   int
	height  int
	samples int
	values  []float64
}

func (b *rasterBlock) set(x, y, sample int, value float64) {
	b.values[((y*b.width)+x)*b.samples+sample] = value
}

type colorBlock struct {
	width  int
	height int
	values []Color
}

type blockCache struct {
	max     int
	mu      sync.Mutex
	list    *list.List
	entries map[int]*list.Element
}

type blockCacheEntry struct {
	key   int
	block *rasterBlock
}

type blockFlight struct {
	done  chan struct{}
	block *rasterBlock
	err   error
}

type colorBlockCache struct {
	max     int
	mu      sync.Mutex
	list    *list.List
	entries map[int]*list.Element
}

type colorBlockCacheEntry struct {
	key   int
	block *colorBlock
}

type colorBlockFlight struct {
	done  chan struct{}
	block *colorBlock
	err   error
}

func (d *rasterDecoder) startBlockFlight(key int) (*blockFlight, bool) {
	d.flightMu.Lock()
	defer d.flightMu.Unlock()
	if d.flights == nil {
		d.flights = make(map[int]*blockFlight)
	}
	if flight := d.flights[key]; flight != nil {
		return flight, false
	}
	flight := &blockFlight{done: make(chan struct{})}
	d.flights[key] = flight
	return flight, true
}

func (d *rasterDecoder) finishBlockFlight(key int, flight *blockFlight, block *rasterBlock, err error) {
	d.flightMu.Lock()
	if d.flights[key] == flight {
		delete(d.flights, key)
	}
	flight.block = block
	flight.err = err
	close(flight.done)
	d.flightMu.Unlock()
}

func (d *rasterDecoder) startColorBlockFlight(key int) (*colorBlockFlight, bool) {
	d.colorFlightMu.Lock()
	defer d.colorFlightMu.Unlock()
	if d.colorFlights == nil {
		d.colorFlights = make(map[int]*colorBlockFlight)
	}
	if flight := d.colorFlights[key]; flight != nil {
		return flight, false
	}
	flight := &colorBlockFlight{done: make(chan struct{})}
	d.colorFlights[key] = flight
	return flight, true
}

func (d *rasterDecoder) finishColorBlockFlight(key int, flight *colorBlockFlight, block *colorBlock, err error) {
	d.colorFlightMu.Lock()
	if d.colorFlights[key] == flight {
		delete(d.colorFlights, key)
	}
	flight.block = block
	flight.err = err
	close(flight.done)
	d.colorFlightMu.Unlock()
}

func newBlockCache(max int) *blockCache {
	c := &blockCache{
		max:     max,
		list:    list.New(),
		entries: make(map[int]*list.Element),
	}
	return c
}

func (c *blockCache) get(key int) *rasterBlock {
	if c == nil || c.max <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem := c.entries[key]; elem != nil {
		c.list.MoveToFront(elem)
		return elem.Value.(blockCacheEntry).block
	}
	return nil
}

func (c *blockCache) add(key int, block *rasterBlock) *rasterBlock {
	if c == nil || c.max <= 0 {
		return block
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem := c.entries[key]; elem != nil {
		c.list.MoveToFront(elem)
		return elem.Value.(blockCacheEntry).block
	}
	elem := c.list.PushFront(blockCacheEntry{key: key, block: block})
	c.entries[key] = elem
	for c.list.Len() > c.max {
		back := c.list.Back()
		if back == nil {
			break
		}
		c.list.Remove(back)
		delete(c.entries, back.Value.(blockCacheEntry).key)
	}
	return block
}

func newColorBlockCache(max int) *colorBlockCache {
	c := &colorBlockCache{
		max:     max,
		list:    list.New(),
		entries: make(map[int]*list.Element),
	}
	return c
}

func (c *colorBlockCache) get(key int) *colorBlock {
	if c == nil || c.max <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem := c.entries[key]; elem != nil {
		c.list.MoveToFront(elem)
		return elem.Value.(colorBlockCacheEntry).block
	}
	return nil
}

func (c *colorBlockCache) add(key int, block *colorBlock) *colorBlock {
	if c == nil || c.max <= 0 {
		return block
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem := c.entries[key]; elem != nil {
		c.list.MoveToFront(elem)
		return elem.Value.(colorBlockCacheEntry).block
	}
	elem := c.list.PushFront(colorBlockCacheEntry{key: key, block: block})
	c.entries[key] = elem
	for c.list.Len() > c.max {
		back := c.list.Back()
		if back == nil {
			break
		}
		c.list.Remove(back)
		delete(c.entries, back.Value.(colorBlockCacheEntry).key)
	}
	return block
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxIntValue() int {
	return int(^uint(0) >> 1)
}

func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}

func readBitsMSB(data []byte, bitOffset, bits int) uint64 {
	var v uint64
	for i := 0; i < bits; i++ {
		pos := bitOffset + i
		byteIndex := pos / 8
		shift := 7 - (pos % 8)
		v = (v << 1) | uint64((data[byteIndex]>>shift)&1)
	}
	return v
}

func writeBitsMSB(data []byte, bitOffset, bits int, value uint64) {
	for i := 0; i < bits; i++ {
		pos := bitOffset + i
		byteIndex := pos / 8
		shift := 7 - (pos % 8)
		mask := byte(1 << shift)
		if (value & (1 << uint(bits-i-1))) != 0 {
			data[byteIndex] |= mask
		} else {
			data[byteIndex] &^= mask
		}
	}
}

func float16ToFloat64(bits uint16) float64 {
	sign := uint64(bits>>15) & 0x1
	exp := int((bits >> 10) & 0x1f)
	frac := uint64(bits & 0x03ff)
	var out uint64
	switch exp {
	case 0:
		if frac == 0 {
			out = sign << 63
			return math.Float64frombits(out)
		}
		e := -14
		for frac&0x0400 == 0 {
			frac <<= 1
			e--
		}
		frac &= 0x03ff
		out = (sign << 63) | (uint64(e+1023) << 52) | (frac << 42)
	case 31:
		out = (sign << 63) | (0x7ff << 52) | (frac << 42)
	default:
		out = (sign << 63) | (uint64(exp-15+1023) << 52) | (frac << 42)
	}
	return math.Float64frombits(out)
}

func buildBands(decoder *rasterDecoder, ifd *tiffIFD) []Band {
	noData := decoder.noData
	scales, offsets := parseGDALBandMetadata(ifd, decoder.samples)
	bands := make([]Band, decoder.samples)
	for i := range bands {
		scale := 1.0
		if i < len(scales) && scales[i] != 0 {
			scale = scales[i]
		}
		offset := 0.0
		if i < len(offsets) {
			offset = offsets[i]
		}
		bands[i] = Band{
			Index:         i,
			BitsPerSample: int(decoder.bits[i]),
			SampleFormat:  SampleFormat(decoder.formats[i]),
			NoData:        noDataForBand(noData, i),
			Scale:         scale,
			Offset:        offset,
		}
	}
	return bands
}

func noDataForBand(values []*float64, band int) *float64 {
	if len(values) == 0 {
		return nil
	}
	if band < len(values) {
		return values[band]
	}
	return values[len(values)-1]
}

func sampleEqualsNoData(v, noData float64) bool {
	if math.IsNaN(noData) {
		return math.IsNaN(v)
	}
	return v == noData
}

func parseGDALNoData(ifd *tiffIFD) []*float64 {
	raw, ok := ifd.raw(tiffTagGDALNodata)
	if !ok {
		return nil
	}
	text := strings.Trim(strings.TrimRight(string(raw), "\x00"), " \t\r\n")
	if text == "" {
		return nil
	}
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ',' || r == ';' || r == '|'
	})
	out := make([]*float64, 0, len(parts))
	for _, part := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			continue
		}
		value := v
		out = append(out, &value)
	}
	return out
}

type gdalMetadataXML struct {
	Items []gdalMetadataItem `xml:"Item"`
}

type gdalMetadataItem struct {
	Name   string `xml:"name,attr"`
	Sample int    `xml:"sample,attr"`
	Value  string `xml:",chardata"`
}

func parseGDALBandMetadata(ifd *tiffIFD, bands int) ([]float64, []float64) {
	raw, ok := ifd.raw(tiffTagGDALMetadata)
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	text := strings.TrimRight(string(raw), "\x00")
	var doc gdalMetadataXML
	if err := xml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, nil
	}
	scales := make([]float64, bands)
	offsets := make([]float64, bands)
	for _, item := range doc.Items {
		if item.Sample < 0 || item.Sample >= bands {
			continue
		}
		name := strings.ToUpper(strings.TrimSpace(item.Name))
		v, err := strconv.ParseFloat(strings.TrimSpace(item.Value), 64)
		if err != nil {
			continue
		}
		switch name {
		case "SCALE", "SCALE_FACTOR":
			scales[item.Sample] = v
		case "OFFSET", "ADD_OFFSET":
			offsets[item.Sample] = v
		}
	}
	return scales, offsets
}
