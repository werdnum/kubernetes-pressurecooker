FROM amd64/golang:1.18 AS builder

COPY . /work
WORKDIR /work
RUN useradd pressurecooker
RUN go build -o app/pressurecooker cmd/main.go

FROM  golang/alpine

COPY  --from=builder /work/app/pressurecooker /usr/sbin/pressurecooker
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/

USER pressurecooker

ENTRYPOINT ["/usr/sbin/pressurecooker", "-logtostderr"]
