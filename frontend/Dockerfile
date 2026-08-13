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
# through envsubst at container start. Nothing needs substituting now that
# Caddy owns routing (deploy/Caddyfile), but it stays a template so adding a
# run-time knob later doesn't mean relocating the file.
COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1/healthz || exit 1
