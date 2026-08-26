package viewer

// Result is what Load returns: Kind decides how internal/ui presents the
// file (see showBuiltinLook); Content and Truncated are only meaningful
// when Kind is KindText — both are zero for KindUnsupported, which never
// carries any of the file's actual (binary) content back to the caller.
type Result struct {
	Kind      Kind
	Content   string
	Truncated bool
}

// Load reads up to limit bytes of path (see ReadPreview) and classifies
// them (see Sniff), in one call — internal/ui's openLook/showBuiltinLook
// is the only real caller, wanting exactly this combination rather than
// two separate round-trips through the same file.
func Load(path string, limit int64) (Result, error) {
	data, truncated, err := ReadPreview(path, limit)
	if err != nil {
		return Result{}, err
	}

	sample := data
	if len(sample) > sniffLen {
		sample = sample[:sniffLen]
	}
	if Sniff(sample) != KindText {
		return Result{Kind: KindUnsupported}, nil
	}
	return Result{Kind: KindText, Content: string(data), Truncated: truncated}, nil
}
