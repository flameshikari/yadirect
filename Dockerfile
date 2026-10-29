# syntax=docker/dockerfile:1
ARG BUN_VERSION=1.4.4
FROM dhi.io/bun:${BUN_VERSION}-dev AS install
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production

FROM dhi.io/bun:${BUN_VERSION} AS release
WORKDIR /app
ENV NODE_ENV=production PORT=3000
COPY --from=install --chown=65532:65532 /app/node_modules ./node_modules
COPY --chown=65532:65532 package.json ./
COPY --chown=65532:65532 src ./src
USER 65532:65532
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["bun", "-e", "const r = await fetch('http://127.0.0.1:' + process.env.PORT + '/health', { signal: AbortSignal.timeout(4000) }); process.exit(r.ok ? 0 : 1)"]
ENTRYPOINT ["bun"]
CMD ["src/index.js"]
