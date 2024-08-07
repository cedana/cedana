VERSION=$(git describe --tags --always --dirty)
LDFLAGS="-X main.Version=$VERSION"

CGO_ENABLED=1 go build -ldflags "$LDFLAGS"
