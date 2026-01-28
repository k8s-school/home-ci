#!/bin/bash

# Configuration
IMAGE_NAME="home-ci-2404"
CONTAINER_NAME="home-ci-app"

echo "--- Building image $IMAGE_NAME ---"
docker build -t $IMAGE_NAME .

# Suppression du conteneur existant s'il existe
docker rm -f $CONTAINER_NAME 2>/dev/null

echo "--- Starting container $CONTAINER_NAME ---"
# --privileged : Requis pour que systemd gère les cgroups
# --tmpfs : Requis pour que systemd puisse écrire ses fichiers volatiles
# -v /sys/fs/cgroup : Requis pour la hiérarchie des services
docker run -d \
    --name "$CONTAINER_NAME" \
    --privileged \
    --tmpfs /run \
    --tmpfs /run/lock \
    --tmpfs /tmp \
    -v /sys/fs/cgroup:/sys/fs/cgroup:ro \
    "$IMAGE_NAME"

echo "------------------------------------------------"
echo "Waiting for systemd to boot..."
sleep 2

# Vérification du service
docker exec -it "$CONTAINER_NAME" systemctl status home-ci
