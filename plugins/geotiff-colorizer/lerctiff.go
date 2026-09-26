package geotiffcolorizer

// LERC-in-TIFF integration (Compression=34887), mirroring libtiff's
// tif_lerc.c: optional additional deflate/zstd wrapping declared by the
// LERC_PARAMETERS tag, per-block Lerc2 blobs, alpha reconstruction from the
// validity mask for byte RGBA, and NaN filling for masked float pixels.

import (
	"fmt"
	"math"
)

const (
	lercAddCompressionNone    = 0
	lercAddCompressionDeflate = 1
	lercAddCompressionZstd    = 2
)

func (d *rasterDecoder) decodeLERCBlock(blockIndex int) (*rasterBlock, error) {
	blockWidth, blockHeight := d.blockSize(blockIndex)
	block := &rasterBlock{
		width:   blockWidth,
		height:  blockHeight,
		samples: d.samples,
		values:  make([]float64, blockWidth*blockHeight*d.samples),
	}
	if d.planarConfig == tiffPlanarSeparate {
		for plane := 0; plane < d.samples; plane++ {
			raster, err := d.lercRasterForBlock(plane, blockIndex, blockWidth, blockHeight)
			if err != nil {
				return nil, err
			}
			if raster == nil { // sparse block
				d.fillLERCBlankPlane(block, plane)
				continue
			}
			if raster.nDepth != 1 || len(raster.bands) != 1 {
				return nil, fmt.Errorf("LERC plane blob has depth %d bands %d, want 1x1", raster.nDepth, len(raster.bands))
			}
			d.fillLERCPlane(block, raster.bands[0], raster.masks[0], plane, 1, 0)
		}
		return block, nil
	}

	raster, err := d.lercRasterForBlock(0, blockIndex, blockWidth, blockHeight)
	if err != nil {
		return nil, err
	}
	if raster == nil { // sparse block
		for plane := 0; plane < d.samples; plane++ {
			d.fillLERCBlankPlane(block, plane)
		}
		return block, nil
	}

	switch {
	case raster.nDepth == d.samples && len(raster.bands) == 1:
		for s := 0; s < d.samples; s++ {
			d.fillLERCPlane(block, raster.bands[0], raster.masks[0], s, raster.nDepth, s)
		}
	case d.lercAlphaFromMask() && raster.nDepth == d.samples-1 && len(raster.bands) == 1:
		// The alpha band was not stored; rebuild it from the validity mask.
		for s := 0; s < d.samples-1; s++ {
			d.fillLERCPlane(block, raster.bands[0], raster.masks[0], s, raster.nDepth, s)
		}
		alpha := d.samples - 1
		mask := raster.masks[0]
		for k := 0; k < blockWidth*blockHeight; k++ {
			v := 255.0
			if mask != nil && !mask.isValid(k) {
				v = 0
			}
			block.values[k*d.samples+alpha] = v
		}
	case raster.nDepth == 1 && len(raster.bands) == d.samples:
		for s := 0; s < d.samples; s++ {
			d.fillLERCPlane(block, raster.bands[s], raster.masks[s], s, 1, 0)
		}
	default:
		return nil, fmt.Errorf("LERC blob with depth %d and %d bands does not match %d samples",
			raster.nDepth, len(raster.bands), d.samples)
	}
	return block, nil
}

// fillLERCPlane copies one depth slice (or single-depth band) of LERC values
// into sample plane s of the block, applying NaN to masked float pixels.
func (d *rasterDecoder) fillLERCPlane(block *rasterBlock, values []float64, mask *lercBitMask, s, nDepth, depthIndex int) {
	isFloat := d.formats[s] == tiffSampleFormatFloat
	n := block.width * block.height
	for k := 0; k < n; k++ {
		v := values[k*nDepth+depthIndex]
		if isFloat && mask != nil && !mask.isValid(k) {
			v = math.NaN()
		}
		block.values[k*block.samples+s] = v
	}
}

func (d *rasterDecoder) fillLERCBlankPlane(block *rasterBlock, plane int) {
	var fill float64
	if value := noDataForBand(d.noData, plane); value != nil {
		fill = *value
	} else if plane == 3 && d.samples > 3 && d.isAlphaPlane(3) {
		fill = 255
	}
	if fill == 0 {
		return
	}
	n := block.width * block.height
	for k := 0; k < n; k++ {
		block.values[k*block.samples+plane] = fill
	}
}

// lercAlphaFromMask reports whether the last sample is an unassociated alpha
// channel that libtiff strips before LERC encoding for byte data.
func (d *rasterDecoder) lercAlphaFromMask() bool {
	return d.planarConfig == tiffPlanarChunky &&
		len(d.extraSamples) > 0 &&
		d.extraSamples[len(d.extraSamples)-1] == 2 && // EXTRASAMPLE_UNASSALPHA
		d.formats[0] == tiffSampleFormatUnsigned &&
		d.bits[0] == 8
}

// lercRasterForBlock reads, unwraps and decodes the LERC blob for one block.
// A nil raster (no error) means the block is sparse.
func (d *rasterDecoder) lercRasterForBlock(plane, blockIndex, blockWidth, blockHeight int) (*lercRaster, error) {
	globalIndex := blockIndex
	if d.planarConfig == tiffPlanarSeparate {
		globalIndex = plane*d.blocksPerPlane + blockIndex
	}
	if globalIndex < 0 || globalIndex >= len(d.offsets) {
		return nil, fmt.Errorf("LERC block index outside layout")
	}
	offset, count := d.offsets[globalIndex], d.counts[globalIndex]
	if offset == 0 || count == 0 {
		return nil, nil
	}
	if offset > d.ifd.source.sizeUint64() || count > d.ifd.source.sizeUint64()-offset {
		return nil, fmt.Errorf("LERC block offset outside file")
	}
	blob, err := d.ifd.source.readAt(offset, count)
	if err != nil {
		return nil, fmt.Errorf("read LERC block: %w", err)
	}
	switch d.lercAddCompression {
	case lercAddCompressionNone:
	case lercAddCompressionDeflate:
		blob, err = inflateZlib(blob, d.lercBlobSizeLimit())
	case lercAddCompressionZstd:
		blob, err = decompressTIFF(blob, tiffCompressionZSTD, d.lercBlobSizeLimit())
	default:
		return nil, fmt.Errorf("unsupported LERC additional compression %d", d.lercAddCompression)
	}
	if err != nil {
		return nil, fmt.Errorf("unwrap LERC block: %w", err)
	}
	raster, err := lercDecode(blob)
	if err != nil {
		return nil, fmt.Errorf("decode LERC block: %w", err)
	}
	if raster.nCols != blockWidth || raster.nRows != blockHeight {
		return nil, fmt.Errorf("LERC block is %dx%d, want %dx%d", raster.nCols, raster.nRows, blockWidth, blockHeight)
	}
	return raster, nil
}

// lercBlobSizeLimit bounds the decoded size of the deflate/zstd wrapping
// around a LERC blob. The blob is never larger than the raw block data plus
// its own headers.
func (d *rasterDecoder) lercBlobSizeLimit() int {
	bytes := d.sampleBlockDecodedBytes() + 65536
	if bytes > int64(maxIntValue()) {
		return maxIntValue()
	}
	return int(bytes)
}
