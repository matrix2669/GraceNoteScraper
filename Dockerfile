FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /gracenotescraper .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /gracenotescraper /usr/local/bin/gracenotescraper
WORKDIR /data
ENTRYPOINT ["/usr/local/bin/gracenotescraper"]
