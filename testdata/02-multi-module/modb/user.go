package modb

import "example.com/moda"

func CallA(x int) int {
	return moda.ExportedFn(x) + 1
}
