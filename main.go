package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	defaultListen = "unix:/var/run/docker-proxy.sock"
	dockerSock    = "/var/run/docker.sock"
)

func main() {
	listen := flag.String("listen", "unix:/var/run/docker-proxy.sock", "listen address: prefix with 'unix:' or 'tcp:'; default unix:/var/run/docker-proxy.sock")

	// path allow flags
	allow := flag.String("allow", "", "comma-separated allowed path prefixes (e.g. /containers,/images)")
	allowContainers := flag.Bool("allow-containers", false, "allow /containers/* paths")
	allowImages := flag.Bool("allow-images", false, "allow /images/* paths")
	allowVolumes := flag.Bool("allow-volumes", false, "allow /volumes/* paths")
	allowNetworks := flag.Bool("allow-networks", false, "allow /networks/* paths")
	allowSwarm := flag.Bool("allow-swarm", false, "allow swarm APIs (/swarm,/services,/nodes,/secrets,/configs)")
	allowSystem := flag.Bool("allow-system", true, "allow system APIs (/info,/version,/_ping)")
	allowEvents := flag.Bool("allow-events", false, "allow /events")
	allowExec := flag.Bool("allow-exec", false, "allow exec/attach APIs (/exec,/containers/*/exec,/containers/*/attach)")

	flag.Parse()

	if *listen == "" {
		*listen = defaultListen
	}

	log.Printf("starting docker socket proxy; target=%s; listen=%s", dockerSock, *listen)

	// build allowed prefixes
	allowed := make([]string, 0)
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
		allowed = append(allowed, "/exec", "/containers")
	}
	if *allow != "" {
		parts := strings.Split(*allow, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if !strings.HasPrefix(p, "/") {
				p = "/" + p
			}
			allowed = append(allowed, p)
		}
	}

	log.Printf("allowed path prefixes: %v", allowed)

	proto, addr, err := parseListen(*listen)
	if err != nil {
		log.Fatalf("invalid listen: %v", err)
	}

	ln, err := net.Listen(proto, addr)
	if err != nil {
		log.Fatalf("listen %s %s: %v", proto, addr, err)
	}

	// If unix, try to set socket mode to 0660 (best-effort)
	if proto == "unix" {
		if f, ok := ln.(*net.UnixListener); ok {
			// can't change mode via f directly; do best-effort on file path
			os.Chmod(addr, 0660)
			_ = f
		}
	}

	done := make(chan struct{})
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		log.Printf("shutting down")
		ln.Close()
		if proto == "unix" {
			os.Remove(addr)
		}
		close(done)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-done:
				// shutdown
				return
			default:
				log.Printf("accept error: %v", err)
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}

		go handleConn(conn, allowed)
	}
}

func parseListen(s string) (proto, addr string, err error) {
	if strings.HasPrefix(s, "unix:") {
		return "unix", strings.TrimPrefix(s, "unix:"), nil
	}
	if strings.HasPrefix(s, "tcp:") {
		return "tcp", strings.TrimPrefix(s, "tcp:"), nil
	}
	// fallback: if it contains ':' treat as tcp host:port
	if strings.Contains(s, ":") {
		return "tcp", s, nil
	}
	return "", "", fmt.Errorf("unknown listen prefix; use unix:/path or tcp:host:port")
}

func handleConn(client net.Conn, allowed []string) {
	defer client.Close()

	// read request headers to inspect path
	r := bufio.NewReader(client)
	var hdr bytes.Buffer

	// read first line (request line)
	firstLine, err := r.ReadString('\n')
	if err != nil {
		return
	}
	hdr.WriteString(firstLine)

	// read headers until empty line
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		hdr.WriteString(line)
		if line == "\r\n" {
			break
		}
	}

	parts := strings.SplitN(strings.TrimSpace(firstLine), " ", 3)
	path := ""
	if len(parts) >= 2 {
		path = parts[1]
	}

	allowedMatch := false
	for _, p := range allowed {
		if strings.HasPrefix(path, p) {
			allowedMatch = true
			break
		}
	}

	if !allowedMatch {
		// deny
		resp := "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
		client.Write([]byte(resp))
		return
	}

	backend, err := net.Dial("unix", dockerSock)
	if err != nil {
		log.Printf("dial docker socket: %v", err)
		return
	}
	defer backend.Close()

	// forward the buffered request (request line + headers)
	if _, err := backend.Write(hdr.Bytes()); err != nil {
		return
	}

	// wire client <-> backend: copy remaining buffered data and then ongoing streams
	done := make(chan struct{}, 2)

	go func() {
		if _, err := io.Copy(backend, r); err != nil {
			// ignore
		}
		done <- struct{}{}
	}()

	go func() {
		if _, err := io.Copy(client, backend); err != nil {
			// ignore
		}
		done <- struct{}{}
	}()

	<-done
}
