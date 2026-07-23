FROM --platform=$BUILDPLATFORM node:22.12-bookworm AS builder

# Install git to clone the upstream repo
RUN apk add --no-cache git

# Clone the MCP servers repo and extract everything server
# Pinned to commit before SDK bump that broke Zod types (2026-01-06)
WORKDIR /tmp
RUN git clone https://github.com/modelcontextprotocol/servers.git && \
    cd servers && git checkout 37b2926 && cd .. && \
    mkdir -p /app && \
    cp -r servers/src/everything/* /app/ && \
    cp servers/tsconfig.json /tsconfig.json

WORKDIR /app

RUN --mount=type=cache,target=/root/.npm npm install && npm run build

FROM --platform=$TARGETPLATFORM node:22-bookworm-slim AS release

WORKDIR /app

COPY --from=builder /app/dist /app/dist
COPY --from=builder /app/package.json /app/package.json
COPY --from=builder /app/package-lock.json /app/package-lock.json

ENV NODE_ENV=production

RUN npm ci --ignore-scripts --omit-dev

# Run with streamableHttp transport for MCP Gateway compatibility
CMD ["node", "dist/index.js", "streamableHttp"]
