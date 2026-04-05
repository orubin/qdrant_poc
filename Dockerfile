# Stage 1: Build the Go application
FROM golang:1.25-alpine AS builder

# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Generate Templ code
RUN templ generate

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/app/main.go

# Stage 2: Final minimal image
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from the builder
COPY --from=builder /app/main .

# Expose port 8080
EXPOSE 8080

# Command to run the application
CMD ["./main"]
