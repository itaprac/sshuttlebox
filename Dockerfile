FROM golang:1.22-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/site ./cmd/site
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sshuttlebox-site ./cmd/site

FROM alpine:3.20

WORKDIR /app
COPY --from=build /out/sshuttlebox-site /usr/local/bin/sshuttlebox-site
COPY docs ./docs

ENV SHBX_SITE_ROOT=/app/docs
EXPOSE 8080

CMD ["sshuttlebox-site"]
