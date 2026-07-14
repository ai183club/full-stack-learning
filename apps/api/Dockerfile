FROM --platform=linux/amd64 golang:1.26-alpine AS build

WORKDIR /src

RUN apk add --no-cache ca-certificates && update-ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o /out/profile-api ./cmd/server
RUN wget -q -O /out/global-bundle.pem https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem

FROM --platform=linux/arm64 alpine:3.22

COPY --from=build --chown=10001:10001 /out/profile-api /app/profile-api
COPY --from=build --chown=10001:10001 /out/global-bundle.pem /app/certs/global-bundle.pem

USER 10001:10001
WORKDIR /app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
	CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1

CMD ["/app/profile-api"]
