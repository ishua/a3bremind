FROM golang:alpine AS build
RUN apk add --no-cache ca-certificates git
WORKDIR /build
ADD . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -ldflags '-extldflags "-static"' -o app ./cmd/a3bremindbot

FROM alpine:3.23
RUN apk add -U tzdata
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /build/app /app
RUN mkdir /data

ENTRYPOINT ["/app"]
