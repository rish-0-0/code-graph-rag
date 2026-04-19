// Package util is a small sibling package that gives the top-level hello
// package some CONTAINS-sibling noise for list:neighbors to surface.
package util

import "strings"

func Shout(s string) string {
	return strings.ToUpper(s) + "!"
}

func Repeat(s string, n int) string {
	return strings.Repeat(s, n)
}

func Reverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}
