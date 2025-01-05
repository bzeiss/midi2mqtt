#!/bin/bash

# Function to validate runner number
validate_runner_number() {
   local num=$1
   # Check if number is a positive integer (1 or higher)
   if [[ $num =~ ^[1-9][0-9]*$ ]]; then
       return 0
   else
       return 1
   fi
}

# Function to validate token format
validate_token() {
    local token=$1
    # Check if token contains only uppercase letters and numbers, at least 20 chars
    if [[ $token =~ ^[A-Z0-9]{20,}$ ]]; then
        return 0
    else
        return 1
    fi
}

# Check if both arguments are provided
if [ $# -ne 2 ]; then
   echo "Usage: $0 <RUNNER_NUMBER> <TOKEN>"
   echo "Example: $0 1 AAGYAYQC5SE4VXAMI27WL33HMYE22"
   echo "RUNNER_NUMBER must be a positive integer"
   echo "TOKEN must be 28 characters long and contain only uppercase letters and numbers"
   exit 1
fi

# Validate runner number
if ! validate_runner_number "$1"; then
   echo "Error: Invalid runner number format"
   echo "Runner number must be a positive integer (1 or higher)"
   exit 1
fi

# Validate token format
if ! validate_token "$2"; then
   echo "Error: Invalid token format"
   echo "Token must be 28 characters long and contain only uppercase letters and numbers"
   exit 1
fi

# Format runner number with leading zero
RUNNER_NUM=$(printf "%02d" "$1")

# Run docker container with provided arguments
docker run -d \
   --name "build-midi2mqtt-linux-${RUNNER_NUM}" \
   --restart always \
   -e "LABELS=self-hosted,AMD64,Linux,midi2mqtt-builder-linux" \
   -e "RUNNER_NAME=build-midi2mqtt-linux-${RUNNER_NUM}" \
   -e "TOKEN=$2" \
   midi2mqtt-runner
