package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

// `jc api` — one unshaped request against the JumpCloud API.
//
// This exists because docs/solutions/conventions/empirical-gate-before-coding
// prescribes probing the live tenant before writing an emitter, and its worked
// example is `jc api get /policytemplates/<id>` — a command that did not exist.
// The gate had no executable form, so every new API surface began with someone
// pulling a key out of the keychain and hand-rolling curl. That is both slower
// and worse: the key gets materialised, and the probe does not go through the
// same auth resolution, retries, rate limiting and error decoding that the
// real commands use, so what it proves is not quite what jc will do.
//
// It is deliberately thin. It resolves the API version, builds the URL, hands
// the request to the same V1/V2 client every other command uses, and writes
// whatever comes back through the normal output pipeline. It adds no
// pagination: one invocation is exactly one HTTP request, so a V2 list returns
// only the server's first page. That is the point — a probe should show the
// wire, not a convenience layer over it.

// apiVersion selects which client serves a request.
type apiVersion string

const (
	apiV1 apiVersion = "v1"
	apiV2 apiVersion = "v2"
)

// versionPrefixes maps a leading path segment to the version it implies, so a
// path pasted straight out of the API reference ("/api/v2/password-vault/...")
// works without the caller restating the version in a flag. Longest first.
var versionPrefixes = []struct {
	prefix  string
	version apiVersion
}{
	{"/api/v2/", apiV2},
	{"/api/v1/", apiV1},
	{"/v2/", apiV2},
	{"/v1/", apiV1},
}

// resolvePath splits a user-supplied path into the version that should serve
// it and the endpoint to hand the client. defaultVersion applies only when the
// path carries no version prefix of its own.
//
// The returned endpoint keeps any query string the caller wrote, because the
// clients append it to their base URL verbatim.
func resolvePath(raw string, defaultVersion apiVersion) (apiVersion, string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", "", fmt.Errorf("path is empty")
	}
	if strings.Contains(p, "://") {
		return "", "", fmt.Errorf("path %q is a full URL: pass just the path, e.g. /api/v2/systemusers", raw)
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	for _, vp := range versionPrefixes {
		if strings.HasPrefix(p, vp.prefix) {
			// Keep the leading slash: "/api/v2/foo" -> "/foo".
			return vp.version, p[len(vp.prefix)-1:], nil
		}
	}
	return defaultVersion, p, nil
}

// appendParams adds --param key=value pairs to a path's query string. It does
// not replace an existing query string, so a path that already carries one and
// a --param can be combined.
func appendParams(endpoint string, params []string) (string, error) {
	if len(params) == 0 {
		return endpoint, nil
	}
	values := url.Values{}
	for _, p := range params {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return "", fmt.Errorf("invalid --param %q: expected key=value", p)
		}
		if k == "" {
			return "", fmt.Errorf("invalid --param %q: empty key", p)
		}
		values.Add(k, v)
	}
	sep := "?"
	if strings.Contains(endpoint, "?") {
		sep = "&"
	}
	return endpoint + sep + values.Encode(), nil
}

// readBody resolves the --data flag: inline JSON, @file, or "-" for stdin. It
// validates that the result is JSON here rather than letting the server reject
// it, so a typo costs a local error instead of a round trip and a 400 whose
// message may say nothing useful.
func readBody(cmd *cobra.Command, data string) (json.RawMessage, error) {
	if data == "" {
		return nil, nil
	}
	var raw []byte
	switch {
	case data == "-":
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("reading body from stdin: %w", err)
		}
		raw = b
	case strings.HasPrefix(data, "@"):
		b, err := os.ReadFile(data[1:])
		if err != nil {
			return nil, fmt.Errorf("reading body file: %w", err)
		}
		raw = b
	default:
		raw = []byte(data)
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("--data is not valid JSON (use @file to read from a file, or - for stdin)")
	}
	return json.RawMessage(raw), nil
}

// writeAPIResult renders a response through the normal output pipeline, so
// --output, --query, --fields and -t behave as they do everywhere else. A
// 204 and an empty body both print nothing rather than "null".
func writeAPIResult(cmd *cobra.Command, body json.RawMessage) error {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	// A body that is not JSON goes out verbatim. The output pipeline cannot
	// carry it — every format marshals — and on a probe the malformed body IS
	// the finding, so a decode error in its place would lose exactly the thing
	// worth seeing.
	if !json.Valid(body) {
		fmt.Fprintln(cmd.ErrOrStderr(), "── response is not valid JSON; printing it verbatim ──")
		_, err := fmt.Fprintln(cmd.OutOrStdout(), trimmed)
		return err
	}

	opts := output.CurrentOptions()
	if trimmed[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(body, &items); err != nil {
			return fmt.Errorf("decoding array response: %w", err)
		}
		return output.WriteList(cmd.OutOrStdout(), items, opts)
	}
	return output.WriteSingle(cmd.OutOrStdout(), body, opts)
}

func newAPICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "Send a raw request to the JumpCloud API",
		Long: `Send one unshaped request to the JumpCloud API.

This is the probe tool behind the empirical gate: before implementing a new
API surface, hit it live and see what the wire actually looks like rather than
trusting the OpenAPI spec, which is routinely wrong about bodies, id formats
and even whether an endpoint exists.

Authentication, retries, rate limiting and error decoding are the same as
every other jc command — the API key is resolved from --api-key, JC_API_KEY
or the active profile, and never has to be read or pasted by hand.

The version is taken from the path when it carries one, so a path copied out
of the API reference works unchanged:

  /api/v2/systemusers   ->  V2
  /v1/systems           ->  V1
  /systemusers          ->  --api (default v2)

One invocation is exactly one HTTP request. There is no pagination, so a V2
list returns the server's first page only; pass --param limit=... to widen it.`,
		Example: `  # Probe a new surface before writing any code.
  jc api get /api/v2/password-vault/status

  # Shape a response without printing values.
  jc api get /api/v2/password-vault/folders --query 'keys(@)'

  # A V1 path, with query parameters.
  jc api get /v1/systemusers --param limit=1 --param fields=email

  # Preview a write; --plan makes no request and exits 10.
  jc api post /api/v2/usergroups -d '{"name":"probe"}' --plan

  # Body from a file, or from stdin.
  jc api put /api/v2/systems/abc -d @body.json
  echo '{"name":"x"}' | jc api post /api/v2/usergroups -d -`,
	}
	cmd.AddCommand(
		newAPIVerbCmd("get", "GET"),
		newAPIVerbCmd("post", "POST"),
		newAPIVerbCmd("put", "PUT"),
		newAPIVerbCmd("patch", "PATCH"),
		newAPIVerbCmd("delete", "DELETE"),
	)
	return cmd
}

// isWriteVerb reports whether a method changes server state, and so needs the
// confirmation and --plan treatment every other mutating command gets.
func isWriteVerb(method string) bool {
	return method != "GET"
}

func newAPIVerbCmd(use, method string) *cobra.Command {
	var (
		data      string
		params    []string
		apiFlag   string
		shortVerb = strings.ToLower(method)
	)

	cmd := &cobra.Command{
		Use:   use + " <path>",
		Short: method + " a raw JumpCloud API path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			defaultVersion := apiV2
			switch apiFlag {
			case "", "v2":
				defaultVersion = apiV2
			case "v1":
				defaultVersion = apiV1
			default:
				return fmt.Errorf("invalid --api %q: expected v1 or v2", apiFlag)
			}

			version, endpoint, err := resolvePath(args[0], defaultVersion)
			if err != nil {
				return err
			}
			endpoint, err = appendParams(endpoint, params)
			if err != nil {
				return err
			}

			body, err := readBody(cmd, data)
			if err != nil {
				return err
			}
			if body != nil && method == "GET" {
				return fmt.Errorf("--data is not sent on a GET: use --param for query parameters")
			}

			target := strings.ToUpper(string(version)) + " " + endpoint

			if isWriteVerb(method) {
				if viper.GetBool("plan") {
					effects := []string{"method: " + method, "endpoint: " + target}
					if body != nil {
						effects = append(effects, "body: "+truncateForPlan(string(body)))
					}
					return renderPlan(cmd, &plan.Plan{
						Action:   shortVerb,
						Resource: "api",
						Target:   target,
						Effects:  effects,
						// A raw call can do anything the API can do, including
						// deleting a resource outright. Claiming otherwise in a
						// preview would be worse than saying nothing.
						Reversible: false,
					})
				}
				if mustAbortWithoutTTY() {
					return fmt.Errorf("%s %s requires --force or --non-interactive (or preview with --plan first)", method, target)
				}
				if shouldConfirm() {
					ok, err := askYesNo(cmd, fmt.Sprintf("Send %s %s?", method, target))
					if err != nil {
						return err
					}
					if !ok {
						fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
						return nil
					}
				}
			}

			resp, err := doAPIRequest(cmd, version, method, endpoint, body)
			if err != nil {
				return err
			}
			return writeAPIResult(cmd, resp)
		},
	}

	cmd.Flags().StringVarP(&data, "data", "d", "", "Request body: inline JSON, @file, or - for stdin")
	cmd.Flags().StringArrayVar(&params, "param", nil, "Query parameter as key=value (repeatable)")
	cmd.Flags().StringVar(&apiFlag, "api", "v2", "API version for a path with no version prefix: v1 or v2")
	if method == "GET" {
		// A GET has no body; leaving the flag registered but rejected at run
		// time would be a worse error than not offering it at all.
		_ = cmd.Flags().MarkHidden("data")
	}
	return cmd
}

// truncateForPlan keeps a plan preview readable when the body is large.
func truncateForPlan(s string) string {
	const max = 200
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// doAPIRequest dispatches to the client for the resolved version. Each client
// method is the same one the typed commands use, so retries, rate limiting and
// error decoding are identical.
func doAPIRequest(cmd *cobra.Command, version apiVersion, method, endpoint string, body json.RawMessage) (json.RawMessage, error) {
	ctx := cmd.Context()

	if version == apiV1 {
		client, err := api.NewV1Client()
		if err != nil {
			return nil, err
		}
		switch method {
		case "GET":
			return client.Get(ctx, endpoint)
		case "POST":
			return client.Post(ctx, endpoint, body)
		case "PUT":
			return client.Update(ctx, endpoint, body)
		case "DELETE":
			return client.Delete(ctx, endpoint)
		case "PATCH":
			return nil, fmt.Errorf("PATCH is not supported on the V1 API")
		}
		return nil, fmt.Errorf("unsupported method %q", method)
	}

	client, err := api.NewV2Client()
	if err != nil {
		return nil, err
	}
	switch method {
	case "GET":
		return client.Get(ctx, endpoint)
	case "POST":
		return client.Create(ctx, endpoint, body)
	case "PUT":
		return client.Update(ctx, endpoint, body)
	case "PATCH":
		return client.Patch(ctx, endpoint, body)
	case "DELETE":
		// A V2 DELETE may legitimately carry a body — several association
		// endpoints take one — so route through DeleteWithBody when given.
		if body != nil {
			return client.DeleteWithBody(ctx, endpoint, body)
		}
		return client.Delete(ctx, endpoint)
	}
	return nil, fmt.Errorf("unsupported method %q", method)
}
