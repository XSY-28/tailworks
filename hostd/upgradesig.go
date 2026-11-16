package hostd

import (
	"fmt"

	"github.com/rosskukulinski/twd/internal/releasesig"
)

// verifyRelease checks an extracted release directory before its
// install.sh runs (issue #71): SHA256SUMS must be signed by a release key
// pinned in this binary (internal/releasesig), not one twd-mgmt names, and
// every file must match it. The SHA-256 in the upgrade order only proves
// the download matches what mgmt holds; a compromised mgmt could send a
// matching tarball and hash, but it can't sign one.
func (h *Host) verifyRelease(dir string) error {
	keys := h.upg.signers
	if keys == nil {
		var err error
		if keys, err = releasesig.Pinned(); err != nil {
			return fmt.Errorf("the pinned release keys: %w", err)
		}
	}
	if err := releasesig.VerifyTree(dir, keys); err != nil {
		return fmt.Errorf("refusing an unverified release: %w", err)
	}
	return nil
}
