// Package texture stores the images of textured meshes and packs the parts
// that a mesh uses into an atlas.
package texture

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // PNG textures
	"io"
	"os"

	_ "golang.org/x/image/bmp"  // BMP textures
	_ "golang.org/x/image/tiff" // TIFF textures
)

// channels is the number of bytes of a pixel: red, green and blue.
const channels = 3

// Source gives the pixels of an image, region by region.
type Source interface {
	// Size returns the width and height of the image in pixels.
	Size() (width, height int)
	// Read copies the region of width by height pixels whose top left corner
	// is at x, y to dst, row by row, three bytes a pixel. The region must
	// lie within the image.
	Read(x, y, width, height int, dst []byte) error
}

// Image is an image in memory: its rows from the top, three bytes a pixel.
type Image struct {
	Width, Height int
	Pix           []byte
}

// NewImage returns a black image.
func NewImage(width, height int) *Image {
	return &Image{Width: width, Height: height, Pix: make([]byte, width*height*channels)}
}

// Solid returns a small image of one color.
func Solid(c [3]uint8) *Image {
	img := NewImage(2, 2)
	for i := 0; i < len(img.Pix); i += channels {
		copy(img.Pix[i:], c[:])
	}
	return img
}

func (m *Image) Size() (int, int) { return m.Width, m.Height }

func (m *Image) Read(x, y, width, height int, dst []byte) error {
	for row := range height {
		start := ((y+row)*m.Width + x) * channels
		copy(dst[row*width*channels:], m.Pix[start:start+width*channels])
	}
	return nil
}

// EncodeJPEG writes m to w as a JPEG of the given quality, from 1 to 100.
func (m *Image) EncodeJPEG(w io.Writer, quality int) error {
	rgba := image.NewRGBA(image.Rect(0, 0, m.Width, m.Height))
	for i, o := 0, 0; i < len(m.Pix); i, o = i+channels, o+4 {
		rgba.Pix[o], rgba.Pix[o+1], rgba.Pix[o+2], rgba.Pix[o+3] = m.Pix[i], m.Pix[i+1], m.Pix[i+2], 255
	}
	return jpeg.Encode(w, rgba, &jpeg.Options{Quality: quality})
}

// headerSize is the size of the header of a raw image file: its width and
// height.
const headerSize = 8

// WriteFile writes m to a raw image file at path, which Open and ReadFile
// read.
func (m *Image) WriteFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	var header [headerSize]byte
	binary.LittleEndian.PutUint32(header[0:], uint32(m.Width))
	binary.LittleEndian.PutUint32(header[4:], uint32(m.Height))
	if _, err := f.Write(header[:]); err != nil {
		return errors.Join(err, f.Close())
	}
	if _, err := f.Write(m.Pix); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}

// ReadFile reads the whole raw image file at path.
func ReadFile(path string) (*Image, error) {
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	img := NewImage(f.width, f.height)
	return img, f.Read(0, 0, f.width, f.height, img.Pix)
}

// File is a raw image file, read region by region, so that images larger
// than memory can be used. It is safe for concurrent use.
type File struct {
	file          *os.File
	width, height int
}

// Open opens the raw image file at path.
func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var header [headerSize]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return &File{
		file:   f,
		width:  int(binary.LittleEndian.Uint32(header[0:])),
		height: int(binary.LittleEndian.Uint32(header[4:])),
	}, nil
}

func (f *File) Size() (int, int) { return f.width, f.height }

func (f *File) Read(x, y, width, height int, dst []byte) error {
	if x < 0 || y < 0 || x+width > f.width || y+height > f.height {
		return fmt.Errorf("region %d,%d %dx%d outside the %dx%d image", x, y, width, height, f.width, f.height)
	}
	if width == f.width {
		_, err := f.file.ReadAt(dst[:width*height*channels], headerSize+int64(y)*int64(f.width)*channels)
		return err
	}
	for row := range height {
		offset := headerSize + (int64(y+row)*int64(f.width)+int64(x))*channels
		if _, err := f.file.ReadAt(dst[row*width*channels:(row+1)*width*channels], offset); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the file.
func (f *File) Close() error { return f.file.Close() }

// Convert decodes the image file at source, a PNG, JPEG, TIFF or BMP, and
// writes it as a raw image file at path.
func Convert(source, path string) (err error) {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck
	img, _, err := image.Decode(bufio.NewReaderSize(in, 1<<20))
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	w := bufio.NewWriterSize(out, 4<<20)
	bounds := img.Bounds()
	var header [headerSize]byte
	binary.LittleEndian.PutUint32(header[0:], uint32(bounds.Dx()))
	binary.LittleEndian.PutUint32(header[4:], uint32(bounds.Dy()))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	row := make([]byte, bounds.Dx()*channels)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		fillRow(row, img, y)
		if _, err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Flush()
}

// fillRow copies the row y of img to row, three bytes a pixel.
func fillRow(row []byte, img image.Image, y int) {
	bounds := img.Bounds()
	switch src := img.(type) {
	case *image.NRGBA:
		line := src.Pix[src.PixOffset(bounds.Min.X, y):]
		for x, o := 0, 0; o < len(row); x, o = x+4, o+channels {
			row[o], row[o+1], row[o+2] = line[x], line[x+1], line[x+2]
		}
	case *image.RGBA:
		line := src.Pix[src.PixOffset(bounds.Min.X, y):]
		for x, o := 0, 0; o < len(row); x, o = x+4, o+channels {
			row[o], row[o+1], row[o+2] = line[x], line[x+1], line[x+2]
		}
	case *image.YCbCr:
		for x, o := bounds.Min.X, 0; o < len(row); x, o = x+1, o+channels {
			yi, ci := src.YOffset(x, y), src.COffset(x, y)
			row[o], row[o+1], row[o+2] = color.YCbCrToRGB(src.Y[yi], src.Cb[ci], src.Cr[ci])
		}
	default:
		for x, o := bounds.Min.X, 0; o < len(row); x, o = x+1, o+channels {
			r, g, b, _ := img.At(x, y).RGBA()
			row[o], row[o+1], row[o+2] = uint8(r>>8), uint8(g>>8), uint8(b>>8)
		}
	}
}
