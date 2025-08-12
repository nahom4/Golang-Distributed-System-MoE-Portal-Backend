#!/bin/bash

# Start all services in the background
cd /app/auth/server_1 && go run main.go &
cd /app/backend/petition1 && go run main.go &
cd /app/backend/server_1 && go run main.go &

# Start load balancer in the foreground
cd /app/load_balancer && go run LoadBalancer.go