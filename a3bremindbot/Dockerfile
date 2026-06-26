FROM golang:alpine AS build
RUN apk add --no-cache ca-certificates git
WORKDIR /build
ADD . .
ARG APP_VERSION=dev
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -ldflags '-extldflags "-static"' -o app ./cmd/a3bremindbot

FROM alpine:3.23
ARG APP_VERSION=dev
RUN apk add -U tzdata
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /build/app /app
RUN mkdir /data
ENV APP_VERSION=${APP_VERSION}
ENTRYPOINT ["/app"]
