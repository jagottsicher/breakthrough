package fsops

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Hashes holds hex-encoded fingerprints of one file, as computed by Hash.
// MD5 and SHA-1 are included despite being cryptographically broken:
// they're still the digests most existing checksums (release pages,
// deduplication tools, older SHASUMS files) were published with, so
// leaving them out would make Hash less useful for the fingerprinting/
// verification use case it's for, not more secure.
type Hashes struct {
	MD5    string
	SHA1   string
	SHA256 string
}

// Hash computes MD5, SHA-1, and SHA-256 digests of the file at path in a
// single streaming pass — io.MultiWriter fans the same read out to all
// three hash.Hash instances, so the file is only read once regardless of
// its size, rather than three times over.
//
// It follows symlinks (os.Stat, not os.Lstat) and refuses directories,
// including a symlink that resolves to one: there's no single canonical
// "directory hash", and silently picking a convention (e.g. hashing
// concatenated contents) would be more likely to surprise a fingerprinting
// use case than help it.
func Hash(path string) (Hashes, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Hashes{}, err
	}
	if fi.IsDir() {
		return Hashes{}, fmt.Errorf("fsops: %s is a directory, hashing is only supported for files", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return Hashes{}, err
	}
	defer func() { _ = f.Close() }() // read-only; nothing to do if this fails

	md5h, sha1h, sha256h := md5.New(), sha1.New(), sha256.New()
	if _, err := io.Copy(io.MultiWriter(md5h, sha1h, sha256h), f); err != nil {
		return Hashes{}, err
	}

	return Hashes{
		MD5:    hex.EncodeToString(md5h.Sum(nil)),
		SHA1:   hex.EncodeToString(sha1h.Sum(nil)),
		SHA256: hex.EncodeToString(sha256h.Sum(nil)),
	}, nil
}
