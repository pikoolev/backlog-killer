FROM node:22.15.1-alpine AS build
ARG APP
WORKDIR /src
RUN corepack enable && corepack prepare pnpm@10.2.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json ./
COPY apps ./apps
COPY packages ./packages
RUN pnpm install --frozen-lockfile
RUN pnpm --filter @backlog-killer/${APP} build
RUN mkdir -p /out && cp -r apps/${APP}/dist/. /out/
FROM nginx:1.27.5-alpine
COPY infra/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /out /usr/share/nginx/html
EXPOSE 80
