//go:build !linux

package main

import "errors"

func runComposeStoragePreparation([]string) error {
	return errors.New("Compose storage guard preparation requires Linux")
}
