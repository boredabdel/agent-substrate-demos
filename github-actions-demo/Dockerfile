FROM golang:1.23-bookworm AS builder
WORKDIR /src
COPY go.mod ./
COPY actor-daemon ./actor-daemon
COPY antigravity-cli ./antigravity-cli
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/actor-daemon ./actor-daemon && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/antigravity-cli ./antigravity-cli

FROM ubuntu:22.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    jq \
    libicu70 \
    libkrb5-3 \
    liblttng-ust1 \
    libssl3 \
    zlib1g \
    procps \
    sudo \
    tar \
    gzip && \
    rm -rf /var/lib/apt/lists/*

RUN useradd -m -d /home/runner -s /bin/bash runner && \
    echo "runner ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers && \
    mkdir -p /runner /home/runner/state && \
    curl -sL https://github.com/actions/runner/releases/download/v2.322.0/actions-runner-linux-x64-2.322.0.tar.gz \
      | tar -xz -C /runner && \
    chown -R runner:runner /runner /home/runner

COPY --from=builder /out/actor-daemon /usr/local/bin/actor-daemon
COPY --from=builder /out/antigravity-cli /usr/local/bin/antigravity-cli
RUN chmod +x /usr/local/bin/actor-daemon /usr/local/bin/antigravity-cli

EXPOSE 80
ENTRYPOINT ["/usr/local/bin/actor-daemon"]
