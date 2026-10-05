FROM ubuntu:24.04@sha256:a853f94d226358a79c740cfc7bce0c289748f3fe3488d921d038ccd752c61b60

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential crossbuild-essential-arm64 ca-certificates clang curl git \
    libbpf-dev libnuma-dev llvm patch pkg-config python3 unzip zip

ENTRYPOINT ["/usr/local/bin/bazelisk"]
