package main

import (
	"github.com/eeyung1/meuseAI/cmd/server"
	"log"
)

func main() {
	if err := server.Main(); err != nil {
		log.Fatal(err)
	}
}
