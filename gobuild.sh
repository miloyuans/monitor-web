#!/bin/bash
set -e
Packages=$1
# Build for x86 (32-bit)
GOOS=linux GOARCH=386 CGO_ENABLED=0 go build -o ${Packages}-linux-386_32 ./cmd/monitor-web
echo "Built monitor-web-386 for x86"

# Build for AMD64 (64-bit)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ${Packages}-linux-amd64 ./cmd/monitor-web
echo "Built monitor-web-amd64 for amd64"

# Build for ARM64
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o ${Packages}-linux-arm64 ./cmd/monitor-web
echo "Built monitor-web-arm64 for arm64"