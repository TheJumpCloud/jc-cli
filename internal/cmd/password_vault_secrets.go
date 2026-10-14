package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/passwordvault"
)

// Reading secrets out of the vault.
//
// THESE COMMANDS HAVE NO MCP EQUIVALENT, and that is the one design decision
// in this file worth arguing about. internal/mcp's safety model is built
// around mutation: tools are classified by what they change, writes are
// plan-by-default, and destructive ones sit behind a step-up prompt. None of
// that helps here. Reading a secret changes nothing, so it would classify as
// read-only and sail through every gate — and the damage is done the moment
// the value lands in a transcript, which no confirmation can undo. An agent
// able to read every credential in an organization is not a capability this
// CLI should offer, however the tool description is worded.
//
// NOTHING IS PRINTED UNLESS ASKED FOR. With neither --reveal nor --clipboard,
// these commands report which secrets a credential HOLDS and not one of their
// values. That is the useful default rather than a cautious one: "does this
// record have a TOTP key?" is the common question, and answering it should not
// put a password into terminal scrollback, a CI log, a `set -x` trace or
// somebody's screen recording.

// clipboardWrite is swapped in tests, which must never touch a real
// clipboard — and must never be given a real secret either.
var clipboardWrite = clipboard.WriteAll

// secretOutput is how a caller asked for the values, if at all.
type secretOutput struct {
	reveal    bool
	toClip    bool
	fieldName string
}

func (o secretOutput) chosen() bool { return o.reveal || o.toClip }

// addSecretFlags registers the output flags shared by the secret readers.
func addSecretFlags(cmd *cobra.Command, o *secretOutput) {
	cmd.Flags().BoolVar(&o.reveal, "reveal", false,
		"Print the secret values to stdout. Without this, only the field NAMES are shown.")
	cmd.Flags().BoolVarP(&o.toClip, "clipboard", "c", false,
		"Copy the secret to the clipboard instead of printing it")
	cmd.Flags().StringVar(&o.fieldName, "field", "",
		"Read one field only, e.g. password or mfaKey (required with --clipboard when several exist)")
}

// describeHeld lists what a credential holds, without values. This is what
// runs when no output flag was given.
func describeHeld(cmd *cobra.Command, id string, sec passwordvault.Secret, v2 bool) error {
	fields := sec.Fields
	names := passwordvault.SecretFieldOrder(fields)

	// The three ways of having nothing to show are NOT the same thing, and
	// saying "holds no secret" when the truth is "you may not look" would be
	// the most damaging error this command could make.
	if len(names) == 0 {
		switch {
		case sec.Withheld():
			return fmt.Errorf("credential %s exists, but the API reports canView=false for "+
				"this caller — the values are withheld, NOT absent. Note this happens even "+
				"for a caller holding Manage, View Detail and Connect, so it is not "+
				"necessarily something the access policy can fix", id)
		case sec.Ambiguous():
			return fmt.Errorf("credential %s returned no secret values, and this endpoint "+
				"cannot say whether that means it holds none or that you may not see them — "+
				"it answers 200 with empty strings either way. Run `jc password-vault "+
				"credentials secret-v2 %s`, which reports canView", id, id)
		default:
			fmt.Fprintf(cmd.ErrOrStderr(),
				"credential %s holds no secret values (canView=true, so this is absence).\n", id)
			return nil
		}
	}

	// FIELD NAMES ONLY, and deliberately not their lengths. An earlier version
	// of this printed len(value) per field, which a background security review
	// flagged as sensitive-data-exposure — correctly. An exact password length
	// materially narrows a brute-force or credential-stuffing search, and this
	// is the path that runs with no flags, the one meant to be safe to use in
	// front of other people. The field's presence already answers the question
	// this output exists for — "does this credential have a TOTP key?" — and
	// the length adds nothing the caller needs to know.
	rows := make([]json.RawMessage, 0, len(names))
	for _, n := range names {
		b, err := json.Marshal(map[string]any{"field": n, "set": true})
		if err != nil {
			return err
		}
		rows = append(rows, b)
	}
	opts := output.CurrentOptions()
	opts.DefaultFields = []string{"field", "set"}
	if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		writeNounFooter(cmd, len(names), "secret field", "secret fields")
		fmt.Fprintf(cmd.ErrOrStderr(),
			"\nValues withheld. Add --clipboard to copy one, or --reveal to print.\n")
	}
	return nil
}

// emitSecret hands over the actual values, by whichever route was asked for.
func emitSecret(cmd *cobra.Command, id string, sec passwordvault.Secret, o secretOutput) error {
	fields := sec.Fields
	names := passwordvault.SecretFieldOrder(fields)
	if len(names) == 0 {
		if sec.Withheld() {
			return fmt.Errorf("credential %s: the API reports canView=false, so there is "+
				"nothing to reveal — the values are withheld rather than absent", id)
		}
		if sec.Ambiguous() {
			return fmt.Errorf("credential %s returned no values, and this endpoint cannot "+
				"distinguish that from them being withheld; try secret-v2", id)
		}
		return fmt.Errorf("credential %s holds no secret values", id)
	}

	if o.fieldName != "" {
		v, ok := fields[o.fieldName]
		if !ok {
			return fmt.Errorf("credential %s has no %q field — it holds %s", id, o.fieldName,
				strings.Join(names, ", "))
		}
		fields = map[string]string{o.fieldName: v}
		names = []string{o.fieldName}
	}

	if o.toClip {
		if len(names) > 1 {
			return fmt.Errorf("credential %s holds %s — name one with --field, since the "+
				"clipboard takes a single value", id, strings.Join(names, ", "))
		}
		if err := clipboardWrite(fields[names[0]]); err != nil {
			return fmt.Errorf("copying to the clipboard: %w (on a headless host use --reveal "+
				"and pipe it)", err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "copied %s to the clipboard.\n", names[0])
		return nil
	}

	// --reveal. Warn when the destination is a terminal, because that is the
	// case where the value persists somewhere the caller did not choose.
	if config.IsStdoutTerminal() && !output.CurrentOptions().Quiet {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"warning: printing a secret to a terminal — it stays in scrollback.")
	}

	if o.fieldName != "" {
		// One field, bare, so it pipes cleanly into another command.
		_, err := fmt.Fprintln(cmd.OutOrStdout(), fields[o.fieldName])
		return err
	}
	ordered := make(map[string]string, len(fields))
	for _, n := range names {
		ordered[n] = fields[n]
	}
	b, err := json.Marshal(ordered)
	if err != nil {
		return err
	}
	return output.WriteSingle(cmd.OutOrStdout(), b, output.CurrentOptions())
}

// newPWVSecretCmds builds the secret readers.
func newPWVSecretCmds() []*cobra.Command {
	var out secretOutput

	var endpointChoice string
	secret := &cobra.Command{
		Use:   "secret <id>",
		Short: "What secrets a credential holds, and optionally their values",
		Long: `Read a credential's stored secrets.

WITH NO FLAGS THIS PRINTS NO VALUES AND NO LENGTHS. It lists which fields the
credential holds — enough to answer "does this record have a TOTP key?"
without putting a password into scrollback, a CI log or a screen share, and
without disclosing a length that would narrow a guess at one.

  --clipboard   copy one value, print nothing
  --reveal      print the values to stdout
  --field NAME  limit to one field; with --reveal it prints bare, so it pipes

IT READS /with-secret, NOT /secret. The two endpoints named for secrets do
not return them: on 2026-10-14, against a credential created seconds earlier
whose policy granted Manage, View Detail and Connect, /secret answered 200
with every field an empty string and /secret/v2 answered with canView=false —
while /with-secret returned the password. This command uses the one that
works; --endpoint reaches the others for diagnosing the disagreement.

There is no MCP tool for this. The MCP safety model classifies tools by what
they mutate, and reading a secret mutates nothing — so it would pass every
gate, while the harm happens the moment the value reaches a transcript.`,
		Example: `  # What does this credential hold?
  jc password-vault credentials secret 6ab6bdcd1d6931fd7863bd4f

  # Copy the password, print nothing.
  jc password-vault credentials secret 6ab6… --field password --clipboard

  # Pipe it somewhere.
  jc password-vault credentials secret 6ab6… --field password --reveal | pbcopy`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ep, v2, eerr := resolveSecretEndpoint(endpointChoice, args[0])
			if eerr != nil {
				return eerr
			}
			return runSecret(cmd, args[0], ep, out, v2)
		},
	}
	addSecretFlags(secret, &out)
	secret.Flags().StringVar(&endpointChoice, "endpoint", "with-secret",
		"Which endpoint to read: with-secret (the only one that returns values), v1 or v2")

	var outV2 secretOutput
	secretV2 := &cobra.Command{
		Use:   "secret-v2 <id>",
		Short: "The v2 secret endpoint, which carries more field types",
		Long: `Read a credential's secrets through the v2 endpoint.

It returns the same values under DIFFERENT NAMES — "pass" rather than
"password" — and adds fields the v1 endpoint omits: secureNoteContent,
twoFaSecret, cardNumber, securityCode. Use this one for a secure note, a
credit card or an identity record; v1 returns nothing for those.

Same output rules as ` + "`secret`" + `: no values unless asked.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecret(cmd, args[0], passwordvault.CredentialSecretV2(args[0]), outV2, true)
		},
	}
	addSecretFlags(secretV2, &outV2)

	var outWith secretOutput
	withSecret := &cobra.Command{
		Use:   "with-secret <id>",
		Short: "The whole credential record including its secret",
		Long: `Read a credential and its secret in one call.

This is the ONLY read that returns a full credential record, because the
plain detail endpoint is defective — it refuses with a demand for a "View
Detail" permission the caller holds. So this doubles as the way to see a
credential's access policies and linked websites.

It always prints the metadata. The secret inside it is withheld unless
--reveal is given, on the same reasoning as ` + "`secret`" + `.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("credential", args[0]); err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), passwordvault.CredentialWithSecret(args[0]), nil)
			if err != nil {
				return err
			}
			var rec map[string]json.RawMessage
			if err := json.Unmarshal(raw, &rec); err != nil {
				return fmt.Errorf("decoding the credential: %w", err)
			}

			if !outWith.reveal {
				// Replace the secret with a description of itself rather than
				// dropping it, so the reader knows it was there.
				sec, perr := passwordvault.ParseSecret(raw)
				if perr != nil {
					return perr
				}
				names := passwordvault.SecretFieldOrder(sec.Fields)
				sort.Strings(names)
				placeholder, merr := json.Marshal(map[string]any{
					"withheld": true,
					"fields":   names,
					"hint":     "add --reveal to print these values",
				})
				if merr != nil {
					return merr
				}
				rec["secret"] = placeholder
			}
			b, err := json.Marshal(rec)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), b, output.CurrentOptions())
		},
	}
	withSecret.Flags().BoolVar(&outWith.reveal, "reveal", false,
		"Include the secret values rather than a description of them")

	mfa := &cobra.Command{
		Use:     "mfa-code <id>",
		Aliases: []string{"totp"},
		Short:   "The current TOTP code for a credential",
		Long: `Return the credential's current one-time code, its issuer, and how many
seconds remain before it rotates.

This needs the "Connect" permission rather than "View Detail" — a caller who
can read the credential's metadata may still be refused here, and jc says
which permission is missing rather than passing on a bare 400.

The code is printed, because a one-time code that expires in seconds is not
a secret worth withholding from the person who asked for it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("credential", args[0]); err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), passwordvault.CredentialMFACode(args[0]), nil)
			if err != nil {
				if passwordvault.MFACodeUnavailable(err) {
					return fmt.Errorf("credential %s needs the \"Connect\" permission to produce "+
						"an MFA code, and this caller does not have it — \"View Detail\" is not "+
						"enough. Grant Connect in the credential's access policy", args[0])
				}
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}

	return []*cobra.Command{secret, secretV2, withSecret, mfa}
}

// runSecret is the shared body of the two secret readers.
func runSecret(cmd *cobra.Command, id, endpoint string, o secretOutput, v2 bool) error {
	if err := vaultObjectArg("credential", id); err != nil {
		return err
	}
	raw, err := pwvGet(cmd.Context(), endpoint, nil)
	if err != nil {
		return err
	}
	sec, err := passwordvault.ParseSecret(raw)
	if err != nil {
		return err
	}
	if !o.chosen() {
		return describeHeld(cmd, id, sec, v2)
	}
	return emitSecret(cmd, id, sec, o)
}

// resolveSecretEndpoint picks the endpoint to read, and reports whether it is
// the v2 shape, which is the only one carrying canView.
//
// The default is with-secret because it is the only one observed to return a
// value. v1 and v2 are reachable for diagnosing that rather than as
// alternatives that merely format the same data differently.
func resolveSecretEndpoint(choice, id string) (string, bool, error) {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "", "with-secret", "withsecret":
		return passwordvault.CredentialWithSecret(id), false, nil
	case "v1", "secret":
		return passwordvault.CredentialSecret(id), false, nil
	case "v2", "secret-v2":
		return passwordvault.CredentialSecretV2(id), true, nil
	}
	return "", false, fmt.Errorf("invalid --endpoint %q: expected with-secret, v1 or v2", choice)
}
