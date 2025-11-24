FROM golang:1.24.2 AS builder

ENV MQC_VERSION="9.4.4.0" \
    CGO_ENABLED=1 \
    XK6_RACE_DETECTOR=1 \
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
RUN go install go.k6.io/xk6/cmd/xk6@latest \
    && xk6 build --with github.com/iambaim/xk6-ibmmq --output /k6

FROM gcr.io/distroless/base-debian12:latest

ENV LD_LIBRARY_PATH="/opt/mqm/lib64:/usr/lib64"

COPY --from=builder /opt/mqm-s /opt/mqm
COPY --from=builder /k6 /usr/bin/k6

ENTRYPOINT ["/usr/bin/k6"]
