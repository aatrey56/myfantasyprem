// Command healthcheck exits 0 when the API answers GET /healthz with 200.
// The container image has no shell or curl, so Docker's HEALTHCHECK runs
// this instead.
//
//	healthcheck                              # http://127.0.0.1:$PORT/healthz, PORT from HTTP_ADDR (default 8080)
//	healthcheck http://127.0.0.1:8080/healthz
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	if err := check(target(os.Args[1:], os.Getenv("HTTP_ADDR"))); err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		os.Exit(1)
	}
}

func target(args []string, addr string) string {
	if len(args) > 0 {
		return args[0]
	}
	port := "8080"
	if _, p, err := net.SplitHostPort(addr); err == nil && p != "" {
		port = p
	}
	return "http://127.0.0.1:" + port + "/healthz"
}

func check(url string) error {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	return nil
}
