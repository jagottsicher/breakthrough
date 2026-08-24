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
// If onProgress is non-nil, it's called after every underlying Read with
// the number of bytes streamed so far, so a caller can show a percentage
// (the total is whatever os.Stat reported before reading started — see
// its own caller for how a file that changes size mid-read is handled).
// It's called synchronously, on whatever goroutine is calling Hash, and
// should stay cheap (e.g. an atomic store) — Hash doesn't rate-limit
// these calls itself, deliberately leaving that to the caller, which is
// free to sample the counter on its own schedule instead (see
// animateHashProgress in internal/ui).
//
// It follows symlinks (os.Stat, not os.Lstat) and refuses directories,
// including a symlink that resolves to one: there's no single canonical
// "directory hash", and silently picking a convention (e.g. hashing
// concatenated contents) would be more likely to surprise a fingerprinting
// use case than help it.
func Hash(path string, onProgress func(readBytes int64)) (Hashes, error) {
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

	var reader io.Reader = f
	if onProgress != nil {
		reader = &progressReader{r: f, onProgress: onProgress}
	}

	md5h, sha1h, sha256h := md5.New(), sha1.New(), sha256.New()
	if _, err := io.Copy(io.MultiWriter(md5h, sha1h, sha256h), reader); err != nil {
		return Hashes{}, err
	}

	return Hashes{
		MD5:    hex.EncodeToString(md5h.Sum(nil)),
		SHA1:   hex.EncodeToString(sha1h.Sum(nil)),
		SHA256: hex.EncodeToString(sha256h.Sum(nil)),
	}, nil
}

// progressReader wraps an io.Reader, reporting the running total of
// bytes read so far via onProgress after every underlying Read — see
// Hash's own doc comment on why that's the caller's job to rate-limit,
// not this type's.
type progressReader struct {
	r          io.Reader
	read       int64
	onProgress func(readBytes int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.read += int64(n)
		p.onProgress(p.read)
	}
	return n, err
}
