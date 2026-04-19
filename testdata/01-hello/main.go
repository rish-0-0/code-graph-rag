package hello

import (
	"example.com/hello/greet"
	"example.com/hello/util"
)

const Version = "v1"

func Run() string {
	g := NewGreeter("hello")
	var t Talker = g
	base := t.Say(DefaultName)
	return greet.FormatChain(util.Shout(base))
}

func Goodbye() string {
	return "bye"
}
