// Package greet holds formatting interfaces and a deep call chain used
// by the top-level hello package.
package greet

import "strings"

// Formatter turns a message into a presentation string.
type Formatter interface {
	Format(msg string) string
}

// Renderer wraps a Formatter with pre/post hooks.
type Renderer interface {
	Render(msg string) string
}

type PlainFormatter struct{}

func (PlainFormatter) Format(msg string) string { return msg }

type FancyFormatter struct {
	Prefix string
	Suffix string
}

func (f FancyFormatter) Format(msg string) string {
	return f.Prefix + msg + f.Suffix
}

type DefaultRenderer struct {
	F Formatter
}

func (r DefaultRenderer) Render(msg string) string {
	return strings.TrimSpace(r.F.Format(msg))
}

// FormatChain is the entry point for a 4-deep call chain:
// FormatChain → applyFormat → finalize → polish → strings.ToUpper.
func FormatChain(msg string) string {
	return applyFormat(FancyFormatter{Prefix: "<<", Suffix: ">>"}, msg)
}

func applyFormat(f Formatter, msg string) string {
	return finalize(f.Format(msg))
}

func finalize(msg string) string {
	return polish(msg)
}

func polish(msg string) string {
	return strings.ToUpper(msg)
}
