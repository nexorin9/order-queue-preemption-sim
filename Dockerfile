# Build stage
FROM python:3.10-slim AS builder

# Install Go
RUN apt-get update && apt-get install -y wget && \
    wget -q https://go.dev/dl/go1.21.6.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.21.6.linux-amd64.tar.gz && \
    rm go1.21.6.linux-amd64.tar.gz

ENV PATH=/usr/local/go/bin:$PATH
ENV GOPATH=/root/go
ENV PATH=$GOPATH/bin:$PATH

WORKDIR /app

# Copy requirements first for caching
COPY requirements.txt .

# Install Python dependencies
RUN pip install --no-cache-dir -r requirements.txt

# Copy project files
COPY . .

# Build Go CLI
RUN go build -o bin/sim ./cmd/sim

# Runtime stage
FROM python:3.10-slim

# Install Go runtime dependencies only
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy Go binary from builder
COPY --from=builder /app/bin/sim /usr/local/bin/sim

# Copy Python packages from builder
COPY --from=builder /usr/local/lib/python3.10/site-packages /usr/local/lib/python3.10/site-packages

# Copy project files (excluding build artifacts)
WORKDIR /app
COPY configs/ ./configs/
COPY sim/ ./sim/
COPY data/ ./data/
COPY outputs/ ./outputs/
COPY requirements.txt .

# Set Python path
ENV PYTHONPATH=/app

# Default command
CMD ["sim", "--help"]