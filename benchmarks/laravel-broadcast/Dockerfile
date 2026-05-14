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
    --with github.com/y-l-g/queue/module@main \
    --with github.com/y-l-g/scheduler/module@main \
    --with github.com/y-l-g/websocket/module=/websocket

FROM serversideup/php:8.5.6-frankenphp-trixie

USER root

COPY --from=builder /usr/local/bin/frankenphp /usr/local/bin/frankenphp

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git libcap2-bin unzip \
    && install-php-extensions sockets @composer \
    && setcap CAP_NET_BIND_SERVICE=+eip /usr/local/bin/frankenphp \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

ARG APP_PATH=benchmarks/laravel-broadcast/pogo

COPY --chown=www-data:www-data composer.json /composer.json
COPY --chown=www-data:www-data lib/. /lib/
COPY --chown=www-data:www-data ${APP_PATH}/. /var/www/html/

WORKDIR /var/www/html

RUN mkdir -p bootstrap/cache storage/framework/cache/data storage/framework/sessions storage/framework/views storage/logs \
    && chown -R www-data:www-data /var/www/html /composer.json /lib /config/caddy /data/caddy

USER www-data

RUN composer install -v \
    --no-dev \
    --optimize-autoloader \
    --no-scripts \
    --prefer-dist \
    --classmap-authoritative \
    && rm -f bootstrap/cache/packages.php bootstrap/cache/services.php \
    && php artisan package:discover --ansi
