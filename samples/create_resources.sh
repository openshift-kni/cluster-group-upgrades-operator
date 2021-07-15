#!/bin/bash

oc create -f common.yaml
oc create -f site1.yaml
oc create -f site2.yaml
oc create -f group1.yaml
