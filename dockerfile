# Start with a base Go image
FROM golang:1.23 AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum for dependency installation
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code to the container
COPY . .

# Build the Go app with static linking for compatibility with Alpine
RUN CGO_ENABLED=0 GOOS=linux go build -o soundWaveService

# Use a minimal image to run the app
FROM alpine:latest

# Copy the built binary from the builder stage
COPY --from=builder /app/soundWaveService /soundWaveService

# Run the app with the compiled binary
CMD ["/soundWaveService"]
