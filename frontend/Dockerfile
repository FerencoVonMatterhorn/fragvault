# Build the Vite app, then serve it from nginx. Two stages so the runtime
# image carries no Node, no sources and no node_modules — tens of MB instead
# of hundreds, on a VM with 1 GB of RAM.

FROM node:20-alpine AS build
WORKDIR /app

# Manifests first: this layer only rebuilds when dependencies actually change,
# so editing a component doesn't reinstall the tree.
COPY package.json package-lock.json ./
RUN npm ci

COPY . .
# `tsc -b && vite build` (see package.json). Type errors fail the image build,
# which is the point — a build that doesn't compile should never reach the
# registry, let alone the VM.
RUN npm run build

FROM nginx:1.27-alpine AS runtime

# The stock nginx entrypoint renders every *.template in this directory
# through envsubst at container start, so the backend address is a run-time
# knob instead of being baked into the image. It only substitutes variables
# that exist in the environment, so nginx's own $uri / $host are left alone.
COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html

# Overridable at `docker run`. No trailing slash: with a bare host nginx
# forwards the original URI untouched, but adding a path (even "/") makes it
# strip the /api prefix instead.
ENV BACKEND_UPSTREAM=http://backend:8080

# Opt in to the image's local-resolver support. 15-local-resolvers.envsh is
# always sourced but returns early unless this is set:
#   [ "${NGINX_ENTRYPOINT_LOCAL_RESOLVERS:-}" ] || return 0
# Without it, NGINX_LOCAL_RESOLVERS never reaches the environment, envsubst
# leaves the placeholder verbatim in the config, and nginx dies trying to
# resolve a literal "${NGINX_LOCAL_RESOLVERS}" as a hostname.
ENV NGINX_ENTRYPOINT_LOCAL_RESOLVERS=1

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1/healthz || exit 1
