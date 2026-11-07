// Package ply reads point clouds from PLY (Polygon File Format) files.
package ply

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type format int

const (
	formatASCII format = iota + 1
	formatBinaryLittleEndian
	formatBinaryBigEndian
)

var formatsByName = map[string]format{
	"ascii":                formatASCII,
	"binary_little_endian": formatBinaryLittleEndian,
	"binary_big_endian":    formatBinaryBigEndian,
}

type scalarType int

const (
	int8Type scalarType = iota + 1
	uint8Type
	int16Type
	uint16Type
	int32Type
	uint32Type
	float32Type
	float64Type
)

var scalarTypesByName = map[string]scalarType{
	"char":    int8Type,
	"int8":    int8Type,
	"uchar":   uint8Type,
	"uint8":   uint8Type,
	"short":   int16Type,
	"int16":   int16Type,
	"ushort":  uint16Type,
	"uint16":  uint16Type,
	"int":     int32Type,
	"int32":   int32Type,
	"uint":    uint32Type,
	"uint32":  uint32Type,
	"float":   float32Type,
	"float32": float32Type,
	"double":  float64Type,
	"float64": float64Type,
}

func (t scalarType) size() int {
	switch t {
	case int8Type, uint8Type:
		return 1
	case int16Type, uint16Type:
		return 2
	case int32Type, uint32Type, float32Type:
		return 4
	case float64Type:
		return 8
	default:
		return 0
	}
}

func (t scalarType) isInteger() bool {
	return t >= int8Type && t <= uint32Type
}

type property struct {
	name      string
	valueType scalarType
	// countType is the type of the length prefix of a list property, zero for
	// scalar properties.
	countType scalarType
}

func (p property) isList() bool {
	return p.countType != 0
}

type element struct {
	name       string
	count      int
	properties []property
}

type header struct {
	format   format
	elements []element
	// dataOffset is the byte length of the header: element data starts at
	// this offset in the file.
	dataOffset int64
}

// readHeader parses a PLY header and leaves r positioned at the first byte of
// element data. Header lines longer than r's buffer are rejected, which also
// bounds the memory spent on files that are not PLY at all.
func readHeader(r *bufio.Reader) (header, error) {
	p := &headerParser{r: r}
	if err := p.parse(); err != nil {
		return header{}, err
	}
	return p.header, nil
}

type headerParser struct {
	r          *bufio.Reader
	lineNumber int
	header     header
	hasFormat  bool
}

func (p *headerParser) parse() error {
	magic, err := p.nextLine()
	if err != nil {
		return err
	}
	if magic != "ply" {
		return errors.New(`not a PLY file: the first line is not "ply"`)
	}
	for {
		line, err := p.nextLine()
		if err != nil {
			return err
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "end_header":
			if !p.hasFormat {
				return errors.New("invalid PLY header: missing format line")
			}
			return nil
		case "comment", "obj_info":
			continue
		case "format":
			err = p.parseFormat(fields[1:])
		case "element":
			err = p.parseElement(fields[1:])
		case "property":
			err = p.parseProperty(fields[1:])
		default:
			err = fmt.Errorf("unknown keyword %q", fields[0])
		}
		if err != nil {
			return fmt.Errorf("invalid PLY header line %d: %w", p.lineNumber, err)
		}
	}
}

func (p *headerParser) nextLine() (string, error) {
	line, err := p.r.ReadSlice('\n')
	p.lineNumber++
	p.header.dataOffset += int64(len(line))
	switch {
	case errors.Is(err, bufio.ErrBufferFull):
		return "", fmt.Errorf("invalid PLY header line %d: line too long", p.lineNumber)
	case errors.Is(err, io.EOF) && len(line) == 0:
		return "", errors.New("truncated PLY header: missing end_header")
	case err != nil && !errors.Is(err, io.EOF):
		return "", err
	}
	return strings.TrimSpace(string(line)), nil
}

func (p *headerParser) parseFormat(args []string) error {
	if p.hasFormat {
		return errors.New("duplicate format line")
	}
	if len(args) != 2 {
		return errors.New("malformed format line, expected: format <encoding> 1.0")
	}
	f, ok := formatsByName[args[0]]
	if !ok {
		return fmt.Errorf("unsupported format %q", args[0])
	}
	if args[1] != "1.0" {
		return fmt.Errorf("unsupported format version %q", args[1])
	}
	p.header.format = f
	p.hasFormat = true
	return nil
}

func (p *headerParser) parseElement(args []string) error {
	if len(args) != 2 {
		return errors.New("malformed element line, expected: element <name> <count>")
	}
	name := args[0]
	count, err := strconv.Atoi(args[1])
	if err != nil || count < 0 {
		return fmt.Errorf("invalid element count %q", args[1])
	}
	for _, e := range p.header.elements {
		if e.name == name {
			return fmt.Errorf("duplicate element %q", name)
		}
	}
	p.header.elements = append(p.header.elements, element{name: name, count: count})
	return nil
}

func (p *headerParser) parseProperty(args []string) error {
	if len(p.header.elements) == 0 {
		return errors.New("property declared before any element")
	}
	prop, err := parsePropertyArgs(args)
	if err != nil {
		return err
	}
	current := &p.header.elements[len(p.header.elements)-1]
	for _, existing := range current.properties {
		if existing.name == prop.name {
			return fmt.Errorf("duplicate property %q in element %q", prop.name, current.name)
		}
	}
	current.properties = append(current.properties, prop)
	return nil
}

func parsePropertyArgs(args []string) (property, error) {
	if len(args) > 0 && args[0] == "list" {
		if len(args) != 4 {
			return property{}, errors.New("malformed list property line, expected: property list <count type> <value type> <name>")
		}
		countType, err := parseScalarType(args[1])
		if err != nil {
			return property{}, err
		}
		if !countType.isInteger() {
			return property{}, fmt.Errorf("list count type %q is not an integer type", args[1])
		}
		valueType, err := parseScalarType(args[2])
		if err != nil {
			return property{}, err
		}
		return property{name: args[3], valueType: valueType, countType: countType}, nil
	}
	if len(args) != 2 {
		return property{}, errors.New("malformed property line, expected: property <type> <name>")
	}
	valueType, err := parseScalarType(args[0])
	if err != nil {
		return property{}, err
	}
	return property{name: args[1], valueType: valueType}, nil
}

func parseScalarType(name string) (scalarType, error) {
	t, ok := scalarTypesByName[name]
	if !ok {
		return 0, fmt.Errorf("unsupported property type %q", name)
	}
	return t, nil
}
