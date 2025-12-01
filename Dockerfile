ARG GO_VER=1.24

FROM golang:${GO_VER} as base
WORKDIR /src
COPY go.mod go.sum /src/
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download -x


FROM base AS inttest
RUN apt update && apt install -y \
    openssh-client \
    sshpass \
    libxml-xpath-perl
RUN --mount=target=/src \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir /out && \
    cd inttest && \
    go test --tags=inttest -c -o /out/inttest.test
WORKDIR /out
COPY inttest/wait-for-hello.sh .
CMD ./inttest.test -test.v -test.race