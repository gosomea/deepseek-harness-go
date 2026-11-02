package sessionjsonl

import (
	"fmt"
	"os"
	"path/filepath"
)

// LeaseSuffix is the suffix of the writer-lease file for a session.
const LeaseSuffix = ".writer.lock"

// claimLease creates the writer lease for a session on disk.
//
// In-process bookkeeping cannot stop a second *process* from opening the same
// session, so the claim is also recorded as an exclusively created file: the
// create fails if the file exists, which is what makes the single-writer rule
// hold across processes rather than only within one.
func claimLease(root string, id string) (string, error) {
	path, err := leasePath(root, id)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("sessionjsonl: another writer holds %s", id)
		}
		return "", err
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// releaseLease removes the writer lease. It is idempotent.
func releaseLease(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// leasePath returns the lease file path for a session.
func leasePath(root string, id string) (string, error) {
	path, err := SessionPath(root, id)
	if err != nil {
		return "", err
	}
	return path + LeaseSuffix, nil
}

// leaseFileFor returns the lease path that claimLease would create. It exists so
// a test can assert the lease was removed without recomputing the path.
func leaseFileFor(root, id string) string {
	path, err := leasePath(root, id)
	if err != nil {
		return ""
	}
	return filepath.Clean(path)
}
