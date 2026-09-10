package resolve

import (
	"encoding/json"
	"fmt"
	"strings"
)

// matchByName filters a listing down to the records whose name field
// equals name, case-insensitively. It is shared by the V1 and V2
// resolvers, which previously carried identical copies of this loop —
// which is why the same defect existed in it twice.
//
// On unreadable records it deliberately does NOT behave like the audit
// checks, which fail on the first record they cannot decode. A resolver
// walks a heterogeneous collection where a record that is not the one
// you asked for is entirely normal, so failing the whole lookup because
// one record in five hundred is odd would be worse than the bug. Instead
// it counts what it could not read and lets the caller decide, because
// the count only matters in one case — see unreadable below.
type matchResult struct {
	matches []match
	// unreadable counts records whose name could not be read at all:
	// the record was not an object, the name field was absent, or it
	// would not decode as a string.
	//
	// This is only interesting when matches is empty. A skipped record
	// alongside a successful match is irrelevant; a skipped record
	// alongside NO match is the whole story, because "not found" and "I
	// could not read the records" are the same output otherwise. If the
	// name field is renamed upstream, every record becomes unreadable
	// and `jc` reports that a user who plainly exists does not.
	unreadable int
	total      int
}

// matchByName returns the matching records, or an error if a record
// matched by name but its ID could not be read.
//
// That case is a hard error rather than a skip: the resource was found
// and cannot be returned, so reporting "not found" would be a lie about
// something we just saw. It is also the one skip here that no amount of
// list heterogeneity explains.
func matchByName(records []json.RawMessage, name string, cfg ResourceConfig) (matchResult, error) {
	res := matchResult{total: len(records)}
	lowerName := strings.ToLower(name)

	for _, raw := range records {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			res.unreadable++
			continue
		}

		var nameVal string
		if cfg.ExtractNameFunc != nil {
			n, err := cfg.ExtractNameFunc(raw)
			if err != nil {
				res.unreadable++
				continue
			}
			nameVal = n
		} else {
			nameRaw, ok := obj[cfg.NameField]
			if !ok {
				res.unreadable++
				continue
			}
			if err := json.Unmarshal(nameRaw, &nameVal); err != nil {
				res.unreadable++
				continue
			}
		}

		// A name that simply differs is not unreadable — it is the
		// filter doing its job, and must not count toward the
		// diagnostic below.
		if strings.ToLower(nameVal) != lowerName {
			continue
		}

		idRaw, ok := obj[cfg.IDField]
		if !ok {
			return res, fmt.Errorf("%s %q was found, but the record has no %q field — "+
				"the record shape has changed and jc cannot return an ID for a resource "+
				"it can see", cfg.NameField, nameVal, cfg.IDField)
		}
		var idVal string
		if err := json.Unmarshal(idRaw, &idVal); err != nil {
			return res, fmt.Errorf("%s %q was found, but its %q field did not decode as "+
				"a string: %w — the record shape has changed and jc cannot return an ID "+
				"for a resource it can see", cfg.NameField, nameVal, cfg.IDField, err)
		}

		res.matches = append(res.matches, match{ID: idVal, Name: nameVal})
	}

	return res, nil
}

// notFound builds the zero-match error, folding in the unreadable count
// when there is one. Without it, a rename of the name field upstream
// presents to the operator as a plain, confident "user not found" — and
// they go looking in the directory rather than at the schema.
func (m matchResult) notFound(name string, cfg ResourceConfig) error {
	msg := fmt.Sprintf("%s %q not found", cfg.NameField, name)
	if m.unreadable > 0 {
		msg += fmt.Sprintf(" — but %d of %d records had no readable %q field, so this "+
			"may be a change in the record shape rather than a missing resource",
			m.unreadable, m.total, cfg.NameField)
	}
	return &ResolveError{
		ResourceType: cfg.NameField,
		Identifier:   name,
		Message:      msg,
	}
}

// ambiguous builds the multi-match error.
func (m matchResult) ambiguous(name string, cfg ResourceConfig) error {
	lines := make([]string, len(m.matches))
	for i, mt := range m.matches {
		lines[i] = fmt.Sprintf("  %s (ID: %s)", mt.Name, mt.ID)
	}
	return &ResolveError{
		ResourceType: cfg.NameField,
		Identifier:   name,
		Message: fmt.Sprintf("ambiguous %s %q matched %d resources:\n%s",
			cfg.NameField, name, len(m.matches), strings.Join(lines, "\n")),
	}
}
