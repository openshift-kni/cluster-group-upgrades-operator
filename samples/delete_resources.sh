#!/bin/bash

oc delete -f common.yaml
oc delete -f site1.yaml
oc delete -f site2.yaml
oc delete -f group1.yaml
