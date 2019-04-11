#!/bin/bash -e

VERSION=`head -1 CHANGELOG | cut -d' ' -f2`
echo "Building version $VERSION"

docker login -u "$DOCKER_USERNAME" -p "$DOCKER_PASSWORD"
docker build -t dparrish/kube-phpipam:$VERSION .
docker push dparrish/kube-phpipam:$VERSION 

