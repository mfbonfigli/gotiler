package geotiffcolorizer

// Decoder for Esri LERC (Lerc2) raster blobs, ported from the LERC C++
// implementation (Apache-2.0). Decode only, Lerc2 versions 1-5 plus the
// non-FPL paths of version 6. Legacy Lerc1 (CntZImage) blobs and the
// version 6 lossless floating-point (FPL) path are rejected.

import (
	"fmt"
	"strings"
)

const (
	lercDTChar = iota
	lercDTByte
	lercDTShort
	lercDTUShort
	lercDTInt
	lercDTUInt
	lercDTFloat
	lercDTDouble
)

const (
	lercFileKey        = "Lerc2 "
	lercCurrentVersion = 6
)

type lercNumber interface {
	~int8 | ~uint8 | ~int16 | ~uint16 | ~int32 | ~uint32 | ~float32 | ~float64
}

type lercHeader struct {
	version           int
	checksum          uint32
	nRows             int
	nCols             int
	nDepth            int
	numValidPixel     int
	microBlockSize    int
	blobSize          int
	nBlobsMore        int
	bPassNoDataValues byte
	dt                int
	maxZError         float64
	zMin              float64
	zMax              float64
}

func (hd *lercHeader) tryHuffmanInt() bool {
	return hd.version >= 2 && (hd.dt == lercDTByte || hd.dt == lercDTChar) && hd.maxZError == 0.5
}

func (hd *lercHeader) tryHuffmanFlt() bool {
	return hd.version >= 6 && (hd.dt == lercDTFloat || hd.dt == lercDTDouble) && hd.maxZError == 0
}

// lercRaster is one decoded LERC blob sequence: one or more bands sharing
// the same shape, each with its own validity mask (nil means all valid).
type lercRaster struct {
	nCols  int
	nRows  int
	nDepth int
	dt     int
	bands  [][]float64 // len nCols*nRows*nDepth each, depth-interleaved
	masks  []*lercBitMask
}

func lercDecode(blob []byte) (*lercRaster, error) {
	if len(blob) >= 9 && strings.HasPrefix(string(blob[:9]), "CntZImage") {
		return nil, fmt.Errorf("legacy Lerc1 blobs are not supported")
	}
	r := &lercReader{buf: blob}
	out := &lercRaster{}
	var prevMask *lercBitMask
	for r.remaining() >= len(lercFileKey) && string(r.buf[r.pos:r.pos+len(lercFileKey)]) == lercFileKey {
		hd, values, mask, err := lercDecodeBand(r, prevMask)
		if err != nil {
			return nil, err
		}
		prevMask = mask
		if len(out.bands) == 0 {
			out.nCols, out.nRows, out.nDepth, out.dt = hd.nCols, hd.nRows, hd.nDepth, hd.dt
		} else if hd.nCols != out.nCols || hd.nRows != out.nRows || hd.nDepth != out.nDepth || hd.dt != out.dt {
			return nil, fmt.Errorf("LERC band %d shape differs from band 0", len(out.bands))
		}
		bandMask := mask
		if hd.numValidPixel == hd.nCols*hd.nRows {
			bandMask = nil // all valid
		}
		out.bands = append(out.bands, values)
		out.masks = append(out.masks, bandMask)
	}
	if len(out.bands) == 0 {
		return nil, fmt.Errorf("not a Lerc2 blob")
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("%d trailing bytes after LERC bands", r.remaining())
	}
	return out, nil
}

func lercDecodeBand(r *lercReader, prevMask *lercBitMask) (*lercHeader, []float64, *lercBitMask, error) {
	bandStart := r.pos
	hd, err := lercReadHeader(r)
	if err != nil {
		return nil, nil, nil, err
	}
	if hd.blobSize <= 0 || bandStart+hd.blobSize > len(r.buf) {
		return nil, nil, nil, fmt.Errorf("LERC band blob size %d outside data", hd.blobSize)
	}
	if hd.version >= 3 {
		headerBytes := len(lercFileKey) + 4 + 4 // key, version, checksum
		if hd.blobSize < headerBytes {
			return nil, nil, nil, fmt.Errorf("LERC blob size smaller than header")
		}
		checksum := lercFletcher32(r.buf[bandStart+headerBytes : bandStart+hd.blobSize])
		if checksum != hd.checksum {
			return nil, nil, nil, fmt.Errorf("LERC blob checksum mismatch")
		}
	}
	if hd.bPassNoDataValues != 0 {
		return nil, nil, nil, fmt.Errorf("LERC blobs with pass-through noData values are not supported")
	}
	mask, err := lercReadMask(r, hd, prevMask)
	if err != nil {
		return nil, nil, nil, err
	}

	var values []float64
	switch hd.dt {
	case lercDTChar:
		values, err = lercDecodeTypedToFloats[int8](r, hd, mask)
	case lercDTByte:
		values, err = lercDecodeTypedToFloats[uint8](r, hd, mask)
	case lercDTShort:
		values, err = lercDecodeTypedToFloats[int16](r, hd, mask)
	case lercDTUShort:
		values, err = lercDecodeTypedToFloats[uint16](r, hd, mask)
	case lercDTInt:
		values, err = lercDecodeTypedToFloats[int32](r, hd, mask)
	case lercDTUInt:
		values, err = lercDecodeTypedToFloats[uint32](r, hd, mask)
	case lercDTFloat:
		values, err = lercDecodeTypedToFloats[float32](r, hd, mask)
	case lercDTDouble:
		values, err = lercDecodeTypedToFloats[float64](r, hd, mask)
	default:
		err = fmt.Errorf("invalid LERC data type %d", hd.dt)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	// Band blobs are self-delimiting through blobSize; normalize the read
	// position so the next band starts exactly at the boundary.
	r.pos = bandStart + hd.blobSize
	return hd, values, mask, nil
}

func lercReadHeader(r *lercReader) (*lercHeader, error) {
	key := make([]byte, len(lercFileKey))
	if r.remaining() < len(key) {
		return nil, fmt.Errorf("LERC blob truncated")
	}
	copy(key, r.buf[r.pos:])
	r.pos += len(key)
	if string(key) != lercFileKey {
		return nil, fmt.Errorf("not a Lerc2 blob")
	}
	hd := &lercHeader{}
	v, err := r.i32()
	if err != nil {
		return nil, err
	}
	hd.version = int(v)
	if hd.version < 0 || hd.version > lercCurrentVersion {
		return nil, fmt.Errorf("unsupported Lerc2 version %d", hd.version)
	}
	if hd.version >= 3 {
		if hd.checksum, err = r.u32(); err != nil {
			return nil, err
		}
	}
	nInts := 6
	if hd.version >= 4 {
		nInts++
	}
	if hd.version >= 6 {
		nInts++
	}
	ints := make([]int, nInts)
	for i := range ints {
		v, err := r.i32()
		if err != nil {
			return nil, err
		}
		ints[i] = int(v)
	}
	i := 0
	hd.nRows = ints[i]
	i++
	hd.nCols = ints[i]
	i++
	hd.nDepth = 1
	if hd.version >= 4 {
		hd.nDepth = ints[i]
		i++
	}
	hd.numValidPixel = ints[i]
	i++
	hd.microBlockSize = ints[i]
	i++
	hd.blobSize = ints[i]
	i++
	dt := ints[i]
	i++
	if hd.nRows <= 0 || hd.nCols <= 0 || hd.nDepth <= 0 || hd.numValidPixel < 0 ||
		hd.microBlockSize <= 0 || hd.blobSize <= 0 || dt < lercDTChar || dt > lercDTDouble {
		return nil, fmt.Errorf("invalid LERC header")
	}
	hd.dt = dt
	if hd.version >= 6 {
		hd.nBlobsMore = ints[i]
		bytes4 := make([]byte, 4)
		for j := range bytes4 {
			if bytes4[j], err = r.u8(); err != nil {
				return nil, err
			}
		}
		hd.bPassNoDataValues = bytes4[0]
	}
	nDbls := 3
	if hd.version >= 6 {
		nDbls = 5
	}
	dbls := make([]float64, nDbls)
	for j := range dbls {
		if dbls[j], err = r.f64(); err != nil {
			return nil, err
		}
	}
	hd.maxZError = dbls[0]
	hd.zMin = dbls[1]
	hd.zMax = dbls[2]
	numPixel := hd.nRows * hd.nCols
	if numPixel > maxIntValue()/8 || hd.numValidPixel > numPixel {
		return nil, fmt.Errorf("invalid LERC pixel counts")
	}
	if hd.nDepth > 1<<20 || numPixel > maxIntValue()/hd.nDepth {
		return nil, fmt.Errorf("invalid LERC depth")
	}
	return hd, nil
}

func lercFletcher32(data []byte) uint32 {
	sum1 := uint32(0xffff)
	sum2 := uint32(0xffff)
	words := len(data) / 2
	pos := 0
	for words > 0 {
		tlen := words
		if tlen > 359 {
			tlen = 359
		}
		words -= tlen
		for ; tlen > 0; tlen-- {
			sum1 += uint32(data[pos]) << 8
			pos++
			sum1 += uint32(data[pos])
			pos++
			sum2 += sum1
		}
		sum1 = (sum1 & 0xffff) + (sum1 >> 16)
		sum2 = (sum2 & 0xffff) + (sum2 >> 16)
	}
	if len(data)&1 != 0 {
		sum1 += uint32(data[pos]) << 8
		sum2 += sum1
	}
	sum1 = (sum1 & 0xffff) + (sum1 >> 16)
	sum2 = (sum2 & 0xffff) + (sum2 >> 16)
	return sum2<<16 | sum1
}

func lercReadMask(r *lercReader, hd *lercHeader, prevMask *lercBitMask) (*lercBitMask, error) {
	numBytesMask, err := r.i32()
	if err != nil {
		return nil, err
	}
	numValid := hd.numValidPixel
	total := hd.nCols * hd.nRows
	if (numValid == 0 || numValid == total) && numBytesMask != 0 {
		return nil, fmt.Errorf("unexpected LERC mask for trivial validity")
	}
	mask := newLercBitMask(hd.nCols, hd.nRows)
	switch {
	case numValid == 0:
		// all invalid: mask stays zero
	case numValid == total:
		mask.setAllValid()
	case numBytesMask > 0:
		if r.remaining() < int(numBytesMask) {
			return nil, fmt.Errorf("LERC mask truncated")
		}
		if err := lercRLEDecompress(r.buf[r.pos:r.pos+int(numBytesMask)], mask.bits); err != nil {
			return nil, err
		}
		r.pos += int(numBytesMask)
		if mask.countValid() != numValid {
			return nil, fmt.Errorf("LERC mask valid count mismatch")
		}
	default:
		// mask bytes absent: the band reuses the previous band's mask
		if prevMask == nil || prevMask.nCols != hd.nCols || prevMask.nRows != hd.nRows {
			return nil, fmt.Errorf("LERC blob references a previous mask that is not available")
		}
		copy(mask.bits, prevMask.bits)
	}
	return mask, nil
}

func lercDecodeTypedToFloats[T lercNumber](r *lercReader, hd *lercHeader, mask *lercBitMask) ([]float64, error) {
	data, err := lercDecodeTyped[T](r, hd, mask)
	if err != nil {
		return nil, err
	}
	out := make([]float64, len(data))
	for i, v := range data {
		out[i] = float64(v)
	}
	return out, nil
}

type lercBandDecoder[T lercNumber] struct {
	hd      *lercHeader
	mask    *lercBitMask
	zMinVec []float64
	zMaxVec []float64
}

func lercDecodeTyped[T lercNumber](r *lercReader, hd *lercHeader, mask *lercBitMask) ([]T, error) {
	d := &lercBandDecoder[T]{hd: hd, mask: mask}
	data := make([]T, hd.nCols*hd.nRows*hd.nDepth)
	if hd.numValidPixel == 0 {
		return data, nil
	}
	if hd.zMin == hd.zMax { // image is const
		d.fillConstImage(data)
		return data, nil
	}
	if hd.version >= 4 {
		if err := d.readMinMaxRanges(r); err != nil {
			return nil, err
		}
		if d.minMaxEqual() {
			d.fillConstImage(data)
			return data, nil
		}
	}
	readDataOneSweep, err := r.u8()
	if err != nil {
		return nil, err
	}
	if readDataOneSweep != 0 {
		if err := d.readDataOneSweep(r, data); err != nil {
			return nil, err
		}
		return data, nil
	}
	if hd.tryHuffmanInt() || hd.tryHuffmanFlt() {
		flag, err := r.u8()
		if err != nil {
			return nil, err
		}
		if flag > 3 || (flag > 2 && hd.version < 6) || (flag > 1 && hd.version < 4) {
			return nil, fmt.Errorf("invalid LERC image encode mode %d", flag)
		}
		if flag != 0 { // not IEM_Tiling
			if hd.tryHuffmanInt() {
				if flag == 1 || (hd.version >= 4 && flag == 2) {
					if err := d.decodeHuffman(r, data, int(flag)); err != nil {
						return nil, err
					}
					return data, nil
				}
				return nil, fmt.Errorf("invalid LERC Huffman encode mode %d", flag)
			}
			if hd.tryHuffmanFlt() && flag == 3 {
				return nil, fmt.Errorf("LERC lossless floating-point compression (FPL) is not supported")
			}
			return nil, fmt.Errorf("invalid LERC image encode mode %d", flag)
		}
	}
	if err := d.readTiles(r, data); err != nil {
		return nil, err
	}
	return data, nil
}

func (d *lercBandDecoder[T]) fillConstImage(data []T) {
	hd := d.hd
	nDepth := hd.nDepth
	z0 := T(hd.zMin)
	if nDepth == 1 {
		for k := 0; k < hd.nRows*hd.nCols; k++ {
			if d.mask.isValid(k) {
				data[k] = z0
			}
		}
		return
	}
	zBuf := make([]T, nDepth)
	for m := range zBuf {
		zBuf[m] = z0
	}
	if hd.zMin != hd.zMax && len(d.zMinVec) == nDepth {
		for m := 0; m < nDepth; m++ {
			zBuf[m] = T(d.zMinVec[m])
		}
	}
	for k, m := 0, 0; k < hd.nRows*hd.nCols; k, m = k+1, m+nDepth {
		if d.mask.isValid(k) {
			copy(data[m:m+nDepth], zBuf)
		}
	}
}

func (d *lercBandDecoder[T]) readMinMaxRanges(r *lercReader) error {
	nDepth := d.hd.nDepth
	d.zMinVec = make([]float64, nDepth)
	d.zMaxVec = make([]float64, nDepth)
	for i := 0; i < nDepth; i++ {
		v, err := lercReadValue[T](r)
		if err != nil {
			return err
		}
		d.zMinVec[i] = float64(v)
	}
	for i := 0; i < nDepth; i++ {
		v, err := lercReadValue[T](r)
		if err != nil {
			return err
		}
		d.zMaxVec[i] = float64(v)
	}
	return nil
}

func (d *lercBandDecoder[T]) minMaxEqual() bool {
	for i := range d.zMinVec {
		if T(d.zMinVec[i]) != T(d.zMaxVec[i]) {
			return false
		}
	}
	return true
}

func (d *lercBandDecoder[T]) readDataOneSweep(r *lercReader, data []T) error {
	hd := d.hd
	nDepth := hd.nDepth
	for k, m0 := 0, 0; k < hd.nRows*hd.nCols; k, m0 = k+1, m0+nDepth {
		if !d.mask.isValid(k) {
			continue
		}
		for m := 0; m < nDepth; m++ {
			v, err := lercReadValue[T](r)
			if err != nil {
				return err
			}
			data[m0+m] = v
		}
	}
	return nil
}

func (d *lercBandDecoder[T]) readTiles(r *lercReader, data []T) error {
	hd := d.hd
	mbSize := hd.microBlockSize
	if mbSize > 32 {
		return fmt.Errorf("invalid LERC micro block size %d", mbSize)
	}
	numTilesVert := (hd.nRows + mbSize - 1) / mbSize
	numTilesHori := (hd.nCols + mbSize - 1) / mbSize
	for iTile := 0; iTile < numTilesVert; iTile++ {
		tileH := mbSize
		i0 := iTile * tileH
		if iTile == numTilesVert-1 {
			tileH = hd.nRows - i0
		}
		for jTile := 0; jTile < numTilesHori; jTile++ {
			tileW := mbSize
			j0 := jTile * tileW
			if jTile == numTilesHori-1 {
				tileW = hd.nCols - j0
			}
			for iDepth := 0; iDepth < hd.nDepth; iDepth++ {
				if err := d.readTile(r, data, i0, i0+tileH, j0, j0+tileW, iDepth); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (d *lercBandDecoder[T]) readTile(r *lercReader, data []T, i0, i1, j0, j1, iDepth int) error {
	hd := d.hd
	nCols := hd.nCols
	nDepth := hd.nDepth

	comprFlag, err := r.u8()
	if err != nil {
		return err
	}
	bDiffEnc := hd.version >= 5 && comprFlag&4 != 0
	pattern := 15
	if hd.version >= 5 {
		pattern = 14
	}
	if (int(comprFlag>>2) & pattern) != ((j0 >> 3) & pattern) {
		return fmt.Errorf("LERC tile integrity check failed")
	}
	if bDiffEnc && iDepth == 0 {
		return fmt.Errorf("LERC diff encoding on first depth slice")
	}
	bits67 := int(comprFlag >> 6)
	comprFlag &= 3

	if comprFlag == 2 { // tile is constant 0 (or same as previous slice)
		for i := i0; i < i1; i++ {
			k := i*nCols + j0
			m := k*nDepth + iDepth
			for j := j0; j < j1; j, k, m = j+1, k+1, m+nDepth {
				if d.mask.isValid(k) {
					if bDiffEnc {
						data[m] = data[m-1]
					} else {
						data[m] = 0
					}
				}
			}
		}
		return nil
	}

	if comprFlag == 0 { // raw binary
		if bDiffEnc {
			return fmt.Errorf("LERC raw tile with diff encoding")
		}
		for i := i0; i < i1; i++ {
			k := i*nCols + j0
			m := k*nDepth + iDepth
			for j := j0; j < j1; j, k, m = j+1, k+1, m+nDepth {
				if d.mask.isValid(k) {
					v, err := lercReadValue[T](r)
					if err != nil {
						return err
					}
					data[m] = v
				}
			}
		}
		return nil
	}

	// bit stuffed: read the offset in its reduced data type
	srcDT := hd.dt
	if bDiffEnc && hd.dt < lercDTFloat {
		srcDT = lercDTInt
	}
	dtUsed, err := lercDataTypeUsed(srcDT, bits67)
	if err != nil {
		return err
	}
	offset, err := lercReadVariableDataType(r, dtUsed)
	if err != nil {
		return err
	}
	zMax := hd.zMax
	if hd.version >= 4 && nDepth > 1 {
		zMax = d.zMaxVec[iDepth]
	}

	if comprFlag == 3 { // tile is constant offset
		for i := i0; i < i1; i++ {
			k := i*nCols + j0
			m := k*nDepth + iDepth
			if !bDiffEnc {
				val := T(offset)
				for j := j0; j < j1; j, k, m = j+1, k+1, m+nDepth {
					if d.mask.isValid(k) {
						data[m] = val
					}
				}
			} else {
				for j := j0; j < j1; j, k, m = j+1, k+1, m+nDepth {
					if d.mask.isValid(k) {
						z := offset + float64(data[m-1])
						data[m] = T(lercMin(z, zMax))
					}
				}
			}
		}
		return nil
	}

	maxElementCount := (i1 - i0) * (j1 - j0)
	buffer, err := lercBitStufferDecode(r, maxElementCount, hd.version)
	if err != nil {
		return err
	}
	invScale := 2 * hd.maxZError
	src := 0

	if len(buffer) == maxElementCount { // all valid
		for i := i0; i < i1; i++ {
			k := i*nCols + j0
			m := k*nDepth + iDepth
			if !bDiffEnc {
				for j := j0; j < j1; j, m = j+1, m+nDepth {
					z := offset + float64(buffer[src])*invScale
					src++
					data[m] = T(lercMin(z, zMax))
				}
			} else {
				for j := j0; j < j1; j, m = j+1, m+nDepth {
					z := offset + float64(buffer[src])*invScale + float64(data[m-1])
					src++
					data[m] = T(lercMin(z, zMax))
				}
			}
		}
		return nil
	}

	// not all valid
	if hd.version > 2 {
		for i := i0; i < i1; i++ {
			k := i*nCols + j0
			m := k*nDepth + iDepth
			for j := j0; j < j1; j, k, m = j+1, k+1, m+nDepth {
				if !d.mask.isValid(k) {
					continue
				}
				if src >= len(buffer) {
					return fmt.Errorf("LERC tile values exhausted")
				}
				z := offset + float64(buffer[src])*invScale
				if bDiffEnc {
					z += float64(data[m-1])
				}
				src++
				data[m] = T(lercMin(z, zMax))
			}
		}
		return nil
	}
	// version <= 2: same loop, but indexes checked against the buffer length
	idx := 0
	for i := i0; i < i1; i++ {
		k := i*nCols + j0
		m := k*nDepth + iDepth
		for j := j0; j < j1; j, k, m = j+1, k+1, m+nDepth {
			if !d.mask.isValid(k) {
				continue
			}
			if idx == len(buffer) {
				return fmt.Errorf("LERC tile values exhausted")
			}
			z := offset + float64(buffer[idx])*invScale
			idx++
			data[m] = T(lercMin(z, zMax))
		}
	}
	return nil
}

func (d *lercBandDecoder[T]) decodeHuffman(r *lercReader, data []T, mode int) error {
	hd := d.hd
	huffman := &lercHuffman{}
	if err := huffman.readCodeTable(r, hd.version); err != nil {
		return err
	}
	if err := huffman.buildTreeFromCodes(); err != nil {
		return err
	}
	offset := 0
	if hd.dt == lercDTChar {
		offset = 128
	}
	height, width, nDepth := hd.nRows, hd.nCols, hd.nDepth
	st := &lercHuffState{pos: r.pos}
	allValid := hd.numValidPixel == width*height

	const modeDeltaHuffman = 1
	if mode == modeDeltaHuffman {
		for iDepth := 0; iDepth < nDepth; iDepth++ {
			var prevVal T
			for k, m, i := 0, iDepth, 0; i < height; i++ {
				for j := 0; j < width; j, k, m = j+1, k+1, m+nDepth {
					if !allValid && !d.mask.isValid(k) {
						continue
					}
					val, err := huffman.decodeOneValue(r.buf, st)
					if err != nil {
						return err
					}
					delta := T(int32(val - offset)) // wraparound like the C++ byte math
					if j > 0 && (allValid || d.mask.isValid(k-1)) {
						delta += prevVal
					} else if i > 0 && (allValid || d.mask.isValid(k-width)) {
						delta += data[m-width*nDepth]
					} else {
						delta += prevVal
					}
					data[m] = delta
					prevVal = delta
				}
			}
		}
	} else { // IEM_Huffman
		for k, m0, i := 0, 0, 0; i < height; i++ {
			for j := 0; j < width; j, k, m0 = j+1, k+1, m0+nDepth {
				if !allValid && !d.mask.isValid(k) {
					continue
				}
				for m := 0; m < nDepth; m++ {
					val, err := huffman.decodeOneValue(r.buf, st)
					if err != nil {
						return err
					}
					data[m0+m] = T(int32(val - offset))
				}
			}
		}
	}

	numUInts := 1 // the decode LUT can read one word ahead
	if st.bitPos > 0 {
		numUInts++
	}
	consumed := (st.pos - r.pos) + numUInts*4
	if r.remaining() < consumed {
		return fmt.Errorf("LERC Huffman stream truncated")
	}
	r.pos += consumed
	return nil
}

func lercMin(a, b float64) float64 {
	if b < a {
		return b
	}
	return a
}

func lercReadValue[T lercNumber](r *lercReader) (T, error) {
	var v T
	var err error
	switch p := any(&v).(type) {
	case *int8:
		var b byte
		b, err = r.u8()
		*p = int8(b)
	case *uint8:
		*p, err = r.u8()
	case *int16:
		var u uint16
		u, err = r.u16()
		*p = int16(u)
	case *uint16:
		*p, err = r.u16()
	case *int32:
		*p, err = r.i32()
	case *uint32:
		*p, err = r.u32()
	case *float32:
		*p, err = r.f32()
	case *float64:
		*p, err = r.f64()
	default:
		err = fmt.Errorf("unsupported LERC value type")
	}
	return v, err
}

// lercDataTypeUsed mirrors Lerc2::GetDataTypeUsed: the reduced type an offset
// value was stored as.
func lercDataTypeUsed(dt, tc int) (int, error) {
	used := dt
	switch dt {
	case lercDTShort, lercDTInt:
		used = dt - tc
	case lercDTUShort, lercDTUInt:
		used = dt - 2*tc
	case lercDTFloat:
		switch tc {
		case 0:
		case 1:
			used = lercDTShort
		default:
			used = lercDTByte
		}
	case lercDTDouble:
		if tc != 0 {
			used = dt - 2*tc + 1
		}
	}
	if used < lercDTChar || used > lercDTDouble {
		return 0, fmt.Errorf("invalid LERC reduced data type")
	}
	return used, nil
}

func lercReadVariableDataType(r *lercReader, dtUsed int) (float64, error) {
	switch dtUsed {
	case lercDTChar:
		v, err := r.u8()
		return float64(int8(v)), err
	case lercDTByte:
		v, err := r.u8()
		return float64(v), err
	case lercDTShort:
		v, err := r.u16()
		return float64(int16(v)), err
	case lercDTUShort:
		v, err := r.u16()
		return float64(v), err
	case lercDTInt:
		v, err := r.i32()
		return float64(v), err
	case lercDTUInt:
		v, err := r.u32()
		return float64(v), err
	case lercDTFloat:
		v, err := r.f32()
		return float64(v), err
	case lercDTDouble:
		return r.f64()
	default:
		return 0, fmt.Errorf("invalid LERC variable data type %d", dtUsed)
	}
}
