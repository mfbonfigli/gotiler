package utils

import (
	"bufio"
	"bytes"
	"reflect"
	"testing"
)

func TestWriteIntAs4ByteNumber(t *testing.T) {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	err := WriteIntAs4ByteNumber(123456789, w)
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}
	w.Flush()
	if !reflect.DeepEqual(b.Bytes(), []byte{21, 205, 91, 7}) {
		t.Errorf("%v", b.Bytes())
	}
}

func TestWriteFloat32LittleEndian(t *testing.T) {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	err := WriteFloat32LittleEndian(123456789, w)
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}
	w.Flush()
	if !reflect.DeepEqual(b.Bytes(), []byte{163, 121, 235, 76}) {
		t.Errorf("%v", b)
	}
}
