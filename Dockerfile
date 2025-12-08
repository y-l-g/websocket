FROM dunglas/frankenphp:1.10-builder-php8.5.0-bookworm AS builder

COPY --from=caddy:builder /usr/bin/xcaddy /usr/bin/xcaddy

COPY . /websocket

RUN CGO_ENABLED=1 \
    XCADDY_SETCAP=1 \
    XCADDY_GO_BUILD_FLAGS="-ldflags='-w -s' -tags=nobadger,nomysql,nopgx" \
    CGO_CFLAGS="-D_GNU_SOURCE $(php-config --includes)" \
    CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
    xcaddy build \
    --output /usr/local/bin/frankenphp \
    --with github.com/dunglas/frankenphp=./ \
    --with github.com/dunglas/frankenphp/caddy=./caddy/ \
    --with github.com/dunglas/caddy-cbrotli \
    --with github.com/y-l-g/websocket=/websocket/

FROM dunglas/frankenphp:1.10-php8.5.0-bookworm AS runner

COPY --from=builder /usr/local/bin/frankenphp /usr/local/bin/frankenphp