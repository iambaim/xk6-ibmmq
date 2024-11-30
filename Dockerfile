FROM golang:1.22.1 as builder

ENV MQC_VERSION="9.4.1.0" \
    CGO_ENABLED=1 \
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
    && mkdir mqtmp \
    && tar -xzf ${MQC_VERSION}-IBM-MQC-Redist-LinuxX64.tar.gz -C mqtmp \
    && mkdir /opt/mqm \
    && mqtmp/bin/genmqpkg.sh -b /opt/mqm

WORKDIR /workspace

# Build the k6 binary with the xk6-ibmmq extension
RUN go install go.k6.io/xk6/cmd/xk6@latest \
    && xk6 build --with github.com/iambaim/xk6-ibmmq --output /k6

FROM debian:bookworm-slim

ENV LD_LIBRARY_PATH="/opt/mqm/lib64:/usr/lib64"

COPY --from=builder /opt/mqm /opt/mqm
COPY --from=builder /k6 /usr/bin/k6