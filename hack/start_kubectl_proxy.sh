#!/bin/bash

kubectl proxy start &
KUBECTL_PROXY_PID=$!
echo $KUBECTL_PROXY_PID > kubectl_proxy_pid
