package bundle

import (
	"fmt"
	"strings"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// Validate runs deep, offline validation over every policy unit and
// aggregates all problems into one error (the repo convention: the
// author fixes everything in one pass, not one-error-per-edit).
//
// Apple units validate against the embedded schema catalog via the
// same BuildPayloadInstances path the compose command uses; Windows
// units through NormalizeAndValidateSettings/Keys (static — no DDF
// catalog fetch, so validate works air-gapped).
func Validate(b *Bundle, cat *apple_mdm.Catalog) error {
	var errs []string
	for i := range b.Policies {
		u := &b.Policies[i]
		at := fmt.Sprintf("policies[%d] (%s)", i, u.Name)
		switch u.Type {
		case UnitAppleProfile:
			if _, _, err := u.Profile.BuildPayloadInstances(cat); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", at, err))
			}
		case UnitWindowsOMAURI:
			if _, err := windows_mdm.NormalizeAndValidateSettings(u.WindowsSettings()); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", at, err))
			}
		case UnitWindowsRegistry:
			if _, err := windows_mdm.NormalizeAndValidateKeys(u.WindowsKeys()); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", at, err))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("bundle %q failed deep validation:\n  - %s", b.Name, strings.Join(errs, "\n  - "))
	}
	return nil
}
