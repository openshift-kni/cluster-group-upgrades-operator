# Cluster Group LCM operator

## What is

Cluster Group LCM operator is a Kubernetes operator that facilitates software lifecycle management of fleets of clusters. It uses Red Hat Advanced Cluster Management (RHACM) for performing changes on target clusters, in particular by using RHACM policies.
Cluster Group LCM uses the following CRs as abstractions for defining state of clusters:

* Common
* Site
* Group

A Common CR defines ACM policies that are common to all clusters managed by ACM.
A Site CR defines ACM policies that are specific to a particular cluster managed by ACM.
A Group CR defines ACM policies that are applicable to the sites belonging to it.

By setting the **remediationStrategy** spec field of the Group CR, you can specify how many remediations can be performed concurrently. That way you can perform remediations serially (one cluster after another), in parallel (all clusters at once) or a in batches of N clusters. It also allows to specify a list of canary Site objects, which will be remediated before the rest of Site objects of the Group.
Check files within **samples** folder for examples.

## How to deploy

1. Run **make docker-build docker-push IMG=*your_repo_image***
2. Run **make deploy IMG=*your_repo_image***

## How to test

1. Export **KUBECONFIG** environment variable to point to your cluster running RHACM
2. Run **cd integration_tests/*scenario***
3. Run **test.sh**

## How to develop

1. Export **KUBECONFIG** environment variable to point to your cluster running RHACM
2. Run **make install run**