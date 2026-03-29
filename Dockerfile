FROM alpine:3.20 AS base
RUN apk add --no-cache ca-certificates

FROM scratch
COPY --from=base /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY apkgo /usr/local/bin/apkgo
ENTRYPOINT ["apkgo"]
