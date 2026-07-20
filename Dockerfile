FROM node:24-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf /src/internal/web/dist/*
COPY --from=frontend /src/web/dist/ /src/internal/web/dist/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/up-update ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app \
    && mkdir /data \
    && chown app:app /data
USER app
WORKDIR /app
COPY --from=backend /out/up-update /app/up-update
ENV UP_UPDATE_PORT=8080 UP_UPDATE_DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/up-update"]
