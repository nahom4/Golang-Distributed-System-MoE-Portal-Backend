FROM golang:latest

# Install gcc
RUN apt-get update && apt-get install -y gcc

# Set working directory
WORKDIR /app

# Copy all the projects
COPY load_balancer /app/load_balancer
COPY auth/server_1 /app/auth/server_1
COPY backend/petition1 /app/backend/petition1
COPY backend/server_1 /app/backend/server_1

# Install dependencies for all projects
RUN cd /app/load_balancer && go mod download && \
    cd /app/auth/server_1 && go mod download && \
    cd /app/backend/petition1 && go mod download && \
    cd /app/backend/server_1 && go mod download

# Copy the startup script
COPY start.sh /app/start.sh
RUN chmod +x /app/start.sh

EXPOSE ${PORT}
# Run the startup script
CMD ["/app/start.sh"]