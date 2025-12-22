# Docker socket gatekeeper

Small Go service that proxies connections to the Docker socket from `/var/run/docker.sock` and only allows access to specified endpoints.

## Features

- Listens on a unix (default) or tcp socket and forward bi-directionally to the Docker socket
- only allows requests to passthrough that match whitelisted HTTP methods and paths

### Rationale

It is common practise to mount the Docker socket `/var/run/docker.sock` into a container such as [traefik](https://traefik.io/) to allow it to dynamically collect information about containers to proxy to the outside. Often, this socket is mounted read-only - in a false sense of security: it is not a file, but a unix-socket over which a REST API is exposed. With `:ro` one cannot delete or change the socket itself, but communication over the socket is not restricted in any way. *docker-gatekeeper* aims to fix this.

### Prior work

There is prior work to this project which heavily influenced it:

- [Tecnativa's docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy)
- [linuxserver's docker-socket-proxy](https://github.com/linuxserver/docker-socket-proxy)

Both are based on [haproxy](https://www.haproxy.org/).

## Building

Build:

```bash
go build -o docker-gatekeeper ./
```

## Usage

Run (default unix listener):

```bash
./docker-gatekeeper
```

Run on TCP (example port 2375):

```bash
./docker-gatekeeper -listen tcp://127.0.0.1:2375
```

### Suitable options for usage w/ traefik

If you plan to use docker-socket-gatekeeper in front of traefik, these options provide sufficient (and safe, hopefully) permissions:

```shell
$ ./docker-gatekeeper -listen unix:///path/to/docker.sock -docker-sock unix:///var/run/docker.sock \
  -allow-read \
  -allow-system \
  -allow-containers \
  -allow-events
```

### Run w/ traefik in Docker compose

Run docker-socket-gatekeeper alongside traefik:

```yaml
services:
  traefik:
    container_name: traefik
    image: traefik:v3.6.5
    volumes:
      - /etc/localtime:/etc/localtime:ro
      - docker-gatekeeper:/var/run/docker-gatekeeper/:ro

    environment:
      DOCKER_HOST: 'unix:///var/run/docker-gatekeeper/docker.sock'

    depends_on:
      - docker-gatekeeper
      
  docker-gatekeeper:
    container_name: docker-gatekeeper
    user: 1000:1000
    image: ghcr.io/kiesel/docker-socket-gatekeeper:latest
    command:
      - -docker-sock=unix:///var/run/docker.sock
      - -listen=unix:///var/run/docker-gatekeeper/docker.sock
      - -allow-read
      - -allow-system
      - -allow-containers
      - -allow-events
    read_only: true
    group_add:
      - 968 # docker group on target host, check stat -c '%g' /var/run/docker.sock

    volumes:
      - docker-gatekeeper:/var/run/docker-gatekeeper/:rw
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  docker-gatekeeper:
    driver_opts:
      type: 'tmpfs'
      device: 'tmpfs'
      o: 'size=1m,uid=1000,gid=1000'
```

docker-gatekeeper can run rootless (`user: 1000:1000`), when it is added to the Docker group with `group_add`.

## Docker tags

* `vX.Y.Z` - fixed version Docker image (always use this in production)
* `latest` - latest development build, not reliable!