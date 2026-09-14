package ui

import (
	"strconv"

	"github.com/jagottsicher/breakthrough/internal/batchrename"
)

// batchRenameFieldKind is what shape one field's value has — the same
// coarse "how is this entered" distinction config.SettingKind makes for
// the Options screen, reused here because the interaction is identical:
// a boolean toggles in place, an enum cycles in place, a number opens a
// one-line typed editor.
type batchRenameFieldKind int

const (
	brFieldString batchRenameFieldKind = iota
	brFieldBool
	brFieldInt
	brFieldEnum
)

// batchRenameChoice is one option of an enum field — the value stored
// on r.batchRenameRules and the label shown for it, the same split
// optionChoice already makes for the Options screen's own enums.
type batchRenameChoice struct {
	value, label string
}

// batchRenameField is one row of a step's own settings table: a label,
// what kind of value it takes, and how to read/write it on
// r.batchRenameRules. Modeled directly on optionSpec (see
// optioncatalog.go) — same shape, because the table that renders it
// (see batchrename.go) is the same table, reused for a Rules value
// instead of a config.Settings one.
type batchRenameField struct {
	label   string
	kind    batchRenameFieldKind
	value   func(r *Root) string
	apply   func(r *Root, value string)
	choices func(r *Root) []batchRenameChoice // only consulted for brFieldEnum

	// help is the one- or two-line explanation shown live under the
	// fields table for whichever field is currently selected (see
	// renderBatchRenameFieldHelp) — always visible as the cursor moves,
	// rather than behind an info button the way the Options screen's
	// own longer per-setting docs are: a rename rule needs one sentence
	// of "what does this do to a name", not a config-key reference.
	help string
}

// batchRenameStep is one entry of the left-hand "tabs" list and the
// fields shown on its own settings table once selected. active reports
// whether the step currently does anything at all under r's own rules
// — the list marks such steps (see renderBatchRenameStepsList), so the
// shape of the whole pipeline is readable at a glance without opening
// each step to check.
type batchRenameStep struct {
	name   string
	fields []batchRenameField
	active func(rules batchrename.Rules) bool
}

// caseModeValues/parseCaseMode/numberPositionValues/
// parseNumberPosition/extensionModeValues/parseExtensionMode convert
// batchrename's own small int-backed enums to and from the plain
// strings batchRenameField's value()/apply() (and the shared enum-cycle
// logic they share with optionSpec's — see cycleBatchRenameChoice) deal
// in, the same string-keyed convention the config package's own enums
// (color_scheme, pager) already use.

func caseModeValue(m batchrename.CaseMode) string {
	switch m {
	case batchrename.CaseUpper:
		return "upper"
	case batchrename.CaseLower:
		return "lower"
	case batchrename.CaseTitle:
		return "title"
	case batchrename.CaseSentence:
		return "sentence"
	default:
		return "none"
	}
}

func parseCaseMode(v string) batchrename.CaseMode {
	switch v {
	case "upper":
		return batchrename.CaseUpper
	case "lower":
		return batchrename.CaseLower
	case "title":
		return batchrename.CaseTitle
	case "sentence":
		return batchrename.CaseSentence
	default:
		return batchrename.CaseNone
	}
}

func caseModeChoices(*Root) []batchRenameChoice {
	return []batchRenameChoice{
		{"none", "Unchanged"},
		{"upper", "UPPERCASE"},
		{"lower", "lowercase"},
		{"title", "Title Case"},
		{"sentence", "Sentence case"},
	}
}

func numberPositionValue(p batchrename.NumberPosition) string {
	switch p {
	case batchrename.NumberPrefix:
		return "prefix"
	case batchrename.NumberSuffix:
		return "suffix"
	default:
		return "none"
	}
}

func parseNumberPosition(v string) batchrename.NumberPosition {
	switch v {
	case "prefix":
		return batchrename.NumberPrefix
	case "suffix":
		return batchrename.NumberSuffix
	default:
		return batchrename.NumberNone
	}
}

func numberPositionChoices(*Root) []batchRenameChoice {
	return []batchRenameChoice{
		{"none", "None"},
		{"prefix", "Prefix"},
		{"suffix", "Suffix"},
	}
}

func extensionModeValue(m batchrename.ExtensionMode) string {
	switch m {
	case batchrename.ExtensionLower:
		return "lower"
	case batchrename.ExtensionUpper:
		return "upper"
	case batchrename.ExtensionRemove:
		return "remove"
	case batchrename.ExtensionSetTo:
		return "set"
	default:
		return "keep"
	}
}

func parseExtensionMode(v string) batchrename.ExtensionMode {
	switch v {
	case "lower":
		return batchrename.ExtensionLower
	case "upper":
		return batchrename.ExtensionUpper
	case "remove":
		return batchrename.ExtensionRemove
	case "set":
		return batchrename.ExtensionSetTo
	default:
		return batchrename.ExtensionKeep
	}
}

func extensionModeChoices(*Root) []batchRenameChoice {
	return []batchRenameChoice{
		{"keep", "Keep"},
		{"lower", "lowercase"},
		{"upper", "UPPERCASE"},
		{"remove", "Remove"},
		{"set", "Set to..."},
	}
}

// intField/stringField/boolField/enumField build one batchRenameField
// from a get/set pair — kept as small builder functions (the same
// pattern boolOption/intOption already establish in optioncatalog.go)
// so batchRenameSteps' own table below reads as a plain list of what
// each step has, not how the plumbing works.

func intField(label, help string, get func(r *Root) int, set func(r *Root, v int)) batchRenameField {
	return batchRenameField{
		label: label,
		help:  help,
		kind:  brFieldInt,
		value: func(r *Root) string { return strconv.Itoa(get(r)) },
		apply: func(r *Root, value string) {
			n, err := strconv.Atoi(value)
			if err != nil {
				return // the input field's own acceptance func already rejects non-digits as they're typed
			}
			set(r, n)
			r.renderBatchRenamePreview()
		},
	}
}

func stringField(label, help string, get func(r *Root) string, set func(r *Root, v string)) batchRenameField {
	return batchRenameField{
		label: label,
		help:  help,
		kind:  brFieldString,
		value: get,
		apply: func(r *Root, value string) {
			set(r, value)
			r.renderBatchRenamePreview()
		},
	}
}

func boolField(label, help string, get func(r *Root) bool, set func(r *Root, v bool)) batchRenameField {
	return batchRenameField{
		label: label,
		help:  help,
		kind:  brFieldBool,
		value: func(r *Root) string { return strconv.FormatBool(get(r)) },
		apply: func(r *Root, value string) {
			set(r, value == "true")
			r.renderBatchRenamePreview()
		},
	}
}

func enumField(label, help string, choices func(r *Root) []batchRenameChoice, get func(r *Root) string, set func(r *Root, v string)) batchRenameField {
	return batchRenameField{
		label:   label,
		help:    help,
		kind:    brFieldEnum,
		choices: choices,
		value:   get,
		apply: func(r *Root, value string) {
			set(r, value)
			r.renderBatchRenamePreview()
		},
	}
}

// batchRenameSteps is the whole pipeline, in the fixed order it's
// actually applied (see batchrename's own package doc) — the left-hand
// "tabs" list is exactly this list's own names, in this order.
func batchRenameSteps() []batchRenameStep {
	return []batchRenameStep{
		{
			name:   "Search & Replace",
			active: func(rules batchrename.Rules) bool { return rules.Find != "" },
			fields: []batchRenameField{
				stringField("Find",
					"Text to look for in the name (not the extension). Every occurrence is replaced. Leave empty to skip this step.",
					func(r *Root) string { return r.batchRenameRules.Find },
					func(r *Root, v string) { r.batchRenameRules.Find = v }),
				stringField("Replace with",
					"What each match becomes — may be empty to delete the match. With Regex on, $1 / ${name} / \\1 refer to capture groups.",
					func(r *Root) string { return r.batchRenameRules.Replace },
					func(r *Root, v string) { r.batchRenameRules.Replace = v }),
				boolField("Regex (Find is a pattern, not literal text)",
					"Treat Find as a Go regular expression: ^ $ . * + ? [ ] ( ) | \\d \\w \\s all work. Off: Find is matched literally, case-sensitive.",
					func(r *Root) bool { return r.batchRenameRules.Regex },
					func(r *Root, v bool) { r.batchRenameRules.Regex = v }),
			},
		},
		{
			name:   "Case",
			active: func(rules batchrename.Rules) bool { return rules.Case != batchrename.CaseNone },
			fields: []batchRenameField{
				enumField("Change case to",
					"Applies to the name only, never the extension (see the Extension step for that). Title Case capitalizes each word; Sentence case only the first letter.",
					caseModeChoices,
					func(r *Root) string { return caseModeValue(r.batchRenameRules.Case) },
					func(r *Root, v string) { r.batchRenameRules.Case = parseCaseMode(v) }),
			},
		},
		{
			name:   "Trim",
			active: func(rules batchrename.Rules) bool { return rules.TrimFront > 0 || rules.TrimBack > 0 },
			fields: []batchRenameField{
				intField("Characters off the front",
					"How many characters to drop from the start of the name. Runs after Search & Replace and Case, before Numbering.",
					func(r *Root) int { return r.batchRenameRules.TrimFront },
					func(r *Root, v int) { r.batchRenameRules.TrimFront = v }),
				intField("Characters off the back",
					"How many characters to drop from the end of the name, extension excluded. A count longer than the name just empties it (and is flagged in the preview).",
					func(r *Root) int { return r.batchRenameRules.TrimBack },
					func(r *Root, v int) { r.batchRenameRules.TrimBack = v }),
			},
		},
		{
			name:   "Numbering",
			active: func(rules batchrename.Rules) bool { return rules.NumberPosition != batchrename.NumberNone },
			fields: []batchRenameField{
				enumField("Position",
					"Where the counter goes: in front of the name (\"01-name\") or after it (\"name-01\"). Files are numbered in the order they're listed in the preview.",
					numberPositionChoices,
					func(r *Root) string { return numberPositionValue(r.batchRenameRules.NumberPosition) },
					func(r *Root, v string) { r.batchRenameRules.NumberPosition = parseNumberPosition(v) }),
				intField("Start at",
					"The number the first file gets.",
					func(r *Root) int { return r.batchRenameRules.NumberStart },
					func(r *Root, v int) { r.batchRenameRules.NumberStart = v }),
				intField("Step",
					"How much the counter grows per file. 0 counts as 1.",
					func(r *Root) int { return r.batchRenameRules.NumberStep },
					func(r *Root, v int) { r.batchRenameRules.NumberStep = v }),
				intField("Digits (zero-padded)",
					"Minimum width of the counter, padded with leading zeros: 3 gives 001, 002, ... A number that needs more digits simply gets them.",
					func(r *Root) int { return r.batchRenameRules.NumberDigits },
					func(r *Root, v int) { r.batchRenameRules.NumberDigits = v }),
			},
		},
		{
			name: "Extension",
			active: func(rules batchrename.Rules) bool {
				return rules.ExtensionMode != batchrename.ExtensionKeep || rules.ExtensionOnDirs
			},
			fields: []batchRenameField{
				enumField("Extension",
					"What happens to the part after the last dot. \"Set to...\" replaces it with the value below. Dotfiles like .bashrc have no extension.",
					extensionModeChoices,
					func(r *Root) string { return extensionModeValue(r.batchRenameRules.ExtensionMode) },
					func(r *Root, v string) { r.batchRenameRules.ExtensionMode = parseExtensionMode(v) }),
				stringField("Set to (used by \"Set to...\" above)",
					"The new extension, with or without a leading dot. Empty removes the extension.",
					func(r *Root) string { return r.batchRenameRules.ExtensionValue },
					func(r *Root, v string) { r.batchRenameRules.ExtensionValue = v }),
				boolField("Treat folder names as having extensions too",
					"Off: a folder called \"my.project\" is just that — nothing after its dot is ever touched by any step. On: folders split into name + extension like files.",
					func(r *Root) bool { return r.batchRenameRules.ExtensionOnDirs },
					func(r *Root, v bool) { r.batchRenameRules.ExtensionOnDirs = v }),
			},
		},
	}
}
