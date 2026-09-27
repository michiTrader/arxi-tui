package ext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Identity and PackageDigest compute the tuple a persisted consent grant is
// bound to (DESIGN-BLOCK-I §I-H, Q15). The whole contract is inherited by copy
// from arxi-sim (`internal/ext/identity.go`), never imported across repos, for
// the same reason the supervisor is ported: a cross-repo import couples two
// disposable trees, and the identity rule is small enough that a copy with its
// argument written down is safer than a dependency.
//
// The tuple is name + version + protocol + executable + args + capability-set +
// digest. Each field earns its place: version so a bump re-asks; executable and
// args so swapping the program the grant runs re-asks; the capability set (order
// irrelevant, exact membership) so a manifest cannot silently widen its powers
// under a remembered grant; and the digest so the grant binds to the exact bytes
// fetched, not to a name a later package could reuse. Drop any one and a grant
// remembered for a benign plugin transfers to a different program.

// consentIdentity is the deterministic pre-image of the identity hash. It exists
// as a struct only so the JSON encoding is stable and every field is named — a
// positional concatenation would silently reorder if a field were inserted, and
// a reordered identity is a grant that no longer matches the plugin it was made
// for. Capabilities are sorted before it is built, so membership decides the
// identity and declaration order does not.
type consentIdentity struct {
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Protocol      string   `json:"protocol"`
	Executable    string   `json:"executable"`
	PackageDigest string   `json:"package_digest"`
	Args          []string `json:"args"`
	Capabilities  []string `json:"capabilities"`
}

// Identity returns the deterministic consent identity for a manifest bound to a
// package digest. The digest is passed in rather than read here because it is
// computed by the loader over the fetched package (PackageDigest), and the
// manifest alone cannot vouch for the bytes it shipped beside — "an executable
// outside the package is code the digest never covered" (DESIGN-BLOCK-H.md).
//
// Args and Capabilities are copied before sorting so a caller's slice is never
// reordered underneath it: Identity is a pure read of the manifest and must not
// mutate the argument a live plugin is still using.
func Identity(m *Manifest, packageDigest string) string {
	caps := append([]string(nil), m.Capabilities...)
	sort.Strings(caps)
	body, _ := json.Marshal(consentIdentity{
		Name:          m.Name,
		Version:       m.Version,
		Protocol:      m.Protocol,
		Executable:    m.Executable,
		PackageDigest: packageDigest,
		Args:          append([]string(nil), m.Args...),
		Capabilities:  caps,
	})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// PackageDigest hashes a fetched plugin package tree canonically, so the same
// bytes always produce the same digest regardless of the order the filesystem
// walks them or the temp directory they landed in. Each regular file
// contributes its slash-normalised relative path, an executable-bit marker, its
// byte length and its bytes; the length is framed in so two files cannot be
// confused by concatenation (a/"xy"+b/"" hashing the same as a/"x"+b/"y").
//
// Symlinks and other non-regular entries are refused rather than followed: a
// symlink is a pointer to bytes the digest did not read, so a package that
// smuggled its real executable behind a link would be granted consent for a
// tree whose content the user never saw. Directories affect the digest only
// through the paths of the files under them, which is enough — an empty
// directory carries no code.
func PackageDigest(root string) (string, error) {
	var paths []string
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			if !entry.IsDir() {
				return fmt.Errorf("%s: package root is not a directory; the digest is computed over a package tree, and a single file has no canonical tree", root)
			}
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: package entry is a symlink; a symlink points at bytes the digest did not read, so a grant bound to this tree would cover content the consent screen never showed (DESIGN-BLOCK-I §I-H)", path)
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s: package entry is not a regular file; only regular files carry the bytes a consent grant is bound to", path)
		}
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	sort.Slice(paths, func(i, j int) bool {
		return filepath.ToSlash(paths[i]) < filepath.ToSlash(paths[j])
	})
	h := sha256.New()
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
		exec := "0"
		if info.Mode().Perm()&0o111 != 0 {
			exec = "1"
		}
		fmt.Fprintf(h, "%s\x00regular\x00%s\x00%d\x00", strings.ReplaceAll(filepath.ToSlash(rel), "\\", "/"), exec, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
