FROM golang:latest AS builder

WORKDIR /src
COPY go.* ./
RUN go mod download

COPY . /src/
RUN mkdir -p /bin/ && \
    go build -o /bin/mock ./tools/mock/mimecast

FROM busybox AS runtime
COPY --from=builder /bin/mock /bin/mock
CMD /bin/mock