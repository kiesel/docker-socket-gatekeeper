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
	"strconv"
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
	allowSystem := flag.Bool("allow-system", false, "allow system APIs (/info,/version,/_ping)")
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
	if after, ok := strings.CutPrefix(s, "unix:"); ok {
		return "unix", after, nil
	}
	if after, ok := strings.CutPrefix(s, "tcp:"); ok {
		return "tcp", after, nil
	}
	// fallback: if it contains ':' treat as tcp host:port
	if strings.Contains(s, ":") {
		return "tcp", s, nil
	}
	return "", "", fmt.Errorf("unknown listen prefix; use unix:/path or tcp:host:port")
}

func handleConn(client net.Conn, allowed []string) {
	defer client.Close()

	backend, err := net.Dial("unix", dockerSock)
	if err != nil {
		log.Printf("dial docker socket: %v", err)
		return
	}
	defer backend.Close()

	clientReader := bufio.NewReader(client)
	backendReader := bufio.NewReader(backend)

	// relay loop: continuously validate and forward requests
	for {
		// read request from client
		req, err := readHTTPRequest(clientReader)
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Printf("error reading request: %v", err)
			return
		}

		// check if path is allowed
		if !isPathAllowed(req.path, allowed) {
			log.Printf("denied request: %s %s (path not allowed)", req.method, req.path)
			// send 403 and close
			forbidden := "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
			client.Write([]byte(forbidden))
			return
		}

		log.Printf("allowed request: %s %s", req.method, req.path)

		// forward request headers to backend
		if _, err := backend.Write(req.rawHeaders); err != nil {
			return
		}

		// forward request body (if any) to backend
		if req.bodyLen > 0 {
			if _, err := io.CopyN(backend, clientReader, req.bodyLen); err != nil {
				return
			}
		}

		// read response from backend and forward to client
		resp, err := readHTTPResponse(backendReader)
		if err != nil {
			return
		}

		if _, err := client.Write(resp.rawHeaders); err != nil {
			return
		}

		if resp.bodyLen > 0 {
			if _, err := io.CopyN(client, backendReader, resp.bodyLen); err != nil {
				return
			}
		}

		// check Connection header
		if resp.closeConn {
			return
		}
	}
}

// httpRequest holds parsed HTTP request metadata
type httpRequest struct {
	method     string
	path       string
	rawHeaders []byte
	bodyLen    int64
}

// httpResponse holds parsed HTTP response metadata
type httpResponse struct {
	rawHeaders []byte
	bodyLen    int64
	closeConn  bool
}

func readHTTPRequest(r *bufio.Reader) (*httpRequest, error) {
	var hdr bytes.Buffer

	// read request line
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	hdr.WriteString(line)

	parts := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid request line")
	}

	method := parts[0]
	path := parts[1]

	// read headers
	bodyLen := int64(0)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		hdr.WriteString(line)

		if line == "\r\n" {
			break
		}

		// parse Content-Length
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			lenStr := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(line), "content-length:"))
			if n, err := strconv.ParseInt(lenStr, 10, 64); err == nil {
				bodyLen = n
			}
		}
	}

	return &httpRequest{
		method:     method,
		path:       path,
		rawHeaders: hdr.Bytes(),
		bodyLen:    bodyLen,
	}, nil
}

func readHTTPResponse(r *bufio.Reader) (*httpResponse, error) {
	var hdr bytes.Buffer

	// read status line
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	hdr.WriteString(line)

	// read headers
	bodyLen := int64(0)
	closeConn := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		hdr.WriteString(line)

		if line == "\r\n" {
			break
		}

		// parse Content-Length
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			lenStr := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(line), "content-length:"))
			if n, err := strconv.ParseInt(lenStr, 10, 64); err == nil {
				bodyLen = n
			}
		}

		// check Connection header
		if strings.HasPrefix(strings.ToLower(line), "connection:") {
			connVal := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(line), "connection:")))
			if connVal == "close" {
				closeConn = true
			}
		}
	}

	return &httpResponse{
		rawHeaders: hdr.Bytes(),
		bodyLen:    bodyLen,
		closeConn:  closeConn,
	}, nil
}

func isPathAllowed(path string, allowed []string) bool {
	for _, p := range allowed {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
