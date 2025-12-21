# Docker socket proxy / gatekeeper

Small Go service that proxies connections to the Docker socket at `/var/run/docker.sock` and only allows access to specified endpoints.

## Features

- Listens on a unix (default) or tcp socket and forward bi-directionally to the Docker socket
- each HTTP request on persistent connections is validated against allowed paths and methods

## Usage

Build:

```bash
go build -o docker-gatekeeper ./
```

Run (default unix listener):

```bash
./docker-gatekeeper
```

Run on TCP (example port 2375):

```bash
./docker-gatekeeper -listen tcp:127.0.0.1:2375
```

### Options for traefik

If you plan to use docker-socket-gatekeeper infront of traefik, these options provide sufficient (and safe, hopefully) permissions:

```shell
$ ./docker-gatekeeper -listen unix://./docker.sock -docker-sock /var/run/docker.sock \
 -allow-read \
 -allow-system \
 -allow-containers \
 -allow-events
```

