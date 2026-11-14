//go:build !linux

package deploy

func publishComposeBirthLaunchPair(string, composeBirthLaunchPair) error {
	return errComposeBirthLaunch
}
