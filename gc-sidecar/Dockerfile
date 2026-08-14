# Single stage: there is nothing to compile, and the runtime dependencies are
# the only dependencies.

FROM node:20-alpine

WORKDIR /app

# Manifests first so the dependency layer only rebuilds when they change.
COPY package.json package-lock.json ./
RUN npm ci --omit=dev

COPY index.js ./

# The refresh token lives here, so Steam Guard is a one-time setup step
# rather than something a restart drags a human into.
RUN mkdir -p /data && chown node:node /data
VOLUME /data

USER node

ENV PORT=3001
EXPOSE 3001

# Reports unhealthy until the game coordinator session is up, which is the
# only state in which this service can actually answer.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:3001/healthz || exit 1

CMD ["node", "index.js"]
