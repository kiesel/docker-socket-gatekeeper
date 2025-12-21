FROM golang:1.25-alpine AS builder

WORKDIR /build

COPY go.mod ./
COPY main.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o docker-gatekeeper .

FROM scratch

COPY --from=builder /build/docker-gatekeeper /docker-gatekeeper

ENTRYPOINT ["/docker-gatekeeper"]
CMD ["-listen", "unix:/run/docker-gatekeeper.sock"]
