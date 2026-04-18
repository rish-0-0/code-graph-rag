package hello

func Run() string {
	g := NewGreeter("hello")
	var t Talker = g
	return t.Say(DefaultName)
}
