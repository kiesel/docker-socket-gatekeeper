# Docker socket proxy

Small Go service that proxies connections to the Docker socket at `/var/run/docker.sock`.

### Features
- Listen on a unix socket (default) or TCP and forward bi-directionally to the Docker socket.
- Simple signal handling and socket cleanup on shutdown.

## Usage

Build:

```bash
go build -o docker-proxy ./
```

Run (default unix listener):

```bash
./docker-proxy
```

Run on TCP (example port 2375):

```bash
./docker-proxy -listen tcp:127.0.0.1:2375
```