package fsops

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/blake2b"
)

// Hashes holds hex-encoded fingerprints of one file, as computed by Hash.
// MD5 and SHA-1 are included despite being cryptographically broken:
// they're still the digests most existing checksums (release pages,
// deduplication tools, older SHASUMS files) were published with, so
// leaving them out would make Hash less useful for the fingerprinting/
// verification use case it's for, not more secure. Blake2 (BLAKE2b,
// 512-bit output) is the one digest here that isn't in the standard
// library at all — it comes from golang.org/x/crypto instead, the Go
// team's own extended crypto module and already a sibling of
// golang.org/x/image/golang.org/x/term this project already depends
// on, not a third-party dependency in the usual sense.
type Hashes struct {
	MD5    string
	SHA1   string
	SHA256 string
	SHA512 string
	Blake2 string
}

// Hash computes MD5, SHA-1, SHA-256, SHA-512, and Blake2 (BLAKE2b-512)
// digests of the file at path in a single streaming pass —
// io.MultiWriter fans the same read out to all five hash.Hash
// instances, so the file is only read once regardless of its size,
// rather than five times over.
//
// ctx is checked before every underlying Read (see progressReader) —
// once it's cancelled, the very next Read returns ctx.Err() instead of
// reading further, io.Copy stops there, and Hash returns (Hashes{},
// ctx.Err()) almost immediately, rather than reading the rest of a
// possibly large file for a result the caller has already stopped
// caring about. A caller not interested in cancellation still has to
// pass a real, non-nil context — context.Background() costs nothing —
// there's no separate nil-means-uncancellable path to keep in sync with
// this one.
//
// If onProgress is non-nil, it's called after every underlying Read
// that actually returned bytes with the number streamed so far, so a
// caller can show a percentage (the total is whatever os.Stat reported
// before reading started — see its own caller for how a file that
// changes size mid-read is handled). It's called synchronously, on
// whatever goroutine is calling Hash, and should stay cheap (e.g. an
// atomic store) — Hash doesn't rate-limit these calls itself,
// deliberately leaving that to the caller, which is free to sample the
// counter on its own schedule instead (see animateHashProgress in
// internal/ui). It's never called again once ctx is cancelled, for the
// same reason a cancelled computation shouldn't keep reading: a caller
// tracking progress in shared state (see Root.hashBytesRead) that a
// *new*, unrelated Hash call might start writing to concurrently must
// be able to trust that a cancelled one has genuinely stopped touching
// it, not merely stopped being reported — otherwise the two visibly
// race over whatever's displaying it (a real bug this scenario itself
// used to cause, before ctx existed here to fix it at the source: an
// "aborted" Hash kept right on reading and reporting progress for as
// long as its own read loop happened to take, however long after the
// caller had already moved on and started a new one).
//
// It follows symlinks (os.Stat, not os.Lstat) and refuses directories,
// including a symlink that resolves to one: there's no single canonical
// "directory hash", and silently picking a convention (e.g. hashing
// concatenated contents) would be more likely to surprise a fingerprinting
// use case than help it.
func Hash(ctx context.Context, path string, onProgress func(readBytes int64)) (Hashes, error) {
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

	reader := &progressReader{r: f, ctx: ctx, onProgress: onProgress}

	// blake2b.New512's own error is only ever non-nil for an invalid key
	// length — never true for the nil we always pass here (the plain,
	// unkeyed hash mode every other algorithm below also is).
	blake2h, err := blake2b.New512(nil)
	if err != nil {
		return Hashes{}, err
	}

	md5h, sha1h, sha256h, sha512h := md5.New(), sha1.New(), sha256.New(), sha512.New()
	w := io.MultiWriter(md5h, sha1h, sha256h, sha512h, blake2h)
	if _, err := io.Copy(w, reader); err != nil {
		return Hashes{}, err
	}

	return Hashes{
		MD5:    hex.EncodeToString(md5h.Sum(nil)),
		SHA1:   hex.EncodeToString(sha1h.Sum(nil)),
		SHA256: hex.EncodeToString(sha256h.Sum(nil)),
		SHA512: hex.EncodeToString(sha512h.Sum(nil)),
		Blake2: hex.EncodeToString(blake2h.Sum(nil)),
	}, nil
}

// progressReader wraps an io.Reader, stopping — see Read — as soon as
// ctx is cancelled, and reporting the running total of bytes read so
// far via onProgress (if non-nil) after every underlying Read that
// returned any — see Hash's own doc comment on both.
type progressReader struct {
	r          io.Reader
	ctx        context.Context
	read       int64
	onProgress func(readBytes int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err // checked before reading further, not just before reporting — see Hash's own doc comment on why that distinction is the actual fix
	}
	n, err := p.r.Read(b)
	if n > 0 {
		p.read += int64(n)
		if p.onProgress != nil {
			p.onProgress(p.read)
		}
	}
	return n, err
}
