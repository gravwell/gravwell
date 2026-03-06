FROM golang:1.26 AS builder
ARG INGESTER
ARG SOURCE
ARG TARGETOS
ARG TARGETARCH
ENV INGESTER=${INGESTER} SOURCE=${SOURCE}

WORKDIR /src
COPY go.* ./
RUN go mod download

COPY . /src/
RUN mkdir -p /bin/ingesters && \
    go build -o /bin/ingesters/${INGESTER} ${SOURCE}

FROM busybox AS runtime
ARG INGESTER
ENV INGESTER=$INGESTER
COPY --from=builder /bin/ingesters/ /opt/gravwell/bin/
CMD /opt/gravwell/bin/${INGESTER} -v