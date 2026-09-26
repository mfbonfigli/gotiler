package version

type TilesetVersion string

const (
	TilesetVersion_1_0 TilesetVersion = "1.0"
	TilesetVersion_1_1 TilesetVersion = "1.1"
)

func (v TilesetVersion) String() string {
	return string(v)
}

func Parse(s string) (TilesetVersion, bool) {
	switch s {
	case string(TilesetVersion_1_0):
		return TilesetVersion_1_0, true
	case string(TilesetVersion_1_1):
		return TilesetVersion_1_1, true
	default:
		return "", false
	}
}
