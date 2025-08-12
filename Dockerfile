FROM golang:1.23

RUN apt-get update && apt-get install -y gcc

WORKDIR /app

COPY load_balancer /app/load_balancer
COPY auth/server_1 /app/auth/server_1
COPY backend/petition1 /app/backend/petition1
COPY backend/server_1 /app/backend/server_1

# Build each service
RUN cd /app/load_balancer && go build -o load_balancer
RUN cd /app/auth/server_1 && go build -o auth_server
RUN cd /app/backend/petition1 && go build -o petition1
RUN cd /app/backend/server_1 && go build -o backend_server

COPY start.sh /app/start.sh
RUN chmod +x /app/start.sh

EXPOSE ${PORT}

CMD ["/app/start.sh"]
