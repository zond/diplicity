# Description: Create a new docker network called my-net

Write-Host "Creating Docker network..."

$dockerCommand = "docker network create -d bridge my-net"

Write-Host "Running Docker command: $dockerCommand"

# Execute the Docker command
Invoke-Expression $dockerCommand