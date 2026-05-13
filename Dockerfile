FROM dunglas/frankenphp:1.12.2-builder-php8.5-trixie AS builder

COPY --from=caddy:builder /usr/bin/xcaddy /usr/bin/xcaddy

COPY module/. /websocket/

RUN CGO_ENABLED=1 \
    XCADDY_SETCAP=1 \
    XCADDY_GO_BUILD_FLAGS="-ldflags='-w -s' -tags=nobadger,nomysql,nopgx" \
    CGO_CFLAGS="-D_GNU_SOURCE $(php-config --includes)" \
    CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
    xcaddy build \
    --output /usr/local/bin/frankenphp \
    --with github.com/dunglas/frankenphp@v1.12.2 \
    --with github.com/dunglas/frankenphp/caddy@v1.12.2 \
    --with github.com/dunglas/caddy-cbrotli@v1.0.1 \
    --with github.com/y-l-g/scheduler/module@main \
    --with github.com/y-l-g/websocket/module=/websocket \
    --with github.com/y-l-g/queue/module@main


FROM serversideup/php:8.5.5-frankenphp-trixie-v4.3.5

USER root

COPY --from=builder /usr/local/bin/frankenphp /usr/local/bin/frankenphp

USER www-data
