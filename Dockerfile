FROM golang:1.26-bookworm AS builder

ENV MQC_VERSION="9.4.5.1" \
    CGO_CFLAGS="-I/opt/mqm/inc" \
    CGO_LDFLAGS="-L/opt/mqm/lib64 -Wl,-rpath=/opt/mqm/lib64" \
    genmqpkg_inctls=1

# Install necessary packages
RUN apt-get update && apt-get install -y \
    gcc \
    libc6-dev \
    wget \
    tar

# Install IBM MQ and strip out the unnecessary files with genmqpkg
RUN wget -q https://public.dhe.ibm.com/ibmdl/export/pub/software/websphere/messaging/mqdev/redist/${MQC_VERSION}-IBM-MQC-Redist-LinuxX64.tar.gz \
    && mkdir /opt/mqm \
    && tar -xzf ${MQC_VERSION}-IBM-MQC-Redist-LinuxX64.tar.gz -C /opt/mqm \
    && mkdir /opt/mqm-s \
    && /opt/mqm/bin/genmqpkg.sh -b /opt/mqm-s

WORKDIR /workspace

# Build the k6 binary with the xk6-ibmmq extension
COPY go.mod go.sum ibmmq.go build.sh ./
ARG XK6_RACE_DETECTOR=1
RUN XK6_RACE_DETECTOR="$XK6_RACE_DETECTOR" ./build.sh --output /k6

FROM gcr.io/distroless/base-debian12:latest

ENV LD_LIBRARY_PATH="/opt/mqm/lib64:/usr/lib64"

COPY --from=builder /opt/mqm-s /opt/mqm
COPY --from=builder /k6 /usr/bin/k6

ENTRYPOINT ["/usr/bin/k6"]
