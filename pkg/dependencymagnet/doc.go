//go:build tools
// +build tools

// Package dependencymagnet is a package for setting dependencies used by extra-build tooling.
// go mod won't pull in code that isn't depended upon, but we have some code we don't depend on from code that must be included
// for our build to work.
package dependencymagnet

import (
	_ "github.com/go-bindata/go-bindata/go-bindata" // Used for generating Go source
	_ "github.com/openshift/build-machinery-go"     // Used for Makefile
	_ "k8s.io/code-generator"                       // used for generating clientset code
)
