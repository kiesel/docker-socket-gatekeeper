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
./docker-gatekeeper -listen tcp://127.0.0.1:2375
```

### Options for traefik

If you plan to use docker-socket-gatekeeper in front of traefik, these options provide sufficient (and safe, hopefully) permissions:

```shell
$ ./docker-gatekeeper -listen unix:///path/to/docker.sock -docker-sock /var/run/docker.sock \
  -allow-read \
  -allow-system \
  -allow-containers \
  -allow-events
```

## Run w/ traefik in Docker compose

Run docker-socket-gateway alongside traefik:

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
      - -docker-sock=/var/run/docker.sock
      - -listen=unix:///var/run/docker-gatekeeper/docker.sock
      - -allow-read
      - -allow-system
      - -allow-containers
      - -allow-events
    read_only: true
    group_add:
      - 968 # docker group on target host

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