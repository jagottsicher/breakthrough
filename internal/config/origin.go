package config

// Origin says which of the two config tiers an effective setting value
// actually came from — or that neither set it and the built-in default
// is what's in force.
//
// Exists for the Options screen (see internal/ui), which shows this per
// setting and needs it for one concrete reason beyond curiosity: it's
// what makes "reset to default" honest. Resetting removes the key from
// the *user* file, so the value falls back through the tiers — landing
// on a system administrator's value where there is one, and only on
// OriginDefault where there isn't. Without showing the origin, that
// distinction would be invisible and the result would look arbitrary.
type Origin int

const (
	// OriginDefault: neither config file set this key, so
	// DefaultSettings' own built-in value is in force.
	OriginDefault Origin = iota
	// OriginSystem: the system-wide file set it (see SystemConfigFile)
	// and the user's own file did not.
	OriginSystem
	// OriginUser: the user's own file set it (see UserConfigFile),
	// overriding whatever the tiers below had.
	OriginUser
)

// String renders an Origin as the short label a settings UI shows next
// to a value.
func (o Origin) String() string {
	switch o {
	case OriginSystem:
		return "system-wide"
	case OriginUser:
		return "changed by you"
	default:
		return "default"
	}
}

// LoadWithOrigins is Load (see its own doc comment for the merge
// itself) plus, for every recognized key, which tier the effective
// value actually came from.
//
// A key present in a file but rejected by apply — a malformed boolean,
// say — does not count as having set anything: it's reported as a
// warning and the tier below stays in force, so its origin has to
// reflect that too rather than crediting the tier that failed. That's
// why origins are recorded from apply's own success, not merely from
// the key being present in the parsed file.
func LoadWithOrigins(systemPath, userPath string) (Settings, map[string]Origin, []string) {
	s := DefaultSettings()
	origins := map[string]Origin{}
	for _, doc := range SettingDocs() {
		origins[doc.Key] = OriginDefault
	}
	var warnings []string

	merge := func(path string, origin Origin) {
		values, parseWarnings, err := ParseFile(path)
		warnings = append(warnings, parseWarnings...)
		if err != nil {
			warnings = append(warnings, path+": "+err.Error())
			return
		}
		for _, k := range sortedKeys(values) {
			if err := s.apply(k, values[k]); err != nil {
				warnings = append(warnings, path+": "+err.Error())
				continue
			}
			origins[k] = origin
		}
	}
	merge(systemPath, OriginSystem)
	merge(userPath, OriginUser)

	return s, origins, warnings
}
