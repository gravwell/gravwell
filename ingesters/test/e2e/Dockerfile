FROM --platform=$BUILDPLATFORM golang:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH
ENV GOOS=$TARGETOS GOARCH=$TARGETARCH

COPY . /src/
WORKDIR /src
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /bin/ingesters && \
    for i in $(cat "./ingesters/test/include"); do go build -o /bin/ingesters/$i ./ingesters/$i; done;

FROM busybox AS runtime
COPY --from=builder /bin/ingesters/ /opt/gravwell/bin/
CMD /opt/gravwell/bin/${INGESTER} -v