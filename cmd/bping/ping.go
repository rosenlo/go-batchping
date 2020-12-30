package main

import (
	"log"
	"runtime"

	"github.com/rosenlo/go-batchping/pkg/bping/app"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	if err := app.Execute(); err != nil {
		log.Fatal(err)
	}
}
