package main

import (
    "flag"
    "fmt"
    "io"
    "log"
    "net"
    "os"
    "os/signal"
    "path/filepath"
    "strings"
    "syscall"
    "time"
)

const (
    defaultListen = "unix:/var/run/docker-proxy.sock"
    dockerSock    = "/var/run/docker.sock"
)

func main() {
    listen := flag.String("listen", os.Getenv("DOCKER_PROXY_LISTEN"), "listen address: prefix with 'unix:' or 'tcp:'; default unix:/var/run/docker-proxy.sock")
    flag.Parse()

    if *listen == "" {
        *listen = defaultListen
    }

    log.Printf("starting docker socket proxy; target=%s; listen=%s", dockerSock, *listen)

    proto, addr, err := parseListen(*listen)
    if err != nil {
        log.Fatalf("invalid listen: %v", err)
    }

    // If unix, ensure parent dir exists and remove any existing socket file
    if proto == "unix" {
        dir := filepath.Dir(addr)
        if dir != "" {
            os.MkdirAll(dir, 0755)
        }
        // Remove existing socket if present
        if _, err := os.Stat(addr); err == nil {
            if err := os.Remove(addr); err != nil {
                log.Fatalf("failed to remove existing unix socket %s: %v", addr, err)
            }
        }
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

        go handleConn(conn)
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

func handleConn(client net.Conn) {
    defer client.Close()

    backend, err := net.Dial("unix", dockerSock)
    if err != nil {
        log.Printf("dial docker socket: %v", err)
        return
    }
    defer backend.Close()

    // wire client <-> backend
    done := make(chan struct{}, 2)

    go func() {
        if _, err := io.Copy(backend, client); err != nil {
            // ignore error
        }
        done <- struct{}{}
    }()

    go func() {
        if _, err := io.Copy(client, backend); err != nil {
            // ignore error
        }
        done <- struct{}{}
    }()

    // wait for one direction to finish
    <-done
}
