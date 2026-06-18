// Command azure-emulator starts the local multi-service Azure emulator.
package main

import (
	"flag"
	"log"
)

func main() {
	addr := flag.String("addr", ":10000", "address to listen on")
	flag.Parse()

	log.Printf("azure-emulator starting on %s", *addr)

	// TODO: wire up storage, queue, and server modules.
	log.Println("azure-emulator: not yet implemented")
}
