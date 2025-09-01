FROM golang:1.25-alpine AS build
WORKDIR /app
COPY . .
RUN GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=linux go build -a -ldflags='-w -s' -installsuffix cgo -o /s3dbdump ./

FROM scratch
COPY --from=build /s3dbdump /
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
USER 65534:65534
CMD ["/s3dbdump"]