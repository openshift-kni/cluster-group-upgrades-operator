#!/bin/bash

kubectl proxy start &
KUBECTL_PROXY_PID=$!
echo $KUBECTL_PROXY_PID > ./bin/kubectl_proxy_pid
