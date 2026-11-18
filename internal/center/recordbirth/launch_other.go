//go:build !linux

package recordbirth

import (
	"fmt"
	"os"
)

func openExecutableForDigest(path string) (*os.File, error) {
	return os.Open(path)
}

func executableMetadataUnchanged(a, b os.FileInfo) bool {
	return true
}

// LoadRuntimeLaunchBinding fails closed where Linux openat2 is unavailable.
func LoadRuntimeLaunchBinding(path, processKind string) (LaunchBinding, [32]byte, error) {
	return LaunchBinding{}, [32]byte{}, fmt.Errorf("%w: launch binding requires Linux", ErrUnavailable)
}

// ReadPublishedLaunchBinding fails closed where Linux openat2 is unavailable.
func ReadPublishedLaunchBinding(path, processKind string) (LaunchBinding, [32]byte, error) {
	return LaunchBinding{}, [32]byte{}, fmt.Errorf("%w: launch binding requires Linux", ErrUnavailable)
}
