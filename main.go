package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	listen := flag.String("listen", "unix:///var/run/docker-gatekeeper.sock", "listen address: use scheme prefix 'unix://' or 'tcp://'; default unix:///var/run/docker-gatekeeper.sock")
	dockerSock := flag.String("docker-sock", "unix:///var/run/docker.sock", "docker socket address: use unix://path or tcp://host:port")

	// path allow flags
	allowRead := flag.Bool("allow-read", true, "allow read-only operations (GET, HEAD) on all paths")
	allowWrite := flag.Bool("allow-write", false, "allow write operations (POST, PUT, PATCH, DELETE) on all paths")

	allowContainers := flag.Bool("allow-containers", false, "allow /containers/* paths")
	allowImages := flag.Bool("allow-images", false, "allow /images/* paths")
	allowVolumes := flag.Bool("allow-volumes", false, "allow /volumes/* paths")
	allowNetworks := flag.Bool("allow-networks", false, "allow /networks/* paths")
	allowSwarm := flag.Bool("allow-swarm", false, "allow swarm APIs (/swarm,/services,/nodes,/secrets,/configs)")
	allowSystem := flag.Bool("allow-system", false, "allow system APIs (/info,/version,/_ping)")
	allowEvents := flag.Bool("allow-events", false, "allow /events")

	allowExec := flag.Bool("allow-exec", false, "allow exec/attach APIs (/exec,/containers/*/exec,/containers/*/attach)")

	flag.Parse()

	log.Printf("starting docker socket proxy; target=%s; listen=%s", *dockerSock, *listen)

	// build allowed prefixes
	allowed := make([]string, 0)
	allowedOps := make([]string, 0)
	if *allowRead {
		allowedOps = append(allowedOps, "GET", "HEAD")
	}
	if *allowWrite {
		allowedOps = append(allowedOps, "POST", "PUT", "DELETE", "PATCH")
	}
	if *allowSystem {
		allowed = append(allowed, "/_ping", "/version", "/info")
	}
	if *allowContainers {
		allowed = append(allowed, "/containers")
	}
	if *allowImages {
		allowed = append(allowed, "/images")
	}
	if *allowVolumes {
		allowed = append(allowed, "/volumes")
	}
	if *allowNetworks {
		allowed = append(allowed, "/networks")
	}
	if *allowSwarm {
		allowed = append(allowed, "/swarm", "/services", "/nodes", "/secrets", "/configs")
	}
	if *allowEvents {
		allowed = append(allowed, "/events")
	}
	if *allowExec {
		allowed = append(allowed, "/exec")
	}

	log.Printf("Allowed operations: %v", allowedOps)
	log.Printf("Allowed path prefixes: %v", allowed)

	proto, addr, err := parseListen(*listen)
	if err != nil {
		log.Fatalf("invalid listen: %v", err)
	}

	dockerProto, dockerAddr, err := parseListen(*dockerSock)
	if err != nil {
		log.Fatalf("invalid docker-sock: %v", err)
	}

	ln, err := net.Listen(proto, addr)
	if err != nil {
		log.Fatalf("listen %s %s: %v", proto, addr, err)
	}

	targetURL := &url.URL{Scheme: "http", Host: "docker"}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	proxy.Transport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.Dial(dockerProto, dockerAddr)
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isMethodAllowed(r.Method, allowedOps) {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			log.Printf("Received request %s %s -> DENY (method not allowed)", r.Method, r.URL.Path)
			return
		}

		if !isPathAllowed(r.URL.Path, allowed) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			log.Printf("Received request %s %s -> DENY (path not allowed)", r.Method, r.URL.Path)
			return
		}

		log.Printf("Received request %s %s -> ALLOW", r.Method, r.URL.Path)
		proxy.ServeHTTP(w, r)
	})

	srv := &http.Server{Handler: handler}

	// graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		log.Printf("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func parseListen(s string) (proto, addr string, err error) {
	// Expect scheme-style prefixes: unix://path and tcp://host:port
	if after, ok := strings.CutPrefix(s, "unix://"); ok {
		return "unix", after, nil
	}
	if after, ok := strings.CutPrefix(s, "tcp://"); ok {
		return "tcp", after, nil
	}
	return "", "", fmt.Errorf("unknown listen prefix; use unix://path or tcp://host:port")
}

// stripVersionPrefix removes Docker API version prefix (e.g., /v1.40) from the path.
// e.g., /v1.40/containers/json -> /containers/json
func stripVersionPrefix(path string) string {
	// match /vX.Y or /vX pattern at the start
	if strings.HasPrefix(path, "/v") && len(path) > 2 {
		// find the next slash after /vX.Y
		parts := strings.SplitN(path[1:], "/", 2)
		if len(parts) == 2 && isVersionSegment(parts[0]) {
			return "/" + parts[1]
		}
	}
	return path
}

// isVersionSegment checks if a segment looks like a Docker version (e.g., "v1.40", "v1.41")
func isVersionSegment(s string) bool {
	if !strings.HasPrefix(s, "v") {
		return false
	}
	rest := s[1:]
	parts := strings.Split(rest, ".")
	// should be v{major}.{minor} or v{major}
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return len(parts) > 0
}

func isMethodAllowed(method string, allowed []string) bool {
	for _, m := range allowed {
		if method == m {
			return true
		}
	}
	return false
}
func isPathAllowed(path string, allowed []string) bool {
	normalizedPath := stripVersionPrefix(path)
	for _, p := range allowed {
		if strings.HasPrefix(normalizedPath, p) {
			return true
		}
	}
	return false
}
