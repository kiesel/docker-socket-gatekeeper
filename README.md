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

Path allow flags
-----------------

This proxy only forwards requests whose API path matches an allowed prefix. Flags are provided to enable common Docker API areas or custom prefixes:

- `-allow-containers` : allow `/containers/*` paths
- `-allow-images` : allow `/images/*` paths
- `-allow-volumes` : allow `/volumes/*` paths
- `-allow-networks` : allow `/networks/*` paths
- `-allow-swarm` : allow swarm endpoints (`/swarm`, `/services`, `/nodes`, `/secrets`, `/configs`)
- `-allow-events` : allow `/events`
- `-allow-exec` : allow exec/attach APIs (`/exec`, `/containers/*`)
- `-allow-system` : allow system endpoints (`/_ping`, `/version`, `/info`) — enabled by default
- `-allow` : comma-separated custom prefixes (e.g. `/plugins,/build`)

Example: allow containers and images:

```bash
./docker-proxy -allow-containers -allow-images
```

If a request path doesn't match any allowed prefix the proxy returns `403 Forbidden`.