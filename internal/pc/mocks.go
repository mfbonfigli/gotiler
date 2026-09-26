package pc

import (
	"io"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

type MockLasReader struct {
	Cur         int
	Pts         []geom.Point64
	CRS         string
	Schema      []model.AttributeDescriptor
	CloseCalled bool
}

// AttributeSchema returns the mock's declared attribute schema.
func (m *MockLasReader) AttributeSchema() []model.AttributeDescriptor {
	return m.Schema
}

// NumberOfPoints returns the number of points stored in the LAS file
func (m *MockLasReader) NumberOfPoints() int {
	return len(m.Pts)
}
func (m *MockLasReader) GetCRS() string {
	return m.CRS
}
func (m *MockLasReader) GetNext() (geom.Point64, error) {
	if m.Cur < len(m.Pts) {
		m.Cur++
		return m.Pts[m.Cur-1], nil
	}
	return geom.Point64{}, io.EOF
}
func (m *MockLasReader) Reset() error {
	m.Cur = 0
	return nil
}

func (m *MockLasReader) Close() {
	m.CloseCalled = true
}
