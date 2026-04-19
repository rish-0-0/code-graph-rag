package modc

import "example.com/moda"

// TripleAndAddOne shows modc → moda cross-module call resolution, mirroring
// modb but from a second consumer so the test covers multiple REPLACES edges.
func TripleAndAddOne(x int) int {
	return moda.ExportedFn(x) + moda.ExportedFn(1)
}
