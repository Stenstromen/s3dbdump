FROM golang:1.24-alpine as build
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /s3dbdump ./

FROM scratch
COPY --from=build /s3dbdump /
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
USER 65534:65534
CMD ["/s3dbdump"]