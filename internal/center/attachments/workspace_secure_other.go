//go:build !linux

package attachments

func openSecureWorkspaceRoot(string) (secureProcessorWorkspaceRoot, error) {
	return nil, ErrUnsafeProcessorWorkspace
}
