# syntax=docker/dockerfile:1

FROM golang:1.26.7-alpine3.24 AS development

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

EXPOSE 8080

CMD ["go", "run", "./cmd/api"]

FROM golang:1.26.7-alpine3.24 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/vacaro-api \
    ./cmd/api

FROM alpine:3.24 AS production

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app

WORKDIR /app

COPY --from=build --chown=app:app /out/vacaro-api ./vacaro-api

USER app

EXPOSE 8080

ENTRYPOINT ["/app/vacaro-api"]
