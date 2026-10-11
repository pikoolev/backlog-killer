FROM node:22.15.1-alpine AS build
ARG APP
ARG VITE_API_URL
ARG VITE_OIDC_URL
ARG VITE_OIDC_REALM=backlog-killer
ARG VITE_OIDC_CLIENT_ID=backlog-killer-web
ARG VITE_PROJECT_KEY=demo-project
ENV VITE_API_URL=$VITE_API_URL \
    VITE_OIDC_URL=$VITE_OIDC_URL \
    VITE_OIDC_REALM=$VITE_OIDC_REALM \
    VITE_OIDC_CLIENT_ID=$VITE_OIDC_CLIENT_ID \
    VITE_PROJECT_KEY=$VITE_PROJECT_KEY
WORKDIR /src
RUN corepack enable && corepack prepare pnpm@10.2.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json ./
COPY apps ./apps
COPY packages ./packages
RUN pnpm install --frozen-lockfile
RUN pnpm --filter @backlog-killer/sdk build && pnpm --filter @backlog-killer/${APP} build
RUN mkdir -p /out/sdk && cp -r apps/${APP}/dist/. /out/ && cp packages/sdk/dist/backlog-killer.js /out/sdk/backlog-killer.js
FROM nginx:1.27.5-alpine
COPY infra/nginx.conf /etc/nginx/templates/default.conf.template
COPY --from=build /out /usr/share/nginx/html
EXPOSE 80
