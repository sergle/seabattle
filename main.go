// Command seabattle runs the local LAN Sea Battle server: an HTTP server that
// serves the embedded web client and a WebSocket game endpoint. Single game,
// two players. See docs/architecture.md / docs/server.md.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	"time"

	"seabattle/internal/netutil"
	"seabattle/internal/server"
)

//go:embed web
var embeddedWeb embed.FS

// version is the application version, printed on startup.
const version = "1.1"

func main() {
	port := flag.String("port", "8080", "TCP port to listen on")
	flag.Parse()

	webFS, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatalf("embed web: %v", err)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	hub := server.NewHub(rng)
	go hub.Run()

	mux := server.NewMux(hub, webFS)
	addr := "0.0.0.0:" + *port
	banner(*port)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func banner(port string) {
	fmt.Printf("Sea Battle v%s on :%s\n", version, port)
	fmt.Printf("  this device : http://localhost:%s\n", port)
	if ip := netutil.LANIP(); ip != "" {
		fmt.Printf("  other phone : http://%s:%s   (same WiFi)\n", ip, port)
	}
}
