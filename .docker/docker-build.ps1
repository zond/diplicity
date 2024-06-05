# Description: Build docker image from Dockerfile

Write-Host "Building Docker image..."

# Confirm that the script is being executed from the correct directory
if (-not (Test-Path ".docker/docker-run.ps1")) {
    Write-Host "You must run this command from the parent directory of this script."
    exit
}

Write-Host "Confirmed that script is being executed from the correct directory."

$dockerCommand = "docker build --tag 'diplicity' ./.docker"

Write-Host "Running Docker command: $dockerCommand"

# Execute the Docker command
Invoke-Expression $dockerCommand