FROM golang:1.21-alpine

# Install git for fetching dependencies
RUN apk add --no-cache git

WORKDIR /app

# Copy go mod files first for caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Expose port
EXPOSE 3001

# Run the binary
CMD ["./main"]
