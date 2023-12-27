FROM node:alpine AS base
WORKDIR /app

FROM base AS install
COPY --chown=node src/package.json /app
RUN npm install

FROM base AS release
COPY --chown=node --from=install /app/node_modules node_modules
COPY --chown=node src/ .
USER node
CMD node index.js