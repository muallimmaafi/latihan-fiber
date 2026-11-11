
# Stage 1: Build aplikasi Go
FROM golang:1.26.5-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app/latihan-fiber .

# Stage 2: Runtime
FROM alpine:3.22

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S appgroup \
    && adduser -S appuser -G appgroup \
    && mkdir -p /app/logs \
    && chown -R appuser:appgroup /app

ENV TZ=Asia/Jakarta
ENV APP_PORT=3000

COPY --from=builder /app/latihan-fiber /app/latihan-fiber

USER appuser

EXPOSE 3000

ENTRYPOINT ["/app/latihan-fiber"]
CMD ["-role=api"]
