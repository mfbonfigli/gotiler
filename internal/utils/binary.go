package utils

import (
	"encoding/binary"
	"io"
	"math"
)

// Writes the 4 byte array corresponding the the given int value to the given reader
func WriteIntAs4ByteNumber(i int, w io.Writer) error {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(i))
	_, err := w.Write(b[:])
	return err
}

// WriteFloat32LittleEndian writes a float32 number as a float32
// in little endian notation to the given writer
func WriteFloat32LittleEndian(n float32, w io.Writer) error {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], math.Float32bits(n))
	_, err := w.Write(b[:])
	return err
}
