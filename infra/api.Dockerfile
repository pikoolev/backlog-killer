FROM golang:1.25.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/server ./cmd/server
FROM alpine:3.21.3
RUN addgroup -S backlog && adduser -S -G backlog -u 10001 backlog
COPY --from=build /out/server /app/server
COPY migrations /app/migrations
COPY infra/local-seed.sql /app/infra/local-seed.sql
WORKDIR /app
RUN mkdir -p /data/uploads && chown -R backlog:backlog /data /app
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app/server"]
