FROM golang:1.26-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
ENV GOOS=linux

RUN go build -ldflags '-w -s' -a -installsuffix cgo -o /main .

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=build /main /main

EXPOSE 8080

ENTRYPOINT ["/main"]
