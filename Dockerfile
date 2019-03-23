FROM golang:1.11.6 as builder

WORKDIR /go/src
ENV GO111MODULE=on
COPY go.mod go.sum *.go ./
COPY phpipam ./phpipam/
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo .
RUN strip kube-phpipam

FROM phusion/baseimage:latest
RUN apt-get update && apt-get -y install ca-certificates && rm -rf /var/lib/apt/lists/*
COPY *.crt /usr/local/share/ca-certificates/
RUN update-ca-certificates
COPY --from=builder /go/src/kube-phpipam /app/
CMD ["/app/kube-phpipam"]
