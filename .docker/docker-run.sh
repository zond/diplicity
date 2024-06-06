#!/bin/bash

# Ensure that this command is being called from the parent directory
# of the Dockerfile. This is necessary for the volume mapping to work
# correctly.

echo "Running Docker container..."

# Confirm that the script is being executed from the correct directory
if [ ! -f ".docker/docker-run.sh" ]; then
    echo "You must run this command from the parent directory of this script."
    exit 1
fi

echo "Confirmed that script is being executed from the correct directory."

# Base docker run command
dockerCommand="docker run"

# Mount a volume from the host machine to the container. This means
# that the container will have access to the files in the host machine
# and that changes made in the host machine will be reflected in the
# container.
dockerCommand+=" -v .:/go/src/app:ro" # Add volume mapping

# Specify the network that the container should be connected to. This
# is necessary for the Discord bot to be able to work correctly.
dockerCommand+=" --network my-net" # Specify the network

# Specify that the 8080 port should be exposed to the host machine. This
# is the port that the API application listens on.
dockerCommand+=" -p 8080:8080" # Map port 8080

# Specify that the 8000 port should be exposed to the host machine. This
# is the port that the admin application listens on.
dockerCommand+=" -p 8000:8000" # Map port 8000

# Specify the image that should be used to create the container.
dockerCommand+=" diplicity"

echo "Running Docker command: $dockerCommand"

# Execute the Docker command
eval $dockerCommand