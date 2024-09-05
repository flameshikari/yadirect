FROM node:alpine AS base
WORKDIR /app

FROM base AS install
COPY --chown=node package.json /app
RUN npm install --no-package-lock

FROM base AS release
COPY --chown=node --from=install /app .
COPY --chown=node src/ .
USER node
CMD npm run --silent start:docker