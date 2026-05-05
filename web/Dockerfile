# Multi-stage build for @rsktash/bd-web. Stage 1 builds the React bundle;
# stage 2 keeps only the Node runtime + server + built dist + node_modules.

FROM node:20-bookworm-slim AS build
WORKDIR /app

# Install deps first for layer caching. better-sqlite3 needs build tools.
RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 build-essential ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY package.json package-lock.json ./
RUN npm ci

COPY client ./client
COPY server ./server
COPY bin ./bin
RUN npm run build

# ---- runtime ----
FROM node:20-bookworm-slim AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV HOST=0.0.0.0
ENV PORT=3333

# Re-install only production deps so node_modules has no dev tools.
COPY package.json package-lock.json ./
RUN npm ci --omit=dev \
 && npm cache clean --force

COPY --from=build /app/dist ./dist
COPY server ./server
COPY bin ./bin

EXPOSE 3333

# bd-web reads .bd/config (and .bd/.env) from the working directory walk-up,
# so callers should bind-mount the workspace and set working_dir to it.
CMD ["node", "server/index.js"]
