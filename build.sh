#!/bin/bash

VERSION=`head -1 CHANGELOG | cut -d' ' -f21`
echo "Building version $VERSION"

docker build -t dparrish/kube-phpipam:$VERSION .
echo "$DOCKER_PASSWORD" | docker login -u "$DOCKER_USERNAME" --password-stdin
docker push dparrish/kube-phpipam:$VERSION 

