package main

import (
	"log"
	"os"

	"zenssh/internal/app"
	"zenssh/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "version":
			log.Print(version.String())
			return
		}
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
