#!/bin/bash

# Description: Create a new docker network called my-net
echo "Creating Docker network..."

dockerCommand="docker network create -d bridge my-net"

echo "Running Docker command: $dockerCommand"

# Execute the Docker command
eval $dockerCommand