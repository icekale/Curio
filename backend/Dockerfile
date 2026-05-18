FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend-build
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend-build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
ARG TARGETOS=linux
ARG TARGETARCH
RUN if [ -z "$TARGETARCH" ]; then TARGETARCH="$(go env GOARCH)"; fi; \
  CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -o /out/curio ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates ffmpeg \
  && adduser -D -H curio \
  && mkdir -p /data/Curio /app/public \
  && chown -R curio:curio /data/Curio
COPY --from=backend-build /out/curio /curio
COPY --from=frontend-build /src/frontend/dist /app/public
USER curio
EXPOSE 8080
ENTRYPOINT ["/curio"]
