FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /pricing .

FROM alpine:3.20
RUN apk --no-cache add ca-certificates
COPY --from=builder /pricing /pricing
RUN mkdir -p /data
EXPOSE 8083
CMD ["/pricing"]
