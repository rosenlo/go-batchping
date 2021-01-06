package main

import (
	"log"
	"runtime"

	"github.com/rosenlo/go-batchping/examples/bping"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	if err := bping.Execute(); err != nil {
		log.Fatal(err)
	}
}
