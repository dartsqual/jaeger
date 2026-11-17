#!/usr/bin/env bash
# Compila o HotROD com dd-trace-go e gera a imagem OCI sem Docker (usa crane).
# Uso: examples/hotrod/datadog/build-image.sh [TAG]   (padrão: 1.0.1)
# Saída: hotrod-dd.tar (importar no k3s: sudo k3s ctr -n k8s.io images import hotrod-dd.tar)
set -euo pipefail

TAG="${1:-1.0.1}"
ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

command -v crane >/dev/null || go install github.com/google/go-containerregistry/cmd/crane@v0.20.6
CRANE="$(command -v crane || echo "$(go env GOPATH)/bin/crane")"

echo "==> build linux/amd64"
mkdir -p "$WORK/layer/usr/local/bin"
(cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o "$WORK/layer/usr/local/bin/hotrod" ./examples/hotrod)

echo "==> imagem docker.io/library/hotrod-dd:${TAG}"
COPYFILE_DISABLE=1 tar -C "$WORK/layer" --uid 0 --gid 0 --numeric-owner -cf "$WORK/layer.tar" usr
"$CRANE" append --platform linux/amd64 \
  -b gcr.io/distroless/static-debian12:nonroot \
  -f "$WORK/layer.tar" \
  -t "docker.io/library/hotrod-dd:${TAG}" \
  -o hotrod-dd.tar
echo "==> hotrod-dd.tar gerado"
