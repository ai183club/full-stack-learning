FROM golang:1.26-alpine AS build

WORKDIR /src

RUN apk add --no-cache ca-certificates && update-ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/profile-api ./cmd/server
RUN wget -q -O /out/global-bundle.pem https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
	&& addgroup -S app \
	&& adduser -S -G app app \
	&& mkdir -p /app/certs

COPY --from=build --chown=app:app /out/profile-api /app/profile-api
COPY --from=build --chown=app:app /out/global-bundle.pem /app/certs/global-bundle.pem

USER app
WORKDIR /app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
	CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1

CMD ["/app/profile-api"]
