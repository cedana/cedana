VERSION=$(git describe --tags)
LDFLAGS="-X main.Version=$VERSION"

go build -ldflags "$LDFLAGS"
