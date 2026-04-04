FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /telemetry-server ./cmd/telemetry-server

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /telemetry-server /telemetry-server
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/telemetry-server"]
