#!/bin/bash

# Description: Build docker image from Dockerfile
echo "Building Docker image..."

# Confirm that the script is being executed from the correct directory
if [ ! -f ".docker/docker-run.sh" ]; then
    echo "You must run this command from the parent directory of this script."
    exit 1
fi

echo "Confirmed that script is being executed from the correct directory."

dockerCommand="docker build --tag 'diplicity' ./.docker"

echo "Running Docker command: $dockerCommand"

# Execute the Docker command
eval $dockerCommand