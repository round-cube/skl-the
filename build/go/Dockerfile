FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG SERVICE_NAME

RUN go build -o /app/bin/$SERVICE_NAME ./services/$SERVICE_NAME/main.go


FROM alpine:3.20.3

ARG SERVICE_NAME

WORKDIR /app

COPY --from=builder /app/bin/$SERVICE_NAME /app/bin/$SERVICE_NAME

ENV entrypoint=/app/bin/${SERVICE_NAME}

ENTRYPOINT ${entrypoint}
