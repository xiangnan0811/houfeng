//go:build !linux

package main

import "errors"

func runOrdinaryTargetContract([]string) error {
	return errors.New("ordinary target contract requires Linux")
}

func runOrdinaryTargetLauncher([]string) error {
	return errors.New("ordinary target launcher requires Linux")
}
