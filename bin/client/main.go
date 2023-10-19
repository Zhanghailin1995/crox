package main

import "crox"

func main() {
	client := crox.ClientBootstrap{}
	client.Boot("6389d977db7f4227bf7e04b2d3305d88", "192.168.1.57:7856")
}
