package hello

import "fmt"

const DefaultName = "world"

type Greeter struct {
	Prefix string
}

type Talker interface {
	Say(name string) string
}

func (g Greeter) Say(name string) string {
	return fmt.Sprintf("%s, %s!", g.Prefix, name)
}

func NewGreeter(prefix string) Greeter {
	return Greeter{Prefix: prefix}
}
