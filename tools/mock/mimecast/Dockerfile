FROM golang:latest AS builder

#WORKDIR /src
#COPY go.* ./
#RUN go mod download

COPY . /src/
WORKDIR /src
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
     mkdir -p /bin/ && go build -o /bin/mock .

FROM busybox AS runtime
COPY --from=builder --chmod=0777 /bin/mock /bin/mock
CMD /bin/mock