#!/bin/bash
set -e

APP_NAME="fofa-grabber"
SRC_FILE="main.go"

# Finding Go binary
if [ -x "/usr/local/go/bin/go" ]; then
    GO_BIN="/usr/local/go/bin/go"
elif command -v go >/dev/null 2>&1; then
    GO_BIN="$(command -v go)"
else
    echo "❌ Go binary not found!"
    echo "👉 Install Go or make sure go is in PATH"
    exit 1
fi

echo "🧠 Using Go binary: $GO_BIN"
"$GO_BIN" version

PLATFORMS=(
  "windows/amd64"
#   "windows/386"
  "linux/amd64"
#   "linux/386"
#   "linux/arm"
#   "linux/arm64"
#   "darwin/amd64"
  "darwin/arm64"
)

BUILD_DIR="Builds"
mkdir -p "$BUILD_DIR"

for PLATFORM in "${PLATFORMS[@]}"; do
    GOOS="${PLATFORM%/*}"
    GOARCH="${PLATFORM#*/}"

    OUTPUT_NAME="$APP_NAME-$GOOS-$GOARCH"
    [[ "$GOOS" == "windows" ]] && OUTPUT_NAME+=".exe"

    echo "🚀 Building $GOOS/$GOARCH..."

    env \
      CGO_ENABLED=0 \
      GOOS="$GOOS" \
      GOARCH="$GOARCH" \
      "$GO_BIN" build \
        -ldflags="-s -w" \
        -o "$BUILD_DIR/$OUTPUT_NAME" \
        "$SRC_FILE"

    echo "✅ Done: $BUILD_DIR/$OUTPUT_NAME"
done

echo "🎉 All builds finished!"
