#!/usr/bin/env bash

helm repo add bitnami https://charts.bitnami.com/bitnami && \
helm dependency update . && \
helm upgrade redis-operator . \
    --install --namespace redis-operator --create-namespace \
    --set redisOperator.imagePullPolicy=IfNotPresent \
    --set resources.limits.cpu=500m,resources.limits.memory=1Gi \
    --set resources.requests.cpu=500m,resources.requests.memory=1Gi \
    --set redisOperator.imageName=us-harbor.huoli101.com/dayu/redis-operator \
    --set redisOperator.imageTag=v0.8.1 \
    --set replicas=2