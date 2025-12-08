# Makefile

.PHONY: all build test run-demo

# Build the custom FrankenPHP binary with the WebSocket module
build:
	CGO_CFLAGS="-D_GNU_SOURCE $(shell php-config --includes)" \
	CGO_LDFLAGS="$(shell php-config --ldflags) $(shell php-config --libs)" \
	XCADDY_GO_BUILD_FLAGS="-ldflags='-w -s' -tags=nobadger,nomysql,nopgx,nowatcher" CGO_ENABLED=1 xcaddy build \
		--output frankenphp \
		--with github.com/y-l-g/websocket=. \
		--with github.com/dunglas/frankenphp/caddy \
		--with github.com/dunglas/caddy-cbrotli

# Run the Go Test Suite
test:
	CGO_CFLAGS="-D_GNU_SOURCE $(shell php-config --includes)" \
	CGO_LDFLAGS="$(shell php-config --ldflags) $(shell php-config --libs)" \
	go test -v . -tags=nobadger,nomysql,nopgx,nowatcher -mod=readonly .


# Run Integration Tests (Requires local build)
test-integration: build
	CGO_CFLAGS="-D_GNU_SOURCE $(shell php-config --includes)" \
	CGO_LDFLAGS="$(shell php-config --ldflags) $(shell php-config --libs)" \
	go test -v -tags=nobadger,nomysql,nopgx,nowatcher ./tests/integration/...

# Helper to run the demo
run-demo: build
	./frankenphp run --config examples/frankenchat/Caddyfile