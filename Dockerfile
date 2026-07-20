FROM golang:1.25-alpine

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /usr/local/bin/gracenotescraper .

WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gracenotescraper"]
