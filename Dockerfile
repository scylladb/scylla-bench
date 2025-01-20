FROM golang:1.23-bookworm AS build

ENV GO111MODULE=on
ENV GOAMD64=v3
ENV GOARM64=v8.3
ENV CFLAGS="-O3"
ENV CXXFLAGS="-O3"
ENV DEBIAN_FRONTEND="noninteractive"
ENV TZ="UTC"

WORKDIR /scylla-bench

COPY . .

RUN apt-get update \
    && apt-get upgrade -y  \
    && apt-get install -y build-essential ca-certificates libc-dev \
    && make build

FROM build AS debug

ENV GODEBUG="default=go1.23,cgocheck=1,disablethp=0,panicnil=0,http2client=1,http2server=1,asynctimerchan=0,madvdontneed=0"
ENV PATH="/scylla-bench/bin:${PATH}"

RUN apt-get install -y gdb gcc iputils-ping mlocate vim \
    && make debug-build \
    && go install github.com/go-delve/delve/cmd/dlv@latest \
    && updatedb

EXPOSE 6060
EXPOSE 2121
EXPOSE 2345

ENTRYPOINT [ \
    "dlv", "exec", "--log", "--listen=0.0.0.0:2345", "--allow-non-terminal-interactive", \
    "--headless", "--api-version=2", "--accept-multiclient", \
    "/scylla-bench/bin/scylla-bench", "--" \
    ]

FROM busybox AS production

WORKDIR /

ENV GODEBUG="default=go1.23,cgocheck=0,disablethp=0,panicnil=0,http2client=1,http2server=1,asynctimerchan=0,madvdontneed=0"

COPY --from=build /scylla-bench/bin/scylla-bench /usr/local/bin/scylla-bench

ENV PATH="/usr/local/bin:${PATH}"

EXPOSE 6060
EXPOSE 2121

ENTRYPOINT ["scylla-bench"]

FROM busybox AS  production-goreleaser

ENV GODEBUG="default=go1.23,cgocheck=0,disablethp=0,panicnil=0,http2client=1,http2server=1,asynctimerchan=0,madvdontneed=0"

WORKDIR /

COPY scylla-bench /usr/local/bin/scylla-bench

ENV PATH="/usr/local/bin:${PATH}"

EXPOSE 6060
EXPOSE 2121

ENTRYPOINT ["scylla-bench"]
