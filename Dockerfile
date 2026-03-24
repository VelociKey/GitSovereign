# Project GitSovereign - Firehorse Sovereign Exit
# Target: Google Cloud Run (Single-Binary Parity)
# Mandate: Pure Go (CGO_ENABLED=0), Distroless Base

# --- Stage 1: Runtime Packaging ---
# We use the static distroless image for maximum security and minimal footprint.
FROM gcr.io/distroless/static-debian12:latest

# Metadata for Institutional Tracking
LABEL fleet.sovereign.asset="GitSovereign"
LABEL fleet.sovereign.pulse="P6"

# Firehorse Single-Binary (Go + Embedded Flutter WASM-GC)
# This binary must be built with CGO_ENABLED=0 -ldflags="-s -w" -trimpath
COPY firehorse /app/firehorse

# Standard Configuration Path
WORKDIR /app

# UDP/QUIC Port (443/UDP) for Firehorse Transport
EXPOSE 443/udp
# HTTP Port (8080/TCP) for Interaction Surface
EXPOSE 8080/tcp

# Environment Defaults for Cloud Run Parity
ENV SOVEREIGN_MODE="service"
ENV SOVEREIGN_PORT="8080"
ENV WORKSTATION_URL="https://workstation.sovereign.fleet"

# Entrypoint: Execute the Firehorse Sovereign Engine
# We use the list form to ensure signals (SIGTERM) are handled correctly.
ENTRYPOINT ["/app/firehorse"]

# Default arguments: start as a service on the designated port.
CMD ["--mode", "service", "--port", "8080"]
