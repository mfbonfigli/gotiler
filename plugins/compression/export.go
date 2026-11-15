package compression

// ExtensionMeshopt is the glTF extension of buffer views that
// EncodeMeshoptAttributes and EncodeMeshoptTriangles compress.
const ExtensionMeshopt = extMeshoptCompression

// EncodeMeshoptAttributes appends the EXT_meshopt_compression encoding, in
// the ATTRIBUTES mode, of count elements of stride bytes each from data to
// dst, and returns the extended slice. The stride must be a multiple of 4,
// up to 256, and data must hold count elements.
func EncodeMeshoptAttributes(dst, data []byte, count, stride int) []byte {
	return encodeMeshoptAttributesInto(dst, data, count, stride)
}
