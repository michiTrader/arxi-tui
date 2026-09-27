package ext

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
)

// The behavioral package installer (DESIGN-BLOCK-I §I-I). This file is the two
// pieces §I-I puts at the center of the design and that a signed version needs
// unchanged: extraction as the security boundary (Decision 3) and the
// digest-keyed atomic lay-out (Decision 4). Both operate on an io.Reader and a
// destination path, so they are pure and testable offline — the HTTP fetch that
// feeds them and the supervisor.Mount they feed live at the edges, exactly as
// PackageDigest takes a root rather than reaching for a URL or a home directory.
//
// Why extraction is where the refusals live, not the digest: PackageDigest
// (identity.go) refuses symlinks and non-regular files too, but by the time it
// runs the bytes are already on disk. A tar/zip-slip entry writing outside the
// root, or a decompression bomb, is endpoint harm the digest cannot undo because
// it strikes before the digest reads a byte. So the same refusals PackageDigest
// makes are made *earlier*, at write time, and a bundle that would fail the
// digest walk is rejected here first — two rules where §I-I insists there is one.

// installMaxBytes caps the total decompressed size of a bundle. A real
// behavioral plugin is an executable plus a small manifest; a compiled Go binary
// runs to tens of megabytes, so the cap is generous for a legitimate package and
// small enough that a gzip bomb — which inflates without bound from a tiny
// archive — is cut off long before it fills the disk. It mirrors the manifest
// fetch's 1 MiB cap in spirit: the number differs because a package legitimately
// carries a binary and a declarative manifest never does.
const installMaxBytes int64 = 128 << 20 // 128 MiB

// installMaxEntries caps the number of tar entries. It bounds the many-tiny-files
// shape of a bomb that installMaxBytes alone would miss (each file empty, so no
// bytes accrue, but the inode and path work is unbounded), and no honest plugin
// approaches it.
const installMaxEntries int = 4096

// Installer carries the extraction limits in one place, the way
// httpManifestFetcher carries the fetch limits, so a later change to a cap is a
// single edit a reviewer sees rather than a constant defaulted at each call site.
type Installer struct {
	MaxBytes   int64
	MaxEntries int
}

// NewInstaller builds an installer with the host's chosen caps.
func NewInstaller() *Installer {
	return &Installer{MaxBytes: installMaxBytes, MaxEntries: installMaxEntries}
}

// Extract writes the gzipped tar read from r into dest, enforcing §I-I
// Decision 3's lay-out invariants as each entry is written. It refuses rather
// than repairs: a bundle that trips any invariant is rejected with a message
// naming the offending entry, because a package the host had to sanitise is a
// package whose bytes no longer match what its author signed and its consumer
// consented to.
//
// dest must already exist and be empty; the caller owns its creation and cleanup
// so a failed extraction leaves a temp tree the caller removes, never a
// half-written entry in the live cache (Decision 4's atomicity depends on that
// separation).
func (in *Installer) Extract(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("plugin bundle is not valid gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	// remaining is the decompression budget shared across every entry, so a bomb
	// spread over many files is bounded by the same cap as one enormous file.
	remaining := in.MaxBytes
	entries := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading plugin bundle: %w", err)
		}

		entries++
		if entries > in.MaxEntries {
			return fmt.Errorf("plugin bundle has more than %d entries; refused before extraction completes, because an unbounded entry count is a decompression bomb the byte cap alone does not catch (DESIGN-BLOCK-I §I-I)", in.MaxEntries)
		}

		target, err := in.resolveEntry(dest, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("creating bundle directory %q: %w", hdr.Name, err)
			}
		case tar.TypeReg:
			written, err := in.writeRegular(tr, target, hdr, remaining)
			if err != nil {
				return err
			}
			remaining -= written
		default:
			// Symlinks, hardlinks, devices and FIFOs are refused, mirroring
			// PackageDigest's own refusal (identity.go) so the two agree: a symlink
			// points at bytes the digest never read, so a grant bound to this tree
			// would cover content the consent screen never showed (§I-H). Naming the
			// entry and its type flag tells the author which file to replace with a
			// regular one.
			return fmt.Errorf("plugin bundle entry %q is not a regular file or directory (tar type %q); only regular files and directories are extracted, because a symlink, hardlink or device points at bytes outside the digested tree that the consent grant never covered (DESIGN-BLOCK-I §I-H)", hdr.Name, string(rune(hdr.Typeflag)))
		}
	}
	return nil
}

// resolveEntry turns a tar entry name into an absolute path under dest, refusing
// any name that would escape the root. filepath.IsLocal is the stdlib predicate
// §I-I names for exactly this: it rejects an absolute path, a `..` that climbs
// out, and — on Windows, a build target — a drive-relative path or a reserved
// device name. The name is path.Clean'd in slash form first (tar names are
// always slash-separated) so a trailing slash or an internal `.` does not defeat
// the check, then converted to the OS form IsLocal and the filesystem expect.
func (in *Installer) resolveEntry(dest, name string) (string, error) {
	cleaned := path.Clean(name)
	local := filepath.FromSlash(cleaned)
	if !filepath.IsLocal(local) {
		return "", fmt.Errorf("plugin bundle entry %q escapes the package root; a path with `..`, an absolute path, or a Windows drive-relative or reserved name is refused at write time, because by digest time the write outside the root has already happened (DESIGN-BLOCK-I §I-I)", name)
	}
	return filepath.Join(dest, local), nil
}

// writeRegular streams one regular file out of the tar under the shared
// decompression budget, returning the number of bytes written so Extract can
// subtract them. It copies at most budget+1 bytes and refuses the moment the
// bundle would exceed the cap, so the offending file is cut off mid-copy rather
// than fully written and then measured — the bomb never lands on disk in full.
func (in *Installer) writeRegular(tr *tar.Reader, target string, hdr *tar.Header, budget int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, fmt.Errorf("creating parent of bundle file %q: %w", hdr.Name, err)
	}
	// The executable bit is preserved from the archive so a bundled binary stays
	// runnable; PackageDigest records that bit, so consent is bound to it and a
	// bundle cannot flip a file to executable after the grant.
	mode := os.FileMode(0o644)
	if hdr.Mode&0o111 != 0 {
		mode = 0o755
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return 0, fmt.Errorf("creating bundle file %q: %w", hdr.Name, err)
	}
	defer f.Close()

	// Copy one byte past the budget so a bundle that is exactly at the cap is
	// accepted and one larger is refused, the same off-by-one guard the manifest
	// fetch uses to tell a legal maximum from an over-large stream.
	n, err := io.CopyN(f, tr, budget+1)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("writing bundle file %q: %w", hdr.Name, err)
	}
	if n > budget {
		return 0, fmt.Errorf("plugin bundle exceeds the %d-byte extraction cap; refused mid-write, because a decompression bomb inflates from a tiny archive and must be cut off before it fills the disk, not measured after (DESIGN-BLOCK-I §I-I)", in.MaxBytes)
	}
	return n, nil
}

// LayoutByDigest digests the already-extracted tree at src and atomically moves
// it to root/<id>/<digest>/, returning the final path and the digest the grant
// will bind to. It is §I-I Decision 4, and the ordering is the whole point:
//
//   - the digest is computed over src BEFORE the move, and the destination is
//     *named by that digest*, so what was hashed is bit-for-bit what is kept and
//     later spawned — nothing can be swapped between the hash and the mount, which
//     closes the TOCTOU window a hash-then-locate-then-run sequence would open;
//   - the move is a rename, so a crash never leaves a half-tree in the live cache
//     that digests to a phantom — the same reason DiskConsentStore writes a
//     security record atomically (consent_disk.go);
//   - the destination is keyed by digest, so identical bytes re-install to the
//     same path idempotently: if it already exists, the extracted temp is the same
//     package already on record and the move is skipped rather than failed.
//
// src and root must be on the same filesystem for the rename to be atomic; the
// caller guarantees this by creating the temp tree under root (Decision 4).
func (in *Installer) LayoutByDigest(src, root, id string) (finalPath, digest string, err error) {
	digest, err = PackageDigest(src)
	if err != nil {
		return "", "", err
	}
	finalPath = filepath.Join(root, id, digest)

	// A pre-existing target is the same bytes under the same digest: consent may
	// already be remembered against it, and re-extracting over it would be a torn
	// write to a tree a live grant references. Treat it as installed and keep the
	// bytes already there.
	if _, statErr := os.Stat(finalPath); statErr == nil {
		return finalPath, digest, nil
	}

	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return "", "", fmt.Errorf("creating plugin cache directory for %q: %w", id, err)
	}
	if err := os.Rename(src, finalPath); err != nil {
		return "", "", fmt.Errorf("installing plugin %q into the cache: %w", id, err)
	}
	return finalPath, digest, nil
}
