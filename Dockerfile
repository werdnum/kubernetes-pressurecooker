FROM golang:1.18-bullseye AS builder

COPY . /work
WORKDIR /work
RUN useradd pressurecooker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o app/pressurecooker cmd/main.go

FROM  scratch

COPY  --from=builder /work/app/pressurecooker /usr/sbin/pressurecooker
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/

USER pressurecooker

ENTRYPOINT ["/usr/sbin/pressurecooker", "-logtostderr"]
