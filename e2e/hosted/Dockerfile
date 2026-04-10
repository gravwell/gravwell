FROM --platform=$BUILDPLATFORM golang:1.26.2 AS builder
ARG TARGETOS
ARG TARGETARCH
ENV GOOS=$TARGETOS GOARCH=$TARGETARCH

COPY . /src/
WORKDIR /src
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /bin/hosted && \
    go build -o /bin/hosted/runner ./hosted/runner

FROM busybox AS runtime
COPY --from=builder /bin/hosted/ /opt/gravwell/bin/
CMD /opt/gravwell/bin/runner -v