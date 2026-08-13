# syntax=docker/dockerfile:1

# ---- build ----------------------------------------------------------------
FROM node:20-alpine AS build

WORKDIR /app

# Manifest and lockfile first, so `npm ci` is only re-run when dependencies
# actually change rather than on every source edit. `npm ci` rather than
# `npm install` because it installs exactly what the lockfile pins and fails
# loudly if the lockfile and package.json have drifted apart — which is what
# you want in an image build, however inconvenient it is locally.
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY . .
RUN npm run build

# ---- runtime --------------------------------------------------------------
# nginx-unprivileged, not the stock nginx image. The stock one starts its
# master process as root and writes its pid to /var/run, so running it as a
# normal user means overriding a good chunk of nginx.conf. This variant is
# built for it: UID 101, port 8080, and every path it writes to already under
# /tmp. Same upstream, same release cadence, just assembled for this case.
FROM nginxinc/nginx-unprivileged:1.27-alpine

# Rendered to /etc/nginx/conf.d/default.conf by the base image's entrypoint on
# container start.
#
# NGINX_ENVSUBST_FILTER is not optional here. Without it the entrypoint hands
# envsubst every environment variable in the container, and any $NAME in the
# config that happens to collide with one gets silently replaced — usually
# with an empty string, and usually noticed much later. Restricting it to the
# one variable we actually template keeps nginx's own $host, $remote_addr and
# friends untouched.
ENV BACKEND_ORIGIN=http://backend:8080 \
    NGINX_ENVSUBST_FILTER=BACKEND_ORIGIN

COPY default.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --quiet --spider http://127.0.0.1:8080/healthz || exit 1
