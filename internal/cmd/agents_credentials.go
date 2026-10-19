package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/agents"
	"github.com/klaassen-consulting/jc/internal/plan"
)

// The three credential writes. CLI-ONLY: there are no MCP tools for these,
// and internal/mcp/no_secret_tools_test.go enforces that structurally.
//
// The reasoning, which is the Password Vault argument with the risk
// inverted. A vault secret leaks on a READ. An agent credential leaks on a
// WRITE: the create is the only call in this API that ever returns a
// clientSecret, and it classifies as mutating — so it earns a plan and a
// confirmation and still puts a live secret into a transcript when it
// succeeds. The gates undo changes; nothing undoes a disclosed secret.
//
// Two requirements pull against each other here, and both have to hold:
//
//	NEVER LEAK   the secret must not reach a terminal, a log or a
//	             scrollback that the operator did not choose.
//	NEVER LOSE   swallowing it costs a credential. An agent can hold only
//	             one, so a lost secret means deleting the live credential
//	             and creating another, and the agent cannot authenticate in
//	             between.
//
// So the destination is chosen explicitly and VALIDATED BEFORE the create
// call. A create that succeeds and then cannot deliver has burned the
// agent's only credential, which is why the file is opened and proven
// writable first.

// --- create ---------------------------------------------------------------

func newAgentsCredentialsCreateCmd() *cobra.Command {
	var lifetime int
	var outPath string
	var toStdout, toClipboard bool

	cmd := &cobra.Command{
		Use:     "create <agentId>",
		Aliases: []string{"add", "issue"},
		Short:   "Create an agent's OAuth credential (the secret is shown once)",
		Long: `Create the OAuth client credential an agent uses to authenticate to JumpCloud.

THE SECRET IS RETURNED ONCE, by this call, and by nothing afterwards. There
is no endpoint that will tell you it again. So you must say where it goes,
and jc checks that destination is usable BEFORE creating the credential —
otherwise a failure here would burn the credential and leave you deleting it
to try again.

` + agents.OneCredentialPerAgent + `.

Pick exactly one destination:

  --out FILE     write it to a new file, created with mode 0600
  --stdout       print it on stdout, for piping into a secret manager
  --clipboard    copy it to the clipboard and print nothing

--lifetime accepts ` + agents.CredentialLifetimesList() + ` days. ` +
			agents.LifetimeZeroNeverExpires + `, so it is not the default and
jc says so when you ask for it. Omitting --lifetime gives the server's own
default of ` + fmt.Sprint(agents.DefaultCredentialLifetimeDays) + ` days.`,
		Example: `  # Straight into a secret manager, nothing on screen.
  jc agents credentials create 6aa85171a36f7d16b20d9b60 --stdout \
      | vault kv put secret/agents/build-bot secret=-

  # To a file, which is created 0600 and must not already exist.
  jc agents credentials create 6aa85171a36f7d16b20d9b60 --out ./build-bot.secret

  # To the clipboard, for pasting into a console.
  jc agents credentials create 6aa85171a36f7d16b20d9b60 --clipboard

  # Preview; makes no request and creates nothing.
  jc agents credentials create 6aa85171a36f7d16b20d9b60 --plan`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]
			if err := resolveAgentID("agent", agentID); err != nil {
				return err
			}

			dest, err := chooseSecretDestination(outPath, toStdout, toClipboard)
			if err != nil {
				return err
			}
			body, err := agents.BuildCredentialCreate(lifetime)
			if err != nil {
				return err
			}

			expiry := fmt.Sprintf("expires in %d days", lifetime)
			switch {
			case lifetime < 0:
				expiry = fmt.Sprintf("expires in %d days (the API's default)",
					agents.DefaultCredentialLifetimeDays)
			case lifetime == 0:
				expiry = "NEVER EXPIRES"
			}

			proceed, err := agentsConfirm(cmd, &plan.Plan{
				Action:   "create",
				Resource: "agent credential",
				Target:   agentID,
				Effects: []string{
					expiry,
					"secret goes to: " + dest.describe(),
					"the secret is shown once and never again",
				},
				Reversible: true,
			}, "Create a credential for agent "+agentID, warnIfNeverExpires(lifetime))
			if err != nil || !proceed {
				return err
			}

			// Prove the destination works before creating anything. A
			// credential created and then undeliverable is a credential that
			// has to be deleted before another can be made.
			handle, err := dest.prepare()
			if err != nil {
				return fmt.Errorf("%w\n\nNo credential was created, so nothing was lost", err)
			}

			endpoint := agents.AgentCredentialsEndpoint(agentID)
			client, err := agentsClient(endpoint)
			if err != nil {
				handle.abandon()
				return err
			}
			raw, err := client.Create(cmd.Context(), endpoint, body)
			if err != nil {
				handle.abandon()
				if agents.CredentialAlreadyExists(err) {
					return fmt.Errorf("agent %s already has a credential, and the API allows only "+
						"one.\n\nTo replace it, delete the existing one first:\n"+
						"    jc agents credentials list %s\n"+
						"    jc agents credentials delete %s <clientId>\n"+
						"    jc agents credentials create %s --out ...\n\n"+
						"The agent cannot authenticate between the delete and the create",
						agentID, agentID, agentID, agentID)
				}
				return explainAgentsError(err)
			}

			cred, err := agents.ParseNewCredential(raw)
			if err != nil {
				handle.abandon()
				return err
			}

			// From here the secret exists only in memory and must land
			// somewhere, so a delivery failure is reported with what it
			// costs rather than as a bare error.
			if err := handle.deliver(cmd, cred.Secret()); err != nil {
				return fmt.Errorf("credential %s was created, but its secret could not be "+
					"delivered: %w\n\nThe secret is now unrecoverable. Delete the credential and "+
					"create another:\n    jc agents credentials delete %s %s",
					cred.ClientID, err, agentID, cred.ClientID)
			}

			reportCredential(cmd, cred, dest)
			return nil
		},
	}
	cmd.Flags().IntVar(&lifetime, "lifetime", -1,
		"Days until the credential expires: "+agents.CredentialLifetimesList()+" (0 means never)")
	cmd.Flags().StringVar(&outPath, "out", "",
		"Write the secret to this file, created with mode 0600; it must not already exist")
	cmd.Flags().BoolVar(&toStdout, "stdout", false,
		"Print the secret on stdout, for piping into a secret manager")
	cmd.Flags().BoolVarP(&toClipboard, "clipboard", "c", false,
		"Copy the secret to the clipboard and print nothing")
	return cmd
}

func warnIfNeverExpires(lifetime int) string {
	if lifetime == 0 {
		return "A lifetime of 0 creates a credential that NEVER EXPIRES."
	}
	return ""
}

// --- Where the secret goes ------------------------------------------------

// secretDestination is the chosen delivery for a one-shot secret.
type secretDestination struct {
	path      string
	stdout    bool
	clipboard bool
}

func (d secretDestination) describe() string {
	switch {
	case d.path != "":
		return d.path + " (mode 0600)"
	case d.stdout:
		return "stdout"
	default:
		return "the clipboard"
	}
}

// chooseSecretDestination requires exactly one destination.
//
// There is no default. A default of "print it" leaks into scrollback for
// anyone who did not think about it, and a default of "write a file"
// scatters secrets into working directories. Requiring the choice costs one
// flag and removes both.
func chooseSecretDestination(path string, stdout, clipboard bool) (secretDestination, error) {
	n := 0
	for _, set := range []bool{path != "", stdout, clipboard} {
		if set {
			n++
		}
	}
	switch n {
	case 1:
		return secretDestination{path: path, stdout: stdout, clipboard: clipboard}, nil
	case 0:
		return secretDestination{}, fmt.Errorf(
			"say where the secret should go: --out FILE, --stdout, or --clipboard.\n\n" +
				"There is no default on purpose. This secret is returned once and never " +
				"again, so jc will not guess whether to print it or file it")
	default:
		return secretDestination{}, fmt.Errorf(
			"--out, --stdout and --clipboard are mutually exclusive: pick one place for the secret")
	}
}

// secretHandle is a prepared destination, proven usable before the
// credential is created.
type secretHandle struct {
	file *os.File
	dest secretDestination
}

// prepare proves the destination can take a secret.
//
// For a file this creates it, empty, with O_EXCL so an existing file is
// never clobbered — a secret file is exactly the kind of thing that must not
// be silently overwritten. For stdout and the clipboard there is nothing to
// reserve.
func (d secretDestination) prepare() (secretHandle, error) {
	if d.path == "" {
		return secretHandle{dest: d}, nil
	}
	if dir := filepath.Dir(d.path); dir != "" {
		if _, err := os.Stat(dir); err != nil {
			return secretHandle{}, fmt.Errorf("cannot write to %s: %w", d.path, err)
		}
	}
	f, err := os.OpenFile(d.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return secretHandle{}, fmt.Errorf("%s already exists, and jc will not overwrite a "+
				"file that might hold a secret — choose another path or remove it first", d.path)
		}
		return secretHandle{}, fmt.Errorf("creating %s: %w", d.path, err)
	}
	return secretHandle{file: f, dest: d}, nil
}

// abandon removes a reserved file when the credential was never created, so
// a failed attempt does not leave an empty secret file behind.
func (h secretHandle) abandon() {
	if h.file == nil {
		return
	}
	name := h.file.Name()
	_ = h.file.Close()
	_ = os.Remove(name)
}

// deliver puts the secret where it was asked to go.
func (h secretHandle) deliver(cmd *cobra.Command, secret string) error {
	switch {
	case h.file != nil:
		if _, err := h.file.WriteString(secret + "\n"); err != nil {
			_ = h.file.Close()
			return err
		}
		return h.file.Close()

	case h.dest.stdout:
		// stdout only, so a redirect or a pipe captures exactly the secret
		// and nothing else. Everything jc says goes to stderr.
		fmt.Fprintln(cmd.ErrOrStderr(),
			"The secret is being written to stdout. It is shown once and never again.")
		_, err := fmt.Fprintln(cmd.OutOrStdout(), secret)
		return err

	default:
		return clipboardWrite(secret)
	}
}

// reportCredential says what was created, and deliberately not the secret or
// its length.
//
// An exact length narrows a brute-force search, which a security review
// flagged on the Password Vault reader for the same reason. Nothing here
// derives from the secret's value.
func reportCredential(cmd *cobra.Command, cred agents.NewCredential, dest secretDestination) {
	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, "── created %s ──\n", cred)
	fmt.Fprintf(w, "client id:   %s\n", cred.ClientID)
	fmt.Fprintf(w, "auth method: %s\n", cred.TokenEndpointAuthMethod)
	if cred.NeverExpires() {
		fmt.Fprintf(w, "expires:     never\n")
	} else {
		fmt.Fprintf(w, "expires:     %s\n", cred.ExpiresAt)
	}
	switch {
	case dest.path != "":
		fmt.Fprintf(w, "secret:      written to %s (mode 0600)\n", dest.path)
	case dest.clipboard:
		fmt.Fprintf(w, "secret:      copied to the clipboard\n")
	default:
		fmt.Fprintf(w, "secret:      written to stdout\n")
	}
	fmt.Fprintf(w, "\nThis was the only time the secret was available. "+
		"No command can retrieve it again.\n")
}

// --- update ---------------------------------------------------------------

func newAgentsCredentialsUpdateCmd() *cobra.Command {
	var authMethod, status string
	var lifetime int

	cmd := &cobra.Command{
		Use:     "update <agentId> <clientId>",
		Aliases: []string{"set"},
		Short:   "Change a credential's token endpoint auth method",
		Long: `Change how an agent presents its credential at the token endpoint.

THIS IS THE ONLY THING AN UPDATE CAN CHANGE. ` +
			agents.CredentialPatchChangesOnlyTheAuthMethod + `. jc refuses --status and
--lifetime rather than sending them, because the API accepts both and
discards them while returning success.

--auth-method accepts client_secret_basic or client_secret_post.

No secret is returned by this call.`,
		Example: `  jc agents credentials update 6aa85171a36f7d16b20d9b60 \
      agent-6aa85171a36f7d16b20d9b60-e242fb7cd915dd8a \
      --auth-method client_secret_post`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, clientID := args[0], args[1]
			if err := resolveAgentID("agent", agentID); err != nil {
				return err
			}
			body, err := agents.BuildCredentialPatch(authMethod, status, lifetime)
			if err != nil {
				return err
			}

			proceed, err := agentsConfirm(cmd, &plan.Plan{
				Action:     "update",
				Resource:   "agent credential",
				Target:     clientID,
				Effects:    []string{"tokenEndpointAuthMethod: " + authMethod},
				Reversible: true,
			}, "Update credential "+clientID, "")
			if err != nil || !proceed {
				return err
			}

			endpoint := agents.AgentCredentialEndpoint(agentID, clientID)
			client, err := agentsClient(agents.AgentCredentialsEndpoint(agentID))
			if err != nil {
				return err
			}
			raw, err := client.Patch(cmd.Context(), endpoint, body)
			if err != nil {
				if agents.CredentialNotFound(err) {
					return fmt.Errorf("agent %s has no credential with client id %s — list them "+
						"with: jc agents credentials list %s", agentID, clientID, agentID)
				}
				return explainAgentsError(err)
			}

			// The update response is the credential record, which carries no
			// secret — so unlike the create, it is safe for generic output.
			rows, err := agents.ParseCredentials(
				[]byte(`{"credentials":[` + string(raw) + `]}`))
			if err != nil || len(rows) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Updated credential %s\n", clientID)
				return nil
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "── updated credential %s ──\n", clientID)
			return nil
		},
	}
	cmd.Flags().StringVar(&authMethod, "auth-method", "",
		"Token endpoint auth method: client_secret_basic or client_secret_post (required)")
	// Registered so the refusal can explain itself. A flag that silently did
	// nothing would be worse, and leaving it out entirely would make the
	// operator discover the limitation from the API instead.
	cmd.Flags().StringVar(&status, "status", "",
		"Not supported: the API accepts a status change and ignores it (delete the credential instead)")
	cmd.Flags().IntVar(&lifetime, "lifetime", -1,
		"Not supported: a credential's lifetime is fixed at creation")
	return cmd
}

// --- delete ---------------------------------------------------------------

func newAgentsCredentialsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <agentId> <clientId>",
		Aliases: []string{"rm", "remove", "revoke"},
		Short:   "Delete an agent's credential (stops it authenticating)",
		Long: `Delete an agent's OAuth credential.

THE AGENT STOPS BEING ABLE TO AUTHENTICATE. ` + agents.DeleteIsTheOnlyRevocation + `.

This is also how a credential is rotated, because ` +
			agents.OneCredentialPerAgent + `. If that is what you are doing, have the
replacement command ready — the agent is locked out until it runs:

    jc agents credentials delete <agentId> <clientId>
    jc agents credentials create <agentId> --out ./new.secret

If the goal is to stop the agent working rather than to replace its
credential, "jc agents suspend" does that reversibly and leaves the
credential in place.`,
		Example: `  jc agents credentials delete 6aa85171a36f7d16b20d9b60 \
      agent-6aa85171a36f7d16b20d9b60-e242fb7cd915dd8a --plan`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, clientID := args[0], args[1]
			if err := resolveAgentID("agent", agentID); err != nil {
				return err
			}

			proceed, err := agentsConfirm(cmd, &plan.Plan{
				Action:   "delete",
				Resource: "agent credential",
				Target:   clientID,
				Effects: []string{
					"the agent can no longer authenticate",
					"the secret cannot be recovered",
					"a new credential must be created",
				},
				Reversible: false,
			}, "Delete credential "+clientID,
				"Deleting this credential stops agent "+agentID+" authenticating until a new one is created. "+
					"`jc agents suspend` stops it reversibly instead.")
			if err != nil || !proceed {
				return err
			}

			client, err := agentsClient(agents.AgentCredentialsEndpoint(agentID))
			if err != nil {
				return err
			}
			endpoint := agents.AgentCredentialEndpoint(agentID, clientID)
			if _, err := client.Delete(cmd.Context(), endpoint); err != nil {
				if agents.CredentialNotFound(err) {
					return fmt.Errorf("agent %s has no credential with client id %s — it may "+
						"already be deleted. List them with: jc agents credentials list %s",
						agentID, clientID, agentID)
				}
				return explainAgentsError(err)
			}

			// The delete answers with an empty body, so the read-back is the
			// listing: it also tells the operator the agent now has none.
			fmt.Fprintf(cmd.ErrOrStderr(), "Deleted credential %s\n", clientID)
			if raw, rErr := client.Get(cmd.Context(), agents.AgentCredentialsEndpoint(agentID)); rErr == nil {
				if rows, pErr := agents.ParseCredentials(raw); pErr == nil {
					if len(rows) == 0 {
						fmt.Fprintf(cmd.ErrOrStderr(),
							"Agent %s now has no credential and cannot authenticate. "+
								"Create one with: jc agents credentials create %s --out ...\n",
							agentID, agentID)
					} else {
						fmt.Fprintf(cmd.ErrOrStderr(), "── %d credential(s) remain ──\n", len(rows))
					}
				}
			}
			return nil
		},
	}
}
