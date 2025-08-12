#!/bin/bash

# Start all services in the background
/app/auth/server_1/auth_server &
/app/backend/petition1/petition1 &
/app/backend/server_1/backend_server &

# Start load balancer in the foreground
/app/load_balancer/load_balancer
