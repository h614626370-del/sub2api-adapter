FROM node:20-alpine AS web
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY index.html vite.config.ts tsconfig.json ./
COPY src ./src
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/adapter/web/dist ./internal/adapter/web/dist
ARG VERSION=1.0.0
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN go build -trimpath -ldflags="-s -w -X sub2api-adapter/internal/adapter.version=${VERSION} -X sub2api-adapter/internal/adapter.commit=${COMMIT} -X sub2api-adapter/internal/adapter.buildTime=${BUILD_TIME}" -o /out/moderation-adapter ./cmd/moderation-adapter

FROM alpine:3.22
RUN adduser -D -H adapter
WORKDIR /app
COPY --from=build /out/moderation-adapter /usr/local/bin/moderation-adapter
COPY configs/config.example.json /app/configs/config.example.json
RUN mkdir -p /app/data && chown -R adapter:adapter /app/data
USER adapter
EXPOSE 18080
ENV ADAPTER_CONFIG=/app/configs/config.json
ENTRYPOINT ["/usr/local/bin/moderation-adapter"]
