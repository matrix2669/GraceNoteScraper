FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /gracenotescraper .

FROM python:3.13-alpine
RUN pip install --no-cache-dir rapidfuzz==3.14.5
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /gracenotescraper /gracenotescraper
WORKDIR /data
ENTRYPOINT ["/gracenotescraper"]
