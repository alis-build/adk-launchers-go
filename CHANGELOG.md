# Changelog

What changed in each release of go.alis.build/adk/launchers. This file is
generated from release tags and commit messages; do not edit it by hand.

## [1.4.0](https://github.com/alis-build/adk-launchers-go/compare/v1.3.0...v1.4.0) (2026-11-10)

ADK Go v2.7.1, OAuth sign-in for agent tools, and evals parity with
adk-python.

- tooloauth (new): a runner plugin that gives configured tools the end
  user's OAuth token. A tool with no token pauses the run for sign-in
  (an AG-UI auth_required interrupt), tokens refresh once before they
  expire, and replayed or forged sign-in replies are refused.
  tooloauth/spannerstore keeps tokens in Spanner under Cloud KMS envelope
  encryption.
- web: ADK REST callers are authenticated from the forwarded identity.
- agui: an OAuth sign-in pause is an auth_required interrupt, resumed
  with the filled auth config. Every streamed reply is now sent once:
  it was repeated after TurnComplete for any producer with a step.
- scheduler: cron ticks run as headless runs, and a tick that spent its
  model-call budget is not retried.
- evals, identity: every route needs a caller identity; runs execute as
  the caller on the route's app, add-session acts only as the caller,
  reads strip credential secrets, and runs use no stored OAuth token
  unless WithStoredOAuthTokens is set.
- evals, new from adk-python: multi-run with pass rate, pass@k and
  pass^k; the informational efficiency metrics (tool calls, model calls,
  tokens, duration) on every run; ignore_args; parallelism in the eval
  config and run request; file:// storage and ADK_EVAL_STORAGE_URI; live,
  audio-simulated runs (llm_audio); pinned eval sessions, only the
  caller's and only eval sessions; parallel LLM-judge calls
  (parallelismLimit).
- evals, compatibility: the run response, metrics-info, judge parsing,
  ROUGE, thresholds and session IDs match adk-python and adk-web.

Breaking, in evals/evaluation/* only:
- simulation: UserSimulatorProvider.Provide takes a context.Context first.
- models: MetricValueInfo.Interval is a *MetricValueInterval.
- generator: EvalSessionIDPrefix is "adk-eval-session-"; the old prefix
  is still recognized by IsEvalSessionID.
- generator.Generator and simulation.UserSimulatorProvider are no longer
  comparable with ==.

### Features

#### web: Catch up with ADK v2.7's web launcher ([904703c](https://github.com/alis-build/adk-launchers-go/commit/904703c9ad0fe9a5aaec8d1dc6105f918e0d8064))

ADK's own web launcher changed between v2.3 and v2.7; this brings ours
level where the change fits a gateway-fronted Cloud Run deployment.

Telemetry is now initialised before the listener opens and shut down on
every exit path, and an embedder's WithOtelToCloud(true) is no longer
overridden by the -otel_to_cloud flag's default.

A missing memory service defaults to an in-memory one, since ADK's REST
handlers panic without it. The artifact service is deliberately not
defaulted, unlike upstream: on Cloud Run an in-memory store would lose
saved files on restart or on another instance, so a missing one keeps
failing loudly.

New flags: -max_request_body_size limits routes registered through
SetupSubrouters (host routes are left alone, since a body limit would
cut long-lived gRPC streams), and -allow_origins feeds
launcher.Config.AllowedOrigins for ADK's api sublauncher. GET /health
answers for probes.

Upstream's loopback default bind and launcher-wide origin guard are not
adopted; doc.go records why. Run is split into buildRouter so the
router can be tested without the process-wide mux.

The README gains an "Upgrading to ADK v2.7" section and drops an example
that passed ADK's web launcher as a sublauncher.

#### web: Authenticate ADK REST callers from the forwarded identity ([4805ea3](https://github.com/alis-build/adk-launchers-go/commit/4805ea3293f0a3172e6062f860de7b7f9f16f0d5))

IdentityAuthenticator adapts the alis identity that the auth gateway puts
on the request context into ADK's authn.Caller, so ADK's REST server can
tell callers apart. A request without an identity is unauthenticated and
ADK answers 401. The caller's user ID is the identity ID, the same user ID
the AG-UI launcher runs sessions under.

#### web: Require an identity on ADK's REST API by default ([4a56af8](https://github.com/alis-build/adk-launchers-go/commit/4a56af8f5d99feab48037aed958628e8259f67d0))

ADK's REST API ran with no-op authentication and authorization under
this launcher, so any caller could read or write any user's sessions
through /api/apps/{app}/users/{anyone}/... .

buildRouter now fills launcher.Config.Authenticator with the identity
authenticator and Authorizer with authz.NewStrict before any sublauncher
is set up, so the REST server ADK's api sublauncher builds refuses an
anonymous caller with 401 and a caller acting as another user with 403.
ADK's public routes such as /api/health stay open.

A hook the embedder already set is kept. The opt-out is code only
(Config.Authorizer = authz.NewNoop()); there is deliberately no flag.

#### web: Open ADK's web UI as the signed-in caller ([636e832](https://github.com/alis-build/adk-launchers-go/commit/636e832385c0e94b79f07e0ea89af3d68a3c558f))

ADK's web UI acts as user "user" unless the page is loaded with
?userId=, and sends that ID to the REST API in the path, the JSON body
and the query. Now that the REST API refuses a caller acting as another
user, the UI would be refused on every request.

A browser opening /ui/ without a userId query, with an identity on the
request, is redirected to the same URL with userId set to the caller's
identity ID. The UI then sends the real ID everywhere on its own, with
no request rewriting. Anonymous requests, assets and a page already
carrying userId pass through.

#### evals: Require an identity on every evals route ([31dfcf3](https://github.com/alis-build/adk-launchers-go/commit/31dfcf34eec2033087fd00145f8a0986d162bae7))

Anyone who could reach the process could create, edit, delete and run
eval sets, and read sessions through add-session. Every evals route,
canonical and legacy on both bases, is now registered behind
web.RequireIdentity and answers 401 to a caller with no identity before
its handler runs. Eval sets and results stay shared per app.

The routes move into a table so the test can check every one of them
through the host mux.

#### Add a credential-secret redactor matching adk-python's ([365d410](https://github.com/alis-build/adk-launchers-go/commit/365d4102ebaeb7c8688934c8bb241c0858e3de35))

internal/credredact strips credential secrets from decoded JSON with
the same rules, in the same order, as adk-python's
_redact_credential_secrets, so the launchers can return stored payloads
without leaking OAuth client secrets, tokens, PKCE verifiers or API
keys. It never modifies its input.

Expected outputs come from adk-python's own tests and fixtures; the
rule-precedence cases were checked against adk-python's function.

#### evals: Refuse live and audio-simulator run settings for now ([1c59c8a](https://github.com/alis-build/adk-launchers-go/commit/1c59c8a5c6276c9c5669d298ee60feb3eec53bf0))

adk-web main sends live_model_config when live mode is ticked, and may
send user_simulator_config with a type discriminator; this launcher
silently ran a plain run instead. Both fields are now decoded (snake or
camel case) and a run that sets either answers 400 naming the field,
before anything loads or runs. JSON null counts as unset; {} counts as
set, as in adk-python.

#### evals: Match adk-python's run response shape ([4e5e56f](https://github.com/alis-build/adk-launchers-go/commit/4e5e56fab7f51c873291a1e3b9725acc01521497))

Compared with adk-python's captured run response, every run result was
missing the deprecated evalMetricResults field (always [] there) and
every metric result was missing details, which adk-python always builds.
Both are now emitted, including for metrics with no evaluator or whose
evaluator failed, so no "details": null reaches clients.

The response is checked key for key against the captured adk-python
response at the levels adk-web reads, and the v2.7.1 bundle's legacy
run_eval body still answers a bare array.

#### evals: Report metrics-info exactly as adk-python does ([69296ed](https://github.com/alis-build/adk-launchers-go/commit/69296edabd1952858ce6fd776f2bd309c1baa208))

adk-web main skips metrics whose requiresThreshold is false and bounds
threshold inputs by an interval that may be absent. Go sent neither
requiresThreshold nor the interval's open-end flags, and paraphrased
every description.

Each metric now carries adk-python's description verbatim,
requiresThreshold, and a full interval; an interval may be omitted for
metrics without a fixed range. The response is checked field for field
against adk-python's captured metrics-info.

#### evals: Add the INFORMATIONAL eval status ([b3c8167](https://github.com/alis-build/adk-launchers-go/commit/b3c8167e2e99fdec09cf357a839ae2b72183470f))

adk-python added status 4 for metrics that report a value but give no
verdict. Averaging repeated runs turned such a value into PASSED or
FAILED against the threshold, and could pass or fail a case on it. A
metric every run reported as informational now keeps its mean and the
informational status, and informational metrics are skipped when a
case's final status is decided.

#### evals: Name eval sessions adk-eval-session-<uuid> ([d89a9b6](https://github.com/alis-build/adk-launchers-go/commit/d89a9b6b5ff85dcbc86e0b6a4b2464695c8441db))

adk-python switched eval session IDs to a prefix of lowercase letters,
digits and hyphens so they are valid Agent Engine session IDs. Go now
does the same, and IsEvalSessionID recognises both the new and the old
___eval___session___ prefix so embedders that list sessions can hide
eval runs.

#### evals: Match rubric verdicts by echoed rubric ID ([130719f](https://github.com/alis-build/adk-launchers-go/commit/130719ffc80f92fd6a3303877d8939387ef3c685))

Judges sometimes paraphrase a rubric's text, which then matched no
rubric and dropped its verdict. As in adk-python, prompts now tag each
rubric with its ID and ask the judge to echo it, and verdicts match by
ID first, falling back to the rubric text. An ID belongs only to the
property right after it, so an omitted ID cannot shift a later one onto
the wrong rubric. Prompt lines are copied from adk-python.

#### tooloauth: Add the Token credential tools sign requests with ([f7083c1](https://github.com/alis-build/adk-launchers-go/commit/f7083c1eeac27313e81aafb826378a6cfeb6421a))

A stored OAuth token must be a plain value: ADK's OAuth2Credential wraps
a live token source a persistent store cannot encode. Token writes its
bearer header and prints its secrets as &lt;redacted> through fmt and slog.

#### tooloauth: Reject tool configurations the plugin cannot sign in with ([97c7960](https://github.com/alis-build/adk-launchers-go/commit/97c79604ab1c14368f7e0d95d36471edd53a6ca6))

NewPlugin checks each configured tool before any run: a provider name,
a confidential client, an oauth2 authorization code flow or inline
openIdConnect endpoints, and scopes listed once in Tool.Scopes so the
authorization URI, store key and client config cannot disagree. A nil
Store falls back to one process-wide in-memory store, with a warning.

#### tooloauth: Key each stored token by its sign-in inputs ([6c210cd](https://github.com/alis-build/adk-launchers-go/commit/6c210cdb762313be370cd4ab14658064651ea110))

A token is stored under a SHA-256 digest of the scheme, endpoints,
client ID, redirect URI and sorted scopes, length-prefixed so no
character inside a field can make two inputs collide. Two users, or two
scope sets for one provider, never share a token; the client secret is
left out so rotating it does not sign everyone out.

#### tooloauth: Send a configured tool's requests with the stored token ([47093e2](https://github.com/alis-build/adk-launchers-go/commit/47093e2ba1782b5ad5dd24524910506cc14b69a6))

Tools get a credential provider from Config.Provider and send requests
through ADK's auth.Transport; the provider reads the acting user from
the tool's context and serves their stored token. The plugin lets a tool
with a usable token run, leaves tools it does not configure alone, and
answers a store failure with an error that names the provider but not
the user.

#### tooloauth: Refresh an expired token before the tool runs ([76b1be4](https://github.com/alis-build/adk-launchers-go/commit/76b1be4351b289e36de5eb75652e5e993a855ce8))

An expired access token with a refresh token is refreshed through the
token endpoint (client_secret_basic), stored, and kept for
RefreshTokenTTL (90 days by default) from each refresh, so a user who
keeps using a tool stays signed in. Only an invalid_grant answer drops
the stored token; an outage or a misconfigured client keeps the user
signed in and reports the sign-in as unavailable.

golang.org/x/oauth2 becomes a direct dependency at the version already
in the module graph.

#### tooloauth: Tell headless runs to connect in chat instead of pausing ([b00b0b5](https://github.com/alis-build/adk-launchers-go/commit/b00b0b5015f0a38cae26ce348f55edd7be03fa7b))

WithNonInteractive marks a run nobody can answer, such as a scheduled
job or an eval. In such a run a configured tool with no token fails
with "connect &lt;Provider> in chat first" rather than pausing for a
consent that would never come. The text carries no auth URI, since it
is persisted and fed to the model.

#### tooloauth: Pause a tool with no token for the user's sign-in ([d903284](https://github.com/alis-build/adk-launchers-go/commit/d903284440a5b404e11153e64665eed07e8a4896))

A configured tool whose user has no usable token now pauses through
ADK's confirmation request, with adk-python's auth request as the
payload: the scheme with the tool's scopes, the client ID and redirect
URI, and an authorization URI with a fresh random state, offline access
and forced consent. The client secret is stripped before the request is
stored in the session or sent to the client.

#### tooloauth: Finish a sign-in from the client's reply ([cf70754](https://github.com/alis-build/adk-launchers-go/commit/cf7075410758afd57f10a69544b1e395e1b53bc6))

When the user answers the sign-in pause, the plugin checks the reply's
state against the one it sent, exchanges the code with the tool's own
client, stores the token and answers "connected to &lt;Provider>, call the
tool again" without running the tool, so the sign-in's confirmation can
never pass as the tool's own approval. The request is read from the
agent's own events when the run starts, since ADK hides the session from
tool callbacks.

A refresh refused because the user's grant cannot be used (invalid_grant,
invalid_scope, or a bare 400) now drops the stored token so the user is
asked to sign in again; client-setup errors, 401s and outages keep it.

#### tooloauth: Say why a sign-in reply was refused ([77db2fd](https://github.com/alis-build/adk-launchers-go/commit/77db2fd87d1fe30e4c47b0eadca7d15d372a6bc6))

A declined, forged, stale or failed sign-in reply now fails with its own
error naming the provider: declined, no auth response, provider error,
state mismatch, no authorization code, request out of date, token
exchange refused, or sign-in unavailable. The checks run in that order,
so the state is verified before the code is ever sent, and none of them
stores anything. The provider's error description and the token
endpoint's response never reach the model.

#### evals: Run eval inference as a headless run ([d6988ed](https://github.com/alis-build/adk-launchers-go/commit/d6988ed58c29ae2a54fd852f284c792d0da31846))

Nobody can answer a tooloauth sign-in pause during an eval, so eval runs
are marked non-interactive: a tool needing a sign-in fails with
"connect &lt;Provider> in chat first" instead of pausing.

#### scheduler: Run cron jobs as headless runs ([3ac6cdc](https://github.com/alis-build/adk-launchers-go/commit/3ac6cdc65980efcfb568cb83988f560d1f83eff2))

Nobody can answer a tooloauth sign-in pause during a cron run, so every
cron run is marked non-interactive: a tool needing a sign-in fails with
"connect &lt;Provider> in chat first" instead of pausing.

#### agui: Surface an OAuth sign-in pause as an auth_required interrupt ([282ec10](https://github.com/alis-build/adk-launchers-go/commit/282ec108069ebbf543915fc5c674915db704ffdc))

A tool confirmation whose payload is an auth config with an
authorization URI, as tooloauth sends, now becomes an interrupt with
reason auth_required, the config with its secrets stripped under
metadata.adk.authConfig, and a response schema requiring the exchanged
credential. Other confirmations keep their tool_call shape.

#### agui: Advertise auth_required among the interrupt reasons ([f6ef1ca](https://github.com/alis-build/adk-launchers-go/commit/f6ef1cad540c1a4fbc29cdf06b4b81c529325781))

Clients reading capabilities now know to prepare a sign-in UI for
tooloauth's auth_required interrupts.

#### agui: Resume an auth_required interrupt with the filled auth config ([ea34ee9](https://github.com/alis-build/adk-launchers-go/commit/ea34ee98d8d5a6fb63b52415037befb0b9642800))

A resolved resume of a sign-in interrupt now answers the paused call
with the client's filled auth config as the confirmation payload, with
no approved field needed; cancelled declines. The agui docs describe the
sign-in flow end to end.

#### tooloauth: Store tokens in Spanner under Cloud KMS envelope encryption ([f509d7f](https://github.com/alis-build/adk-launchers-go/commit/f509d7f7741990a18846687a665bde36a0cbe1d5))

tooloauth/spannerstore implements ADK's CredentialStore on one Spanner
table. Each token is sealed with AES-256-GCM under a fresh data key that
Cloud KMS wraps, both bound to the row; the row key is a digest of the
app, user and slot, so the table holds no user ID or token in clear.
Only tooloauth tokens are accepted, and a missing table is an error, not
a missing token.

#### tooloauth: Serve a Spanner-stored token only until its expiry ([670c0e6](https://github.com/alis-build/adk-launchers-go/commit/670c0e6ecb96ce6b8e9750798ac4df70bb8e94f7))

A row within ADK's expiry skew of its expiry is a miss, without being
decrypted, and a later Set replaces the expiry either way, so the
plugin's refresh renews a token by storing it again.

#### evals: Keep eval runs off users' stored OAuth tokens by default ([486bcb1](https://github.com/alis-build/adk-launchers-go/commit/486bcb12b0fb9c701ab9837b2ab9ad25307f497e))

Eval runs execute as the caller, so once someone had connected a
provider, an eval of an OAuth tool acted on their real account, writes
included. tooloauth gains a WithoutStoredTokens marker under which every
configured tool fails with "&lt;Provider> is not available in this run",
before any store read, refresh or sign-in. Eval inference sets it unless
the embedder opts in with evals.WithStoredOAuthTokens(), for example to
evaluate against a test account. Cron runs still use the owner's token.

#### tooloauth: Drop Spanner-stored tokens that can never be read ([cb560ca](https://github.com/alis-build/adk-launchers-go/commit/cb560ca4e7f370cf409cc110f4fcde4bb20cfa36))

A row whose token could not be decrypted made every read an error until
it expired, up to 90 days, so the user saw the sign-in as unavailable
and was never asked to sign in again. Such a row is now deleted and read
as no token: a broken seal, an undecodable token, or KMS rejecting its
wrapped key as invalid. Each row records the KMS key that wrapped it, so
a store pointed at the wrong key, which KMS answers the same way,
reports errors and deletes nothing; outages, missing grants and disabled
or destroyed key versions stay errors too. The delete applies only if the
row still holds the data key that was read, so a token stored meanwhile
is kept. KMS errors now carry only their code, never the key's name.

The table gains a KeyName column; spannerstore is unreleased, so no
migration is needed.

#### evals: Read adk-python's multi_run_config in eval configs ([236bee4](https://github.com/alis-build/adk-launchers-go/commit/236bee455405e55f26f8f2a3447d9c8835c51aa7))

EvalConfig accepts multi_run_config in either spelling: num_runs, and
cross_run_criteria of pass_rate, pass_at_k and pass_hat_k with their k
and threshold. Invalid values and unknown keys are rejected with
adk-python's messages, a null is read as unset, and the keys Go does not
port (cross_run_scoring and its kin) are refused by name.

#### evals: Run each eval case num_runs times and label every run ([fc5cb5a](https://github.com/alis-build/adk-launchers-go/commit/fc5cb5ac6773d5a6c3bcd3a1b4a36f5c3e363cdc))

InferenceConfig.NumRuns repeats each case through the same worker pool,
so the run's parallelism bounds the repeats too. Every run, a single one
included, carries its repetition index into its case result, as in
adk-python; results stay ordered case by case.

#### evals: Compute pass rate, pass@k and pass^k over a case's runs ([eb0dea0](https://github.com/alis-build/adk-launchers-go/commit/eb0dea0516e4a1600725cf098ee28e83e308f467))

RunStatus decides each run as adk-python does, leaving crashed and
partly unevaluated runs out; PassRate, PassAtK and PassHatK are the
unbiased estimators over the decided runs.

#### evals: Judge a case across its runs with pass rate, pass@k and pass^k ([824a79d](https://github.com/alis-build/adk-launchers-go/commit/824a79d4b983270d71da4fdf0c8f26b3ccbd3b88))

EvaluateEvalSet runs each case num_runs times in one inference call (the
argument, else multi_run_config.num_runs, else 2), refuses a pass@k or
pass^k criterion that looks at more runs than that before any inference,
and reports each cross-run criterion's value, threshold, status and run
count on the case, apart from the metric results. A failed criterion
fails the case; a criterion without a threshold only reports.

#### evals: Record each model call's usage on invocation events ([f5480e8](https://github.com/alis-build/adk-launchers-go/commit/f5480e8bdb128c83dc54e28c6b1ee2a63b9fafb2))

Invocation events now carry the token usage and model version of the
model call behind them, and the final response's event is kept, without
repeating its text, so its call's usage is not lost. adk-python does the
same since 2d428ead3; the efficiency metrics read both.

#### evals: Add adk-python's informational efficiency metrics ([73dc904](https://github.com/alis-build/adk-launchers-go/commit/73dc904edb9053335cf0ee6fdf07cd5709b489cb))

tool_call_count_v1, inference_call_count_v1, token_usage_v1 and
invocation_duration_v1 report per invocation and on average, with token
usage broken down by type and duration split into model, tool and
framework time. They never pass or fail a case, and refuse a threshold.
Their results match adk-python's for the captured fixture, field for
field.

#### evals: Time each eval turn and split it into model and tool time ([e2ea677](https://github.com/alis-build/adk-launchers-go/commit/e2ea6775578fa036818340592f92417e1d92c24f))

Non-live inference measures each turn's wall-clock seconds and, through
a plugin that runs before every other, the time spent awaiting a model
and inside tools, overlaps counted once. The invocation_duration_v1
metric reads them. As in adk-python, the simulator's time stays out and
live turns are not timed. An ADK agent tool's sub-agent runs without
plugins, so its model time counts as tool time.

#### evals: Fold standalone usage reports into their model calls ([ea81739](https://github.com/alis-build/adk-launchers-go/commit/ea8173947bcc8ce15bc702a4ed2f9c01e5f303bc))

A live connection can report a call's token usage in an event of its
own. Such a report now joins the nearest compatible model event (same
author, same model, no usage yet), so its tokens count without the
report counting as a second model call; an unmatched report stays an
event of its own, as in adk-python.

#### evals: Report the efficiency metrics on every eval run ([bb6e3dc](https://github.com/alis-build/adk-launchers-go/commit/bb6e3dc8e03ede767d96dcb794ada648585a8a92))

run_eval and EvaluateEvalSet append tool_call_count_v1,
inference_call_count_v1, token_usage_v1 and invocation_duration_v1 to
the metrics asked for, as adk-python does; a metric the caller already
named is kept as written. Averaged over several runs, a metric no longer
carries one run's token or duration breakdown next to the mean. Go's
results for adk-python's captured inference now match it field for
field.

#### evals: Compare tool names only when a trajectory criterion sets ignore_args ([8f2fab1](https://github.com/alis-build/adk-launchers-go/commit/8f2fab10715bf5f44b7c5f211bab80157c544218))

The tool trajectory criterion now accepts ignoreArgs (or ignore_args),
matching adk-python: every match type then compares tool names and skips
arguments. Either key on its own selects the trajectory criterion, and
match_type is accepted alongside matchType, as pydantic's populate_by_name
allows.

#### evals: Read parallelism from the eval config in EvaluateEvalSet ([6c9e4f0](https://github.com/alis-build/adk-launchers-go/commit/6c9e4f0106100e77e35e4b5c1309022978edf422))

EvalConfig.parallelism, as in adk-python, limits how many cases run and
are scored at once. Unset keeps the service default; a value below 1 is
rejected when the config is read.

#### evals: Accept parallelism on the run_eval request ([248fd3d](https://github.com/alis-build/adk-launchers-go/commit/248fd3d4c6299d34fbf425d3cb090ba6e51c308f))

POST .../run takes an optional parallelism, as adk-python's RunEvalRequest
does, and runs and scores at most that many cases at once. A value below
1 is a 400.

#### evals: Store eval sets and results under a file:// URI ([a0c966c](https://github.com/alis-build/adk-launchers-go/commit/a0c966ca59fe26239e4652d3c1345ade02130113))

--eval_storage_uri (or the WithEvalStorageURI option) now takes
file:///abs/path as well as gs://bucket, and ADK_EVAL_STORAGE_URI
supplies the URI when neither is set, as in adk-python. The URI is split
the way Python's urlparse does: the host must be empty or localhost (so
file://user:pw@localhost/x is rejected), the path absolute, and a stray %
is kept. The directory is created; any other scheme is an error.

#### evals: Decode user_simulator_config by its type ([1ba8d94](https://github.com/alis-build/adk-launchers-go/commit/1ba8d940c695b1f4c26ca9023e81b96c1b14d064))

ParseUserSimulatorConfig reads the config's "type" as adk-python does:
missing or null is llm_backed (configs written before the field), and
llm_audio is the new audio simulator, with adk-python's defaults
(cloud_tts, the en-US-Studio-O voice, text kept with the audio). Keys may
be camelCase or snake_case, and the eval config also accepts the
user_simulator_config key.

#### evals: Resample synthesized speech to 16 kHz for live input ([128ba95](https://github.com/alis-build/adk-launchers-go/commit/128ba956fc22cf3575c3a23aa3fd6d534c8180d7))

A port of adk-python's _audio_utils: ParseSampleRate reads a whole rate=
MIME parameter, ResamplePCM16 resamples 16-bit PCM by linear
interpolation, and ToLiveInput takes text-to-speech audio (24 kHz when
the MIME type does not say) to the 16 kHz a live session accepts.

#### evals: Speak simulated user turns through an audio model ([949622b](https://github.com/alis-build/adk-launchers-go/commit/949622bae0b8dca0617d26aea3108059077a4a10))

AudioUserSimulator wraps any text user simulator, as adk-python's
_LlmAudioUserSimulator does: each text turn is sent to the audio model,
and the user message carries the speech resampled to 16 kHz, after the
text unless include_text_with_audio is false. The simulator provider
wraps static and scenario cases alike when given an llm_audio config,
resolving the audio model through ADK's model registry, and now takes a
context.

#### evals: Stream audio user turns to live agents and grade transcripts ([c5f5d18](https://github.com/alis-build/adk-launchers-go/commit/c5f5d1882e5b6b047e87cd747e36e49ee370b96f))

In a live eval, a user turn with audio is now streamed as realtime input
between activity markers, in 16000-byte chunks, with the model's voice
activity detection off, as adk-python does. The logged turn keeps the
text and audio. Final transcription events become text content, both in
the history the user simulator reads and before grading, and a trailing
audio-only final event no longer replaces a text final response.

#### evals: Run live, audio-simulated evals from the run request ([8aa9946](https://github.com/alis-build/adk-launchers-go/commit/8aa9946c2eb058aa50e06e43783fe54aea9b73aa))

live_model_config and user_simulator_config on POST .../run (and the
legacy run_eval) are now honoured instead of answered with a 400. A set
live_model_config, even {}, runs inference live with its timeout (the
camelCase key winning, as in pydantic). A user_simulator_config is parsed
by its type, and an llm_audio config's audio model is resolved once per
request, so an unknown model is a 400 rather than a failure in every
case. The launcher's own simulator provider is never changed.

#### evals: Reuse a pinned eval session, only ever the caller's ([b7568d1](https://github.com/alis-build/adk-launchers-go/commit/b7568d1b149353bfcd9eabdaa87f4d9cee300de0))

An eval case's session_input.sessionId pins it to a prepared session, as
in adk-python: the run uses that session ID, and when the session exists
it keeps its own state and history. The lookup is made only as the user
the run runs as (the caller on the HTTP path, else the case's user), so a
case naming another user's session gets a fresh session of that ID under
the caller and never reads the other. With num_runs above 1 the pin is
dropped, with a log line, so the runs do not share a session.

#### evals: Run a metric's LLM-judge calls in parallel ([ec7fc6b](https://github.com/alis-build/adk-launchers-go/commit/ec7fc6bc393dc7f8df4bde68f8eb7e7ff901549b))

judgeModelOptions.parallelismLimit, as in adk-python, lets one metric
make that many judge calls at once across its invocations and samples
(one at a time when unset; below 1 is rejected). A failed judge call now
leaves only its own invocation NOT_EVALUATED, with a log line, instead of
failing the whole metric.

#### evals: Judge user-simulator turns in parallel, keeping turn order ([80c293f](https://github.com/alis-build/adk-launchers-go/commit/80c293fb4d892643cf2d574f78ceafc4499d49ed))

per_turn_user_simulator_quality_v1 now judges its turns and the
stop-signal turn concurrently, every judge call across turns and samples
sharing one judgeModelOptions.parallelismLimit, as in adk-python.
Results stay in turn order, and the first failed call cancels the rest
and is returned.

This metric's judgeModelOptions (numSamples, the judge model) were
ignored whenever its criterion set a stopSignal; they are now read.

### Fixes

#### scheduler: Don't retry a cron tick that spent its model-call budget ([b057b91](https://github.com/alis-build/adk-launchers-go/commit/b057b9122b7bf7042297c692107568d519bc97eb))

ADK v2.7 ends an invocation after 500 model calls with
agent.ErrLLMCallsLimitExceeded. With WithSynchronousExecution an agent
failure answers 500, so Cloud Tasks would retry the tick and spend the
whole budget again on every attempt, failing the same way each time.

Such a run is now acknowledged with 200 and status FAILED; the failure
is already recorded on the cron. Other agent failures keep the
retryable 500.

#### web: Refuse an alis identity that carries no ID ([17561a6](https://github.com/alis-build/adk-launchers-go/commit/17561a6581e62dd8a2f46ece311a767a3d11a906))

An identity with an empty ID became a caller with user ID "", which
authz.Strict lets act as the empty-string user. iam.FromHeader does not
validate the ID, so such an identity is now unauthenticated (401).

Also corrects the opt-out comment: authz.NewNoop() still requires a
signed-in caller; a fully open API also needs authn.NewNoop().

#### web: Treat an identity with no ID as missing in RequireIdentity ([a47e5df](https://github.com/alis-build/adk-launchers-go/commit/a47e5df11ce07b6d2bd4857eebbf04599029b252))

iam.FromContext does not check the ID, so RequireIdentity let an
identity with an empty ID through, and a handler behind it would act as
user "". It now answers 401 for that identity too, the same rule the
REST API authenticator applies.

#### evals: Make add-session act only as the caller ([6f2eef3](https://github.com/alis-build/adk-launchers-go/commit/6f2eef3e5a7d28243eb7596ec419c88005e03180))

add-session took the user from the request body, so any caller could
copy another user's session into an eval set. The session is now read,
and the new case stored, under the caller's identity ID. A body userId
naming anyone else is refused with 403 before any session is read, a
missing one defaults to the caller, and an identity without an ID is
refused with 401.

#### evals: Run eval inference as the caller on the route's app ([73f0aab](https://github.com/alis-build/adk-launchers-go/commit/73f0aab6c9d97f4fa3d19b9613ab667b1279727f))

Eval runs write real sessions, and inference took the user and app from
the stored eval case. A case can be written by anyone with access, so a
run could write sessions into another user's space or run another app's
agent.

Inference now runs as the caller's identity ID and on the runtime's own
app, which the launcher builds for the route's {app_name}; the case's
stored user and app are ignored, and the case itself is not modified.
Results report the user the run actually used, so their session details
resolve, including for cases whose inference failed.

#### web: Skip the web UI redirect for an identity without an ID ([6053111](https://github.com/alis-build/adk-launchers-go/commit/60531111f62c8c2a24f42fdd26b84416bb352488))

An identity with an empty ID was redirected to /ui/?userId=. Every REST
call then answered 401 anyway, but the redirect now follows the same
rule as the authenticator and RequireIdentity and leaves the page alone.

#### evals: Redact credential secrets from eval case and result reads ([5dc562a](https://github.com/alis-build/adk-launchers-go/commit/5dc562a31cbdaa7d75920a4417e2f53579716f5f))

A recorded conversation can hold an adk_request_credential call carrying
an OAuth client secret, tokens or a PKCE verifier, and get-eval-case and
get-eval-result returned them as stored. Both now answer with credential
secrets stripped, matching the endpoints adk-python's dev server
redacts. The stored data is unchanged.

The redactor now decodes numbers exactly, so large integers in a stored
result are not rounded on the way out.

#### evals: Redact credential secrets from the get-eval-set response ([9b4f62c](https://github.com/alis-build/adk-launchers-go/commit/9b4f62ca2578ad1bc76ad3c358835405cdfd505b))

GET eval-set returns every case's conversation, so it leaked the same
credential secrets the eval-case read now strips. adk-python has no such
route; it is redacted like a single case.

#### evals: Name the user on eval case results that failed to evaluate ([7c14db6](https://github.com/alis-build/adk-launchers-go/commit/7c14db6e4662dbc2cb20d9ebf3f9a06f47f4c16c))

When evaluating a case errored, its result carried no user, so adk-web
could not open the session it ran in. Failed results now get the same
user and session details as successful ones, as adk-python sets user_id
on every case result.

Also pins the saved result ID shape (${app}_${evalSetId}_${timestamp})
that adk-web rebuilds from a deep link.

#### evals: Answer 404 for a missing session or eval set on add-session ([12c5f18](https://github.com/alis-build/adk-launchers-go/commit/12c5f183faef3694feb100adb89f4dd5b6b5f498))

adk-python answers 404 when add-session names a session or eval set that
does not exist; this launcher answered 400. Both now answer 404, the
missing session with adk-python's "Session not found" detail.

#### evals: Keep a metric informational when some runs are not evaluated ([58970e9](https://github.com/alis-build/adk-launchers-go/commit/58970e9176992a9ec8788a569e8fb52fea9059d4))

A run that was not evaluated gives no verdict, but it cleared the
informational flag, so the mean of the other runs was graded PASSED or
FAILED again. A metric is now informational when some run reported it
so and no run passed or failed it.

#### evals: Answer 404 only for a session that does not exist ([116ea58](https://github.com/alis-build/adk-launchers-go/commit/116ea58343d8721d3acf7aa621eb50ce850e4fee))

add-session answered 404 for any session-store error. Only a missing
session (session.ErrNotFound, or no session returned) is a 404 now; a
failing store is a server error, as adk-python answers 404 only when the
session is absent.

#### evals: Grade metrics with the criterion's threshold ([a24bfac](https://github.com/alis-build/adk-launchers-go/commit/a24bfacfe2b683aeeee51868f11fb9faa3b46397))

adk-python grades a metric with its criterion's threshold whenever a
criterion is set. Go graded every metric except trajectory with the
top-level threshold, so a run sent over HTTP with a criterion threshold
was graded against the wrong number (often 0, which passes everything).

models.EffectiveThreshold applies adk-python's rule, and the registry
resolves it once before building any evaluator. Trajectory's own partial
override is gone; the response still echoes the request's top-level
threshold, as adk-python does.

#### evals: Leave model thought parts out of scoring ([2824221](https://github.com/alis-build/adk-launchers-go/commit/28242217b4996573ab6e7497cb00953a981f76c1))

Thought parts are the model's reasoning, not its response. They were
scored by ROUGE and sent to judges as part of the response and the
multi-turn dialogue. They are now left out everywhere metrics read text,
as in adk-python.

#### evals: Record a turn the agent answers with no events ([ce5be19](https://github.com/alis-build/adk-launchers-go/commit/ce5be198e1c084721c00768ba550601d5895d1c5))

A turn the agent answered without any events left no invocation, so the
case had fewer invocations than turns and scoring failed with a length
mismatch. The user's message is now recorded under a fresh invocation
ID, as in adk-python.

#### evals: Score non-English text with ROUGE ([32b2212](https://github.com/alis-build/adk-launchers-go/commit/32b2212d8c8f6e17dfbd42422758228c5cdbecf0))

The ROUGE tokenizer kept only [a-z0-9]+, so Thai, Chinese, Arabic,
Japanese or Cyrillic responses tokenized to nothing and always scored 0.
It now follows adk-python's Unicode-aware tokenizer: NFKC-normalised,
CJK characters as their own tokens, Thai-family scripts split into base
characters with their combining marks attached, other letters and
numbers kept as words. ASCII words are tokenized as before, so English
scores do not change.

#### evals: Lowercase ROUGE input with full Unicode rules ([2569e4e](https://github.com/alis-build/adk-launchers-go/commit/2569e4e74b31073805b8312bac4209ab781c9a92))

strings.ToLower maps rune by rune, so Greek final sigma and Turkish
dotted capitals tokenized differently from adk-python, whose str.lower()
applies the full Unicode rules. The tokenizer now uses the same rules,
and a pin keeps Devanagari vowel signs inside their word.

#### evals: Use the top-level threshold when a criterion sets none ([53bc58c](https://github.com/alis-build/adk-launchers-go/commit/53bc58c767eb86acb90e5ea2f87efe7d9d6b38bb))

A criterion without a threshold decodes as 0, which passes every score.
adk-python rejects such a request; here the metric's top-level threshold
applies instead, as trajectory metrics always did.

#### evals: Score nothing from a partial rubric verdict reply ([dedea1c](https://github.com/alis-build/adk-launchers-go/commit/dedea1c19ef666bd3c72f4add12faf19f4cae4b5))

The rubric parser paired Property, Rationale and Verdict lines up to the
shortest list, so a judge reply missing a verdict silently dropped that
rubric and could inflate the score. As in adk-python, a reply whose
counts differ now scores nothing, and a reply with no usable verdict
leaves the sample unscored (NOT_EVALUATED) instead of failing the metric.
The line patterns follow adk-python's.

#### evals: Match rubric text that the judge decorated ([c0a3dc9](https://github.com/alis-build/adk-launchers-go/commit/c0a3dc9230d2fac4727cd938c658f992daa66273))

Judges wrap the rubric text they echo in markdown and typographic
decoration (bullets, bold, headings, smart quotes, dashes, extra
spaces), which defeated the text lookup. Echoed and rubric text are now
normalised the way adk-python does before matching, keeping accented
characters.

#### evals: Leave a rubric sample unscored when a rubric gets no verdict ([718fab1](https://github.com/alis-build/adk-launchers-go/commit/718fab1c84aeff16e916bf4d8333227f2293abfc))

A judge reply that answered only some rubrics was averaged over the
answered ones, so an unanswered and possibly failing rubric dropped out
of the score. As in adk-python, a reply that leaves any rubric without a
yes/no verdict is now unscored; with several samples, the scored ones
decide.

#### evals: Read final_response_match_v2 judge labels as adk-python does ([c1fe76d](https://github.com/alis-build/adk-launchers-go/commit/c1fe76d2ccfd68f9fc89aaa516ecbf72ccaa0af9))

The judge's label was read as strict JSON from is_the_agent_response_valid
only: an unknown label failed the whole metric, the alternate
is_the_agent_response_invalid field was ignored, and with several samples
only the first counted. The parser now ports adk-python's: tolerant
patterns for both fields, its label table, and an unscored sample for an
unknown label; the majority of scored samples decides.

#### evals: Fail a repeated-run case whose inference crashed ([d369ccb](https://github.com/alis-build/adk-launchers-go/commit/d369ccb97abf34d4a9af725893485c057f13e861))

Averaging repeated runs decided a case's status from metric results
alone, so a case whose runs all crashed before any metric ran came out
NOT_EVALUATED. As in adk-python, a run that failed without producing any
metric result now fails the case. A metric that simply produced no score
stays NOT_EVALUATED.

#### evals: Give the rubric judge the agent's instructions without events ([24083b4](https://github.com/alis-build/adk-launchers-go/commit/24083b4f770cd69cb8e1bb5644262236575e477d))

The final-response rubric prompt took the agent from the first invocation
event, so an agent that answered without tools or events reached the
judge with empty developer instructions. It now falls back to the app's
agent, as adk-python does; with several agents, the smallest name stands
in for adk-python's first-inserted one, since Go maps keep no order.

#### evals: List each agent's tools, built-in ones included, for the judge ([2e5d793](https://github.com/alis-build/adk-launchers-go/commit/2e5d7932c72639955856043d6927d8e5fd06f540))

The multi-turn judge prompt de-duplicated tool lines across all agents,
so a second agent sharing a tool lost it; it skipped built-in tools such
as google_search entirely; and its agent order was random. As in
adk-python, tools are now listed per agent and de-duplicated within it,
built-in tools are named, and agents appear in a stable order.

#### evals: Keep the model's grounding metadata on invocation events ([551f286](https://github.com/alis-build/adk-launchers-go/commit/551f286a80da2080aa1040c1875b068151aa65fe))

Eval inference dropped the grounding metadata the model reports, such as
the web searches behind an answer, and dropped the final response event
outright, so a judge never saw what grounded the reply. As in adk-python,
invocation events now carry the grounding metadata, an event with
grounding but no content is kept, and the final response event is kept
for its grounding, without repeating its text.

#### evals: Give the final-response rubric judge the grounding metadata ([653ea30](https://github.com/alis-build/adk-launchers-go/commit/653ea300fda17b60aae625fa6e17c799e6c339e2))

The rubric judge for final responses saw only tool calls as evidence, so
an answer grounded by a model-internal tool such as google_search, whose
search results never appear as a tool response, could not be verified
and was marked down. As in adk-python, the prompt now treats the model's
grounding metadata as trusted evidence and lists it, per event, in a
&lt;grounding_metadata> block.

#### tooloauth: Refresh a token once when parallel calls find it expired ([f68b98a](https://github.com/alis-build/adk-launchers-go/commit/f68b98a6c685992fd46f615409a33d2199594920))

ADK runs a turn's tool calls in parallel. Two calls finding one expired
token both refreshed it; with a provider that rotates refresh tokens the
second refresh was refused, and its invalid_grant deleted the token the
first had just stored, signing the user out mid-turn. Refreshes now run
one at a time per store key and re-read the store first, and a refused
refresh keeps the entry when another server has stored a different
refresh token meanwhile. The plugin also keeps its own copy of the
Config, so a caller changing it afterwards changes nothing.

golang.org/x/sync becomes a direct dependency at the version already in
the module graph.

#### tooloauth: Never complete a sign-in twice or lose its request mid-run ([9645561](https://github.com/alis-build/adk-launchers-go/commit/964556115631d2b0b9e40cb7317b8e875a7e66c2))

Replaying a sign-in reply sent the spent code to the provider again,
which providers answer by revoking the tokens it issued. A request
followed by a reply to its call is now answered and refused, and a resume
claims its request before the exchange, so a concurrent duplicate cannot
send the code either.

The record of a run's sign-in requests was dropped when a run ended, but
ADK reports a run's end even to plugins whose start hook another plugin
skipped; a resume in flight could then lose its request, and a bare
confirmation pass as the tool's own approval. Records are now refreshed
at each run's start and pruned an hour after the last. Two requests for
one call that disagree on the state are refused.

#### tooloauth: Record sign-in requests only for runs that answer one ([4283427](https://github.com/alis-build/adk-launchers-go/commit/428342709cf66cff5e4a855e0670025bc171b4ef))

Only a run whose new message answers a confirmation can resume a paused
call, so other runs no longer read and keep the session's sign-in
requests. A lookup keeps its record alive, and a repeated request for a
call no longer clears that it was answered or conflicting.

#### tooloauth: Share one refresh between the plugin and providers ([09c6a97](https://github.com/alis-build/adk-launchers-go/commit/09c6a977a4a0628e8237b653e3d8bcf88195d1f6))

The plugin and each Config.Provider held their own refresh lock, so a
token expiring between the plugin's check and the tool's request could
be refreshed twice, and with a rotating provider the loser reported the
user as not connected. Refreshes are now shared per store and key across
the package. A RefreshTokenTTL no longer than ADK's expiry skew, which
stored nothing and looped every sign-in, is rejected. The docs say that
the client's sign-in reply stays in the thread's history and that a
revoked token is served until its entry expires.

#### evals: Let the agent's own after-model callbacks run during evals ([8292844](https://github.com/alis-build/adk-launchers-go/commit/829284408be9dd5cfd4fc0dc358e424e9630c565))

The eval request recorder returned the model's response from its
after-model callback, and ADK stops at the first callback that returns
one, so an agent's own after-model callbacks never ran during eval
inference: evals tested a different agent than production. It now tags
the response in place and returns nothing, as adk-python's does.

#### evals: Let ignoreArgs win over ignore_args, as pydantic does ([65eba55](https://github.com/alis-build/adk-launchers-go/commit/65eba55d651376985f2f969cfd6293ae575e6b26))

Found in the P3 phase review: with both keys sent, the snake_case one
won, the opposite of pydantic and of matchType. The run-endpoint
parallelism tests now also check every case returns a result.

#### evals: Give each audio request its own model config ([d08bd10](https://github.com/alis-build/adk-launchers-go/commit/d08bd101f7fd4d3c4643ddea3076b06689e797c9))

Found in review of 949622b. Every case's audio simulator shared the run's
GenerateContentConfig, and ADK's gemini model writes headers into the
request's config, so two cases speaking at once could crash the server
with a concurrent map write. Each request now gets a copy.

The scenario's text simulator also now takes the run's text settings
(model, turn limit, instructions) from an llm_backed or llm_audio
user_simulator_config, as adk-python builds it, through the new
LlmBackedConfigFactory that DefaultLlmBackedFactory implements.

#### evals: Stop the live event reader when a live eval returns ([10274ce](https://github.com/alis-build/adk-launchers-go/commit/10274ce6aa0f2b356be8333c01e7ba9ae1c26fef))

Found in review of c5f5d18. When a turn timed out while the model kept
streaming, the reader goroutine blocked on a full buffer forever and
held the live stream open; it now stops when inference returns. A
cancelled run reports the cancellation rather than a timeout. Tests now
cover grading the transcript of an audio-only reply.

#### evals: Treat blank pinned session and user IDs as unset ([10c1374](https://github.com/alis-build/adk-launchers-go/commit/10c13744b01629eca58ae39243926b95bbe4c04d))

Found in review of b7568d1: the pinned-session lookup used an untrimmed
user while the run uses the default eval user for a blank one, and a
blank pin replaced the fresh session ID. Both now follow the generator.
The no-session-service test also checks that the case state is applied.

#### evals: Only reuse pinned sessions that are eval sessions ([c69a6ce](https://github.com/alis-build/adk-launchers-go/commit/c69a6cecdb528b4d1f004fa17778eb7d84627968))

A case is shared by everyone using the app, so a case pinning someone's
real session ID would make their run of it write eval turns into that
session and save its state in the app-wide eval results. A pin is now
honoured only when it is an eval session ID (adk-eval-session- or the
legacy prefix); any other pin starts a fresh session, with a log line.
This departs from adk-python, which reuses any session ID.

#### evals: Add only the efficiency metrics a registry has, and log failed inference ([d860667](https://github.com/alis-build/adk-launchers-go/commit/d8606675c06f48b8ce9fedd3a1b12e002f26e513))

Found in the whole-track review. Every run added the four efficiency
metrics; with a custom registry lacking them each scored NOT_EVALUATED,
which left every run undecided, so pass@k and pass_rate had no value.
Runs now add only those the registry provides, and a requested
efficiency metric that cannot be built, such as one given a threshold,
is refused before anything runs (a 400 on the run endpoint), as
adk-python refuses it.

A failed inference recorded only that the case failed; the reason (no
simulator factory, a live timeout, failed speech synthesis) is now
logged, as adk-python does. The ADK_EVAL_STORAGE_URI log line no longer
prints the URI, which can carry credentials.

#### evals: Grade a live user's transcript as their turn, not the agent's reply ([7a6642e](https://github.com/alis-build/adk-launchers-go/commit/7a6642e5e1bc8c6a4b42cf07ed7f1a3ef90c0c7a))

Found in the live audio hand check. ADK Go authors the transcript of what
the user said as the agent, so a live eval graded the user's own words as
the agent's final response. Input transcripts are now authored by the
user, as adk-python's are, so they become the invocation's user content.

#### evals: Accept metric_name and custom_function_path in eval metrics ([809a017](https://github.com/alis-build/adk-launchers-go/commit/809a017530213d90c2feac5963e934bc7c3605f5))

Found in the hand check: an eval_metrics item with adk-python's field
name metric_name decoded with an empty name and scored NOT_EVALUATED.
Both spellings are now accepted; the camelCase alias wins when both
appear, as in pydantic.

#### evals: Leave the efficiency metrics out of metrics-info ([6bfe99b](https://github.com/alis-build/adk-launchers-go/commit/6bfe99b9ccb7ddabb2730e2f49af987527cb06af))

Found in the adk-web hand check: the adk-web bundled with ADK Go 2.7.1
crashes opening its eval Run dialog, because the efficiency metrics have
no value range. They take no threshold and are added to every run, so
there is nothing to choose; current adk-web hides them too. They are no
longer listed, a departure from adk-python's metrics-info, and runs
still report them.

#### evals: Default a Gemini audio model to Gemini's own voice ([4f09bc4](https://github.com/alis-build/adk-launchers-go/commit/4f09bc4a22f13985b3d24072f6b06d0079a2eeea))

Found in the live audio hand check: with no audio_model_configuration an
llm_audio run failed with 400 INVALID_ARGUMENT on every Gemini TTS model,
because adk-python's default voice, en-US-Studio-O, is a Cloud
Text-to-Speech voice, and Go has no cloud_tts engine for it to suit. A
gemini-* audio model with no configuration now asks for AUDIO output in
Gemini's default voice; other models keep adk-python's default.

#### evals: Name the Gemini voice Kore in the default audio config ([5f6c07c](https://github.com/alis-build/adk-launchers-go/commit/5f6c07cff2fedfb7b0a328e5d4005ce69e67527c))

Found on the test bed: Vertex AI rejects a Gemini TTS request that asks
for audio but names no voice (400 INVALID_ARGUMENT), so 4f09bc4's default
still failed there. A gemini-* audio model with no configuration now asks
for AUDIO in the prebuilt voice Kore, which Vertex accepted.

#### agui: Stop sending every streamed reply twice ([7d56462](https://github.com/alis-build/adk-launchers-go/commit/7d5646202fa62cca3ad154564fbaaee3787e8aa3))

Found in the tooloauth hand check: with ADK Go v2.7.1 every streamed model
reply reached AG-UI clients twice, even a plain one. TurnComplete closes
the message and the producer's step; ADK's trailing final, repeating the
streamed text, then re-opened the step, and that step change reset the
streamed text the repeat is recognized by, so it went out as a second
message. Any producer with a step was affected: a sub-agent, or the root
agent once ADK stamps it with a node path.

The streamed text now survives a step change back to the producer that
streamed it, and a trailing final that only repeats it no longer
re-opens an empty step.

### Refactoring

#### evals: Let a judge reply leave a sample unscored ([9211d5b](https://github.com/alis-build/adk-launchers-go/commit/9211d5b9fcbf33f42e7a83d68b23b549f2b9323d))

Judge parsers now return an optional score. A sample without one does
not vote and is NOT_EVALUATED, as in adk-python, instead of the parser
having to fail the whole metric. Every current parser still returns a
score whenever it succeeds, so behaviour is unchanged.

### Documentation

#### Document identity on ADK's REST API, the web UI and evals ([b0dc2a7](https://github.com/alis-build/adk-launchers-go/commit/b0dc2a7d356d4712ab95c9cf4f2901bfeab87b66))

Describes the identity defaults on ADK's REST API and the code-only
opt-out, that ADK's A2A routes are not covered, the /ui/ ?userId=
redirect, and that the upstream gateway must forward x-alis-identity on
page navigations and the /run_live WebSocket upgrade for the web UI to
work. The evals docs replace the old "routes do not enforce identity"
note with the identity rules for every route, add-session, runs and
redacted reads.

#### Say which web UI calls are refused without a forwarded identity ([aa91ef6](https://github.com/alis-build/adk-launchers-go/commit/aa91ef621a07e0be2207d95aaca8145c0dd64c39))

Calls that name no user, such as list-apps, still succeed, and ADK's
public routes stay open; only calls naming a user answer 403, and a
/run_live upgrade without an identity answers 401.

#### evals: Document run shapes, eval sessions and score changes ([8f304c7](https://github.com/alis-build/adk-launchers-go/commit/8f304c7a0ec49f7f7b5dfc22902759e3601aa4d0))

Describe the run body forms accepted, the 400 for live and simulator
configs, the canonical and legacy run response shapes, eval session IDs,
the INFORMATIONAL status, and that runs execute as the caller. The
upgrade notes warn that eval scores can change without any agent
change.

#### evals: Name the bundle's run fields and fix a stray doc comment ([11da771](https://github.com/alis-build/adk-launchers-go/commit/11da771ce22445f409f8525bf67ca737f10c4b95))

The v2.7.1 bundle sends evalIds, not evalCaseIds. promptAgentName had
been inserted between formatRubricFinalResponsePrompt and its doc
comment, so godoc gave that comment to the wrong function.

#### tooloauth: Say which Config fields the plugin copies ([8e823fc](https://github.com/alis-build/adk-launchers-go/commit/8e823fc480c31dd63ceb678ce246c27161267157))

#### agui: List auth_required in the interrupt limits and say what is stripped ([c0486c4](https://github.com/alis-build/adk-launchers-go/commit/c0486c40cf68ca922b58e4ffe20c4c370033ced5))

#### tooloauth: Document setup, the resume flow, storage and limits ([f020fef](https://github.com/alis-build/adk-launchers-go/commit/f020fef9b61a92086dd61113f7a4561b076cd0c7))

The package docs cover setting up the plugin and a tool's provider, what
the plugin does on a hit, a refresh, a pause and a resume, how AG-UI and
REST clients resume a sign-in, bringing your own store or using
spannerstore, revocation, headless runs, and every known limit.
spannerstore documents its setup and KMS key rotation. The README and
root package list name the new package.

#### evals: Document storage URIs, run options and the new metrics ([b8acacc](https://github.com/alis-build/adk-launchers-go/commit/b8acaccccc2b5c38882b229819839ccb763084bd))

The evals package doc and README now cover file:// storage and
ADK_EVAL_STORAGE_URI, the run options (parallelism, live_model_config,
user_simulator_config with the audio simulator), the efficiency metrics
added to every run, and pinned sessions. The metrics, simulation and
storage package docs and EvaluateEvalSet cover ignoreArgs,
parallelismLimit, the audio simulator and multi-run. The statements that
live and simulator runs answer 400 are removed.

#### evals: Say judge parallelism is per case and the client must be concurrency-safe ([f9e039d](https://github.com/alis-build/adk-launchers-go/commit/f9e039dae68f0a095916a084225d70c889919262))

Found in the whole-track review: a run scores cases concurrently, so it
can make parallelism × parallelismLimit judge calls at once, and
token_usage_v1 breaks tokens down by type, not by model.

#### evals: Warn that a judge outage can let a case pass ([b0fa8e6](https://github.com/alis-build/adk-launchers-go/commit/b0fa8e6641f54236219a2d6081f4ba998db75fee))

A failed judge call leaves only its invocation NOT_EVALUATED, as in
adk-python, and the score averages the graded invocations, so an
ungraded turn does not count against the case. The metrics package doc
now says so and says what to check when pass rates jump.

#### evals: Note that live runs report no token usage from ADK Go ([378752d](https://github.com/alis-build/adk-launchers-go/commit/378752d555a385fc76c7143e45ea19bf201ec41d))

ADK Go's live connection (v2.7.1) sets no usage metadata, so live runs
report 0 model calls and no token usage, where adk-python reports both.

### Other

#### deps: Bump ADK to v2.7.1 and refresh the module graph ([0bde2ba](https://github.com/alis-build/adk-launchers-go/commit/0bde2bac787e5775724d771fc5c694dac9cc3b0f))

google.golang.org/adk/v2 moves from v2.3.0 to v2.7.1, alongside the
go.alis.build packages (iam v3.16.0, mux v0.7.0, lro v2.10.0, alog
v0.2.2, agui history/scheduler protos), genai v1.77.0, grpc v1.85.0 and
the indirect cloud and OpenTelemetry modules. ADK v2.7 requires Go
1.26.10, so the go directive follows.

Build, vet and tests pass unchanged. The runtime behaviour ADK changed
in these releases (the 500 model-call budget, the GenAI semconv v2
telemetry schema, origin checks on its REST API) is handled and
documented in the commits that follow.

#### Stop ignoring conductor/ from the root .gitignore ([7ac2a6e](https://github.com/alis-build/adk-launchers-go/commit/7ac2a6ebdc95ba24d67bee9db482a9ecd67f32db))

Conductor now keeps its own conductor/.gitignore, so the root entry is
redundant.

## [1.3.0](https://github.com/alis-build/adk-launchers-go/compare/v1.2.0...v1.3.0) (2026-09-09)

AG-UI stream lifecycle fixes and an ADK web launcher catch-up.

- Sub-agent activations always close, including when the run ends with the
  sub-agent as the last producer, and a step close now carries the run id of
  the activation that owned it.
- A reasoning bracket never opens inside an open text message.
- The terminal event carries no usage field when no token count was reported.
- output.encryptedReasoning documents what is implemented: blobs go out, an
  inbound blob is ignored, continuity comes from the ADK session.
- web: Execute validates launcher.Config, cleartext HTTP/2 gets the -h2c flag
  (defaulting on, since the host mux serves gRPC on the same listener), and
  BuildBaseRouter is exported under upstream's name.

### Features

#### web: Catch up with ADK's web launcher and make h2c configurable ([565018f](https://github.com/alis-build/adk-launchers-go/commit/565018fb7bfaa52ec850313d81b98cdfa6fa23d1))

Execute now validates launcher.Config, as ADK's own web launcher has since
v2.3.0. Without it an unusable Compaction setting starts a server that fails
per request: agui and scheduler validate it while mounting host routes, but
evals builds its runtime lazily, so there it surfaced only on the first
request.

Cleartext HTTP/2 was hardcoded on with no way to opt out. It gets the -h2c
flag upstream added, defaulting on rather than off: the host mux serves gRPC
and gRPC-Web on the same listener and those need HTTP/2, so upstream's default
would break every deployment registering them. The reason is recorded at the
flag so it is not "fixed" back later.

buildRouter becomes BuildBaseRouter, matching upstream's name, so a
sublauncher written against ADK builds its routes here unchanged.

Adds web_test.go: the h2c flag driven end to end on a real listener in both
directions, and the config check covering both an invalid Compaction and a
valid one.

### Fixes

#### agui: Close sub-agent brackets and order step closes correctly ([3c987fc](https://github.com/alis-build/adk-launchers-go/commit/3c987fcade4bf958c447dd98e7b81f789c60e21e))

Two lifecycle defects in sub-agent attribution.

A run whose last producer is a sub-agent emitted SUBAGENT_STARTED with no
matching finish. closeSubagent was only reachable from a handover or the
interrupt path, so the ordinary shape of an ADK transfer, where the delegate
gives the final answer, left the bracket open across RUN_FINISHED and
RUN_ERROR and a client rendered that branch as running forever. FinalizeRun
now closes the activation after the rest of the lifecycle, and both executor
terminal paths use it.

STEP_FINISHED for the outgoing producer was stamped with the incoming
sub-agent's run id, because openSubagent ran before the step transition and
attribution is read at emit time. The step now closes before the handover, so
it carries the run id of the activation that owned it. finishWithInterrupts
had the mirror image of the same bug and is reordered to match.

#### agui: Omit the usage field when no token count was reported ([9a49f9e](https://github.com/alis-build/adk-launchers-go/commit/9a49f9e3ab744d76cfd86717d258805b7f4e66d2))

TokenUsage returned an entry as soon as UsageMetadata was present, but every
count is deliberately dropped when zero and there is no provider or model to
fall back on. An all-zero report therefore put "usage":[{}] on the terminal
event, asserting that usage was reported with nothing in it to read.

Return nil when no field survived, matching the "absent rather than asserted"
rule the rest of the function already follows.

#### agui: Keep the reasoning bracket out of an open text message ([32775bc](https://github.com/alis-build/adk-launchers-go/commit/32775bcaccbb6c10f0fed46c30d37a40f4b662c0))

A signature carried by the answer text opened REASONING_START between a
TEXT_MESSAGE_START and its end, nesting the whole reasoning block inside the
message. Two more call sites had the same hole: the thought branch's dedup
paths drain a blob without closing text first.

The guard goes in emitEncryptedReasoning, after its dedup checks, so a part
with no new blob never disturbs a message being streamed. Placing it before
those checks would close and reopen the text message on every partial that
repeats the same signature.

The text branch also needed reordering. Closing the message before the dedup
costs it the "is the streamed message still open" test it reads, which would
re-emit the whole accumulation instead of the tail. So a blob needing a fresh
bracket now waits until after the text is emitted, and the exact-repeat path
carries it too rather than dropping it silently.

### Documentation

#### agui: Correct what the encryptedReasoning capability claims ([cc0ee7a](https://github.com/alis-build/adk-launchers-go/commit/cc0ee7ab8cf9d3cbf823d1c695d2e53c2b717aa9))

The capability said the agent round-trips opaque reasoning blobs so a client
knows to send them back. Only the outbound half is implemented: nothing reads
types.Message.EncryptedValue back, and genai.Part.ThoughtSignature is never
assigned. A client following the advertised contract had its blob silently
dropped.

Wiring the inbound half is not a doc fix. The blobs sit on assistant messages
while the inbound path reads only the last user message, so honouring a
returned blob means rebuilding assistant history from client-supplied content
and pushing it into the ADK session. Continuity already comes from that
session, which keeps the original parts.

Say what is implemented instead: blobs go out so a client can persist and
re-render a thread, and an inbound blob is ignored. The wire field is
unchanged.

### Other

#### deps: Bump the ag-ui Go SDK pin ([d026de8](https://github.com/alis-build/adk-launchers-go/commit/d026de8c0372f2baef0160c7632ce6b171d658ba))

Moves to 0fa1bebd9772. The Go SDK tree is byte-identical to the previous pin:
the newer commit touched nothing under sdks/community/go, and pseudo-versions
track the whole repository.

## [1.2.0](https://github.com/alis-build/adk-launchers-go/compare/v1.1.0...v1.2.0) (2026-09-08)

### Features

#### adkrun: Wire ADK context compaction into launcher runners ([5131dd5](https://github.com/alis-build/adk-launchers-go/commit/5131dd5c665485a9e6887b895b8e3471a4a828ad))

ADK v2.3.0 added Compaction to its own launcher.Config, and every runner ADK
builds forwards it. The runners in this module dropped the field, so an
application that configured compaction got none in AG-UI runs, scheduler cron
ticks or eval inference.

Forward it from launcher.Config at both construction sites, and validate it in
NewRuntime so an unusable setting is reported once at construction rather than
on every request. Compaction stays off unless the application asks for it.

Compaction failures are logged and dropped from the event stream rather than
passed on. They arrive after the turn's events are persisted, so they cost a
smaller prompt later, not the user's answer; every consumer treats a streamed
error as terminal, so forwarding one reported a delivered answer as a RunError
in AG-UI and a failed cron tick in the scheduler. Errors that are not
compaction.ErrCompaction still reach the caller.

Tests pin that compaction reaches the runner from both sites, that a summary
never reaches the caller's iterator nor becomes a thread message, that a
compaction failure leaves the answer and the stream intact while a genuine
agent failure still propagates, and that a nil Compaction stays valid.

#### agui: Add AG-UI interrupt reason constants and call-name dispatch ([2e5e1a1](https://github.com/alis-build/adk-launchers-go/commit/2e5e1a1c28db9206bd2673061231d27dd6db45f4))

Introduce the three AG-UI core interrupt reasons and persist the
originating ADK FunctionCall name on interrupt.Record so resume can
dispatch on it.

Reason cannot be the dispatch key: hosts may classify an interrupt with
any custom reason string, which says nothing about which ADK call
produced it. CallName is empty on records written before this change,
all of which were tool confirmations, so it defaults accordingly.

#### agui: Convert ADK response schemas to AG-UI responseSchema maps ([2449d34](https://github.com/alis-build/adk-launchers-go/commit/2449d34625b1aecf7cf7bed8663afb2dbed56abf))

Add interrupt.SchemaToMap, the *jsonschema.Schema to map[string]any
round-trip that input_required interrupts need to advertise a
responseSchema to clients.

Conversion never fails the run. A schema that cannot marshal (Schema
only rejects mutually exclusive field pairs at marshal time) is logged
and omitted, costing the client its form hints rather than the user
their turn.

Handles the two degenerate shapes jsonschema-go collapses to JSON
booleans: an empty schema marshals as `true` and becomes the empty
accept-anything map, while a reject-all `false` is dropped, since
rendering it as an empty map would invert its meaning.

jsonschema-go moves from an indirect to a direct dependency; no version
change and no new modules.

#### agui: Classify workflow input requests into AG-UI interrupt reasons ([89ccd9f](https://github.com/alis-build/adk-launchers-go/commit/89ccd9f477b45ef7f1db6f0f908884eb9498dbcd))

Add the reason rule that decides whether a workflow input request
surfaces as "confirmation" or "input_required", plus the host override
the spec requires.

The default is shape-based: a response schema that is a bare boolean,
or an object wrapping exactly one boolean property, is a yes/no and maps
to "confirmation". Anything else needs a real input UI and stays
"input_required". The single-property limit is what separates the two:
a boolean alongside a reason field or an amount is a form.

WithInterruptReasonClassifier lets a host override that per request.
A non-empty return is used verbatim, since the AG-UI reason taxonomy is
open and clients must not error on unrecognised reasons, so hosts can
drive bespoke UI from their own namespaced reasons. Returning "" falls
through to the shape rule, which keeps a classifier that only handles
some requests from having to reimplement the default for the rest.

100% statement coverage on the new code; package coverage 71.8% -> 75.5%.

#### agui: Emit AG-UI interrupts for ADK workflow input requests ([fff6fb3](https://github.com/alis-build/adk-launchers-go/commit/fff6fb39066a81356ffa4bd98d73083f5ca41f6f))

Register adk_request_input in the interrupt dispatch table, closing the
TODO(non-tool-interrupts) the launcher has carried since ADK lacked a
native pause primitive.

The emitted interrupt takes its id and message from the request, carries
the response schema converted to AG-UI's map form, and leaves ToolCallID
empty: a non-tool reason has no bound tool call, and nothing is being
proposed here, the agent is asking the human directly. No tool lifecycle
is emitted for the same reason.

The reason comes from the classifier added earlier, so a single-boolean
schema surfaces as "confirmation" and everything else as
"input_required", with WithInterruptReasonClassifier overriding both.

Metadata carries invocationId for correlation, requestPayload so clients
can render the document or parameters being asked about, and callName.
callName is what resume dispatches on: the reason cannot serve that role
because a host classifier may return any custom string, which says
nothing about which ADK call has to be answered.

The request is read from the typed Event.RequestedInput when present and
from the mirrored FunctionCall args otherwise, since clients that do not
model RequestedInput round-trip only the args through session state. A
schema found in args is re-typed rather than passed through, so a host
classifier always sees a populated RequestInput whichever path the event
arrived by. A malformed schema is dropped with a log rather than failing
the run.

New code at 100% statement coverage.

#### agui: Resume workflow input requests, dispatched by ADK call name ([4a6a4c0](https://github.com/alis-build/adk-launchers-go/commit/4a6a4c05cecb4d55a340d73ec812d9cef42b5c09))

EntriesToConfirmationContent only ever built adk_request_confirmation
responses, so an input request had no way back into ADK. It becomes
EntriesToResumeContent, taking the pending records alongside the entries
and answering each interrupt with the call that produced it.

Dispatch keys on Record.CallName, not the reason. A host classifier may
return any custom reason string, which says nothing about which ADK call
is waiting, so keying on reason would resume into the wrong call. An
entry with no matching record, or a record predating CallName, falls back
to a tool confirmation, which is what those records always were.

An input request answers {"response": payload}, the key ADK's
decodeWorkflowInputResponse reads first. The payload passes through in
whatever JSON shape it arrived, since a node may advertise a scalar
schema and there is no required "approved" field to demand.

A cancelled input request sends {"response": null} rather than erroring,
mirroring how a cancelled tool confirmation sends confirmed:false.
Interpreting a withheld answer is the node's business. Resolving with no
payload at all stays an error: that is a client bug, and withholding is
what cancelled already expresses.

Both new functions at 100% statement coverage.

#### agui: Advertise supported interrupt reasons and document non-tool HITL ([4890c98](https://github.com/alis-build/adk-launchers-go/commit/4890c98dda8c8f45bc94035b7f40df6452ecf64f))

Add humanInTheLoop.interruptReasons to the capabilities document, filled
by MergeInterruptCapabilities with the reasons this launcher emits:
tool_call, input_required and confirmation. A host that already listed
its own reasons keeps them, since a custom classifier can return reasons
the launcher knows nothing about.

Rewrite the doc.go claims that this was deferred. Both the interrupts
section and Limitations said input_required and confirmation "are not
implemented" and "depend on a future ADK pause/HITL API", pointing at a
TODO that no longer exists.

The replacement documents what hosts actually need to know: how the
reason is chosen from the response schema, that these interrupts carry no
toolCallId, that the resume payload takes the schema's shape and needs no
"approved" field, and that a cancelled status resumes with a nil response.

It also records two constraints that would otherwise surprise people.
Only workflow agents can produce these interrupts, because resumeInputs
is populated solely by the workflow scheduler, so a plain llmagent never
emits adk_request_input however it is prompted. And clients must answer
every interrupt in outcome.interrupts rather than just the first, since
an event can now deliver several and validation rejects a resume that
leaves any unaddressed.

#### agui: Read workflow graph provenance off ADK events ([a7bd265](https://github.com/alis-build/adk-launchers-go/commit/a7bd26599393757fac96c542d71ebb8f5cd854d3))

Every workflow event already carries which node emitted it, whose output
it counts as, and where the scheduler goes next. The launcher discarded
all of it, so a five-node workflow reached a client as one undifferentiated
stream.

NodeProvenanceFrom reads NodeInfo.Path, MessageAsOutput, OutputFor and
Event.Routes in one place, so later work does not reach into session.Event
from every call site. Workflow membership follows ADK's invariant that
readers test the NodeInfo pointer and not its contents, which is why an
event's Routes alone are not attribution.

StepName falls back to the event author, because top-level static nodes
carry an empty path and the agent name is the only thing identifying them.
That fallback is reachable in process: ADK collapses an all-zero NodeInfo
to nil only when decoding JSON from another runtime.

OutputPaths returns OutputFor when present so one event's output is
recorded for a whole delegation chain rather than only its emitter, and
addresses nothing when there is neither a path nor OutputFor, so an output
is dropped rather than filed under an empty key.

NodeOutputValue prefers an explicit Event.Output and otherwise derives the
output from model text under MessageAsOutput, which is how a node that
answers in prose reports its result. Thought parts are excluded: they are
how the node got there, not what it produced.

Deviation from the plan's file map, recorded in the plan: extraction lives
in internal/stream, not convert.go. convert.go is in package agui, which
imports internal/stream, so the processor could not import back without a
cycle.

All new functions at 100% statement coverage.

#### agui: Bracket workflow node activations in step events ([8beae26](https://github.com/alis-build/adk-launchers-go/commit/8beae26bfb20f3c5ff0f8102d674dd08e13c3c48))

A workflow event is now bracketed by its graph node rather than by its
agent, so an AG-UI client can tell which step of a five-node workflow is
running instead of seeing one undifferentiated stream.

The step is named by NodeInfo.Path, falling back to the event author for
top-level static nodes, which carry no path. Node steps skip the
root-agent normalisation: a node is a real graph activation even when its
agent shares the root's name, and suppressing it would drop a node from
the client's view of the graph.

Non-workflow events keep today's behaviour exactly. The root agent still
gets no step, consecutive root partials still do not trip the change
check, and the plain-agent wire baseline passes unchanged.

State.CurrentStepAuthor becomes CurrentStepName, since it now holds a
node path as often as an agent name.

Steps still close through finalizeLifecycle, so an interrupt cannot leave
one open past the terminal event.

#### agui: Carry node path and routes on interrupts raised inside a node ([9c3a649](https://github.com/alis-build/adk-launchers-go/commit/9c3a649ee8034c516668ac58e7dbd893b89eb923))

An interrupt from a workflow node now records which node paused and where
the scheduler would have gone next, so a client can place a pause in the
graph rather than just showing it.

Applied at the collection site rather than inside each handler, so every
interrupt reason gets attribution without each handler remembering to add
it, including reasons added later.

Interrupt metadata is the only per-event channel the pinned Go SDK
offers: types.Interrupt.Metadata exists while event-level metadata does
not. That asymmetry is why interrupts carry provenance in metadata while
every other event in this track carries it in its step name.

Empty values are omitted rather than written as blanks, so a client can
tell "no routes" from "routes unknown". A non-workflow interrupt gains
nothing, keeping the tool_call wire that aguihitl pinned byte-identical.

#### agui: Surface workflow node outputs under a reserved _adk state key ([5ee2850](https://github.com/alis-build/adk-launchers-go/commit/5ee285057b2e55f64d67ecbd77a182a32f9b1091))

Node results now reach the client as they happen, keyed by node path, so
a client can show what each step produced and not just which step ran.

Keys follow the same identity as step names, falling back to the agent
name for pathless nodes, so one node appears under one name in both the
step stream and the state. OutputFor fans a single output across a whole
delegation chain, which is that field's stated purpose. MessageAsOutput
with no explicit Output records the model text. An output with nothing to
key it on is dropped rather than filed under an empty key.

The delta replaces the whole _adk object rather than patching a path
inside it. A JSON Patch "add" needs its parent to exist and the client has
no _adk until the first node reports, so a per-path patch would fail on
the very first output. Node results are few and small, so resending the
map beats tracking whether the client has the parent yet.

_adk is reserved in isInternalStateKey. Node outputs travel on the same
state channel as host application state, so without the reservation they
would be read back as host state and written into the ADK session on the
next turn, making launcher bookkeeping part of the agent's world. The
match is exact or separator-terminated, so a host key like "_adkish"
stays visible.

The two directions are deliberately asymmetric: an _adk value arriving
from session state or a client request is untrusted and stripped, while
the launcher's own accumulated map is added to interrupt snapshots. A
test proves a client-supplied _adk cannot survive the round trip.

#### agui: Add WithoutGraphAttribution to turn graph attribution off ([c72a3b6](https://github.com/alis-build/adk-launchers-go/commit/c72a3b620ddc5a4155ad2853fa0606ac56164993))

Graph attribution is on by default, but it adds STEP_* events some
clients do not expect. WithoutGraphAttribution suppresses all three
surfaces at once: per-node step events, node outputs under the reserved
_adk state key, and nodePath/routes on interrupt metadata.

Every attribution site now reads provenance through one Processor method,
so a single flag turns them all off together and no site can drift out of
step with the others.

The opt-out is scoped to attribution, not to the agent. A workflow node's
text still streams, and the sub-agent step behaviour that predates this
track keeps working, so disabling attribution returns the stream to what
the launcher emitted before rather than silencing part of the run.

Regression gate: the plain-agent wire baseline passes byte-identical, and
git confirms the golden has not been edited since it was captured before
any of this work landed.

#### agui: Send repeated activity updates as deltas instead of snapshots ([b36a783](https://github.com/alis-build/adk-launchers-go/commit/b36a783daae2db6867d9a56db52b2519012c2940))

An activity surface updated repeatedly re-sent its whole content every
time, so a client re-rendered the block and re-received fields that had
not changed. Consecutive updates for the same (messageID, activityType)
pair now carry a JSON Patch against what the client already holds.

Activity events reach the stream only through a host part converter; the
launcher has no activity source of its own. The converter relay is
therefore the one place a repeat can be recognised, and is where this
hooks in. Events that are not activity snapshots pass through untouched.

The first update for a surface is always a snapshot, since a patch needs
something to patch against. An update whose content is not a JSON object
has no key paths to address and falls back to a snapshot, which also
re-arms the tracking so the update after it can delta again.

Patches use add, replace and remove only. Nothing here needs move or
copy, and both invite index bugs for a saving that does not matter at
these payload sizes. Nested objects and same-length arrays recurse so
only changed leaves travel; a resized array is replaced whole, because
diffing across a length change needs operations at shifting indices and
the replacement is never larger than the array.

A repeat that changes nothing emits nothing: the SDK rejects a delta with
an empty patch, which is the protocol declining to carry no-ops, and a
snapshot would say nothing either. Tracking is keyed on message id and
activity type together, since one surface can carry several types and one
type can appear on several surfaces, and is cleared at run finalization.

#### agui: Carry encrypted reasoning through the stream and history ([2c28ec4](https://github.com/alis-build/adk-launchers-go/commit/2c28ec437d70ffe9053bbeafe79f292bb820b9ea))

Models that return an opaque reasoning blob had it dropped, so reasoning
continuity was lost the moment a client reloaded a thread and sent the
history back.

ADK surfaces the blob as genai.Part.ThoughtSignature, raw bytes; AG-UI
carries a string, so it is base64-encoded. It stays opaque either way:
the launcher neither reads nor validates it.

On the stream it emits after the reasoning content, so it sits inside the
REASONING_START / REASONING_END bracket the protocol expects. ADK
partials carry accumulated state, so the same blob arrives on several
events for one message and is sent only when it changes.

In history it rides the assistant message rather than the reasoning
message, because the assistant turn is what a client sends back, which is
the whole point of preserving it.

Additive throughout: a model returning no signature emits no event and
adds no field, and both wire baselines pass unchanged.

#### agui: Attach namespaced metadata to every AG-UI event ([bb88b0e](https://github.com/alis-build/adk-launchers-go/commit/bb88b0e6783e18f727c73c660c8b72ad54ebaf8d))

Events carried no provenance, so a client could not tell which ADK
invocation, author or workflow node produced one without inferring it
from position in the stream.

Every event now carries metadata.adk.invocationId and metadata.adk.author,
plus metadata.adk.nodePath on workflow events, read from the field
aguigraph already plumbs rather than re-derived. Token usage rides
metadata["ag-ui"].tokenUsage, since only the ag-ui key is protocol space
and everything ADK-specific belongs under adk.

Stamped by wrapping the sink for the duration of one ADK event rather
than passing metadata to each emit site. "Every event in a run" is a
claim about a couple of dozen call sites, and the first one forgotten
would be a silent gap. Metadata already on an event wins: a host part
converter that set its own knows something the launcher does not.

An event with no invocation id, author or node gets no metadata at all,
so the field stays absent rather than serializing as an empty object.

Three wire goldens are updated, since attaching metadata to every event
cannot be byte-identical by construction. Before regenerating each, the
rest of the stream was proven unchanged by stripping the metadata key and
re-running the golden green against its previous contents. Each records
that in a comment. This is the one edit those goldens have had.

#### agui: Advertise the new protocol features and document them ([2a86c32](https://github.com/alis-build/adk-launchers-go/commit/2a86c324d34d4ace15821ab6f80c8ac8e6446910))

Add output.activityDeltas and output.encryptedReasoning to the
capabilities document, so a client knows to apply patches rather than
replace an activity block, and to send reasoning blobs back.

An explicit false is preserved on merge: turning a feature back on would
tell clients to expect events the host has deliberately suppressed.

doc.go gains a Protocol details section covering the delta and snapshot
rule, why an unchanged repeat emits nothing, that activity reaches the
stream only through a part converter, the encrypted reasoning round trip
and why the blob rides the assistant message, and the metadata
namespacing rule that only "ag-ui" is protocol space.

#### agui: Attribute events to sub-agent runs ([0361672](https://github.com/alis-build/adk-launchers-go/commit/0361672eac3c338e9786e0aedd60ad5a4918128a))

A multi-agent run arrived as one undifferentiated stream, so a client
rendering three concurrent researchers had no way to tell them apart.
Each sub-agent activation is now bracketed by SUBAGENT_STARTED and
SUBAGENT_FINISHED, and every event it produces carries its subagentRunId.

An activation is identified by author together with ADK's Branch, not
author alone. Branch is what keeps peer sub-agents from seeing each
other's history, so two activations of the same agent on different
branches are concurrent runs rather than one continuing. The root agent
is the run itself, not a sub-agent within it, so its events carry no
attribution.

A sub-agent still open when the run pauses finishes as suspended rather
than successful, naming the interrupts it owns, so a client can show
which branch is waiting on a human.

Stamping uses reflection over a cached field index rather than a type
switch. SubagentRunID is declared on each of the two dozen carrying event
types rather than on BaseEvent, so there is no interface to set it
through; a type switch would have to name every type, and the first one
missed, or added upstream later, would be a silent gap.

The outgoing producer's message closes before the handover. Without that
the root agent's own TEXT_MESSAGE_END was emitted after the incoming
SUBAGENT_STARTED and stamped with a run id it never belonged to, which
the plain-agent golden caught.

Subagent brackets and STEP_* both ship. They mark the same boundary, but
steps are a flat sequence in AG-UI and subagents are an identity concept,
and replacing steps would break existing clients. They close adjacently
rather than nesting.

#### agui: Add WithoutSubagentAttribution to turn sub-agent identity off ([7fe3b98](https://github.com/alis-build/adk-launchers-go/commit/7fe3b988109319978da9a1d11837fa7cdc366d67))

Subagent brackets shipped on by default, which changes the wire for every
existing multi-agent stream, and unlike graph attribution there was no way
off. That was the wrong side of the precedent this package already set.

The exposure is sharper than an unfamiliar event usually is. The AG-UI Go
SDK's EventDecoder.DecodeEvent returns an error for an event type it does
not recognise rather than skipping it, so a consumer pinned to an SDK
older than the subagent release fails to decode the stream rather than
degrading. The option lets them opt out without pinning the launcher back.

One gate covers both halves: with the sink wrap skipped and the
activation never opened, nothing carries a run id and no bracket is
emitted, so the two cannot drift apart as either grows.

Scoped to identity, not output. The sub-agent's own text still streams
and the STEP_* bracketing that predates this still works, so disabling
attribution returns a multi-agent stream to what the launcher emitted
before rather than hiding part of the run.

#### agui: Move token usage onto the protocol's usage field ([e7fc6a0](https://github.com/alis-build/adk-launchers-go/commit/e7fc6a0604e6e9fc6f2d8c97c92e57b1d9817818))

ag-ui PR 2685 merged, so the Go SDK now has TokenUsage and a usage field
on RUN_FINISHED and RUN_ERROR. Usage moves off the metadata.adk shim onto
its proper home, and the shim is gone.

The move surfaced something the shim had hidden. Usage is per run, not
per event: the shim attached counts to every event that carried them,
while the protocol carries one figure on the terminal event. The launcher
now accumulates across the run and emits once, on RUN_FINISHED or on
RUN_ERROR for a run that died after some model calls completed.

Only completed responses are counted. Streaming partials are skipped,
since ADK reports usage on the completed response — the openai model path
sets it on the final. If counts ever look inflated that summing rule is
the thing to revisit, because a provider reporting a running total on
every event would need "take the last" instead.

Counts are *int64 upstream so zero is expressible, but a count ADK
reported as zero is still left absent: ADK types counts as int32 with
omitempty and cannot tell "produced none" from "did not report", so
asserting a measured zero would be the stronger claim. A negative count
is dropped rather than emitted, because the SDK's Validate rejects one
and would take the whole terminal event down with it.

One entry, no provider or model, because ADK surfaces neither at this
layer. A run spanning several models arrives summed rather than split,
which is a limit of what ADK tells us rather than a choice.

### Fixes

#### agui: Emit every interrupt in an event, not just the first ([75fe333](https://github.com/alis-build/adk-launchers-go/commit/75fe333e9f938cc325f313dec198d72247c93a18))

ProcessEvent returned as soon as it saw an interrupt-producing call, so
an agent proposing three tools in one turn had two of them silently
dropped. The client never learned those interrupt ids, could not resume
them, and resume validation would then reject the run for missing ids it
was never sent.

Interrupts are now collected across all parts in part order and emitted
in a single RunFinished, which is what the AG-UI protocol requires: one
terminal event per run, with an outcome that already accepts a slice.

Emission splits in two to make that possible. Handlers build one
interrupt and emit its own tool lifecycle; finishWithInterrupts emits the
terminal event once. Snapshots move into the latter, since they describe
the session rather than any individual pause and should not repeat per
interrupt.

Ordering is preserved for the single-interrupt case: open text and
reasoning are closed before the first proposal's tool lifecycle, exactly
as before, so the wire baseline passes unchanged.

#### agui: Stop demanding payload.approved when resuming input requests ([64a1062](https://github.com/alis-build/adk-launchers-go/commit/64a1062c05451d3aec4ec619dca86032decbb815))

Resume validation forced every resolved payload through ResumePayloadMap,
which requires a JSON object. A node that asked for a string got its
answer rejected with "payload must be a JSON object" before ADK ever saw
it.

Validation now splits on what is actually waiting. A tool confirmation
still must send an object, because the launcher reads payload.approved
out of it to build the ADK response. An input request is answered in
whatever shape the node's schema described, so a scalar is valid and is
checked against that schema's type.

Nothing special-cases "approved" in either path. The requirement travels
on the tool confirmation's own persisted responseSchema, which is why a
host classifier returning a custom reason on an input request never
inherits it.

The shared rules are untouched: every open interrupt must still be
answered, unknown ids rejected, expiry honoured, and a resolved resume
still requires a payload.

validateResolvedPayload at 100% statement coverage; the interrupt package
rises to 84.5%, up from 71.8% at the start of this track.

#### agui: Keep input requests distinct when one event carries several ([51ea3c1](https://github.com/alis-build/adk-launchers-go/commit/51ea3c1f7e747f03bbb767830d740f51ba46c360))

Event.RequestedInput is a single pointer, but collecting every interrupt
in an event made it possible for one event to carry several
adk_request_input calls. inputRequestFrom returned the typed request for
all of them, so every interrupt got the same id and only the first
request's message survived.

The damage ran past the stream. RecordsFromInterrupts persisted records
sharing an id, and resume validation keys pending records by id, so the
duplicates collapsed into one entry: a client answering the first request
passed validation while the node waiting on the second was never answered
and the run stalled.

The typed field is now authoritative only for the call it names; other
calls fall back to their own mirrored args, which already carry the right
id and message.

Found in review of this track, at the seam between preferring the typed
request and collecting all interrupts. The two were built in separate
phases and no test crossed them.

Also from the same review:
- Pin the behaviour change where a plain tool call sharing an event with
  an interrupt is now emitted rather than dropped, and gets no result
  because the run is ending. Pinned rather than changed: suppressing a
  proposal ADK may still execute is a protocol question this track did
  not settle.
- Document that WithInterruptReasonClassifier is ignored under
  WithExecutor, the trap WithGenAIPartConverter already documents.

#### agui: Close the reserved-key round trip and stop emitted deltas mutating ([7a79f42](https://github.com/alis-build/adk-launchers-go/commit/7a79f421c4fa04967e3de375c0bdfdf41fda4a1b))

Two findings from reviewing this track.

Strip launcher-owned keys from client state before it becomes an ADK
session. Acceptance criterion 7 required _adk to be internal AND absent
from state written back to the session; only the first half was done.
Snapshots publish _adk to clients, so a client echoing it back is the
normal case, and on a thread's first request that state became the
session's initial state: permanent and visible to the agent. The existing
test only covered the snapshot direction, which made the round trip look
closed when it was not. The filter also catches the pre-existing _agui_
keys, which had the same hole but were never sent to clients so nothing
echoed them.

Copy the accumulated node outputs into each emitted event. The
accumulator keeps growing as later nodes report, and emitted events do
not stop at the wire: the launcher hands them to hosts through
AfterEventCallback. Sharing the live map let an event a host was holding
change after the fact, showing a history it never emitted. Verified a
held delta grew from one entry to two when the next node reported. The
wire was always correct, since the writer serialises on emit.

withoutInternalKeys returns its input untouched when nothing is internal,
and never mutates the caller's map.

#### agui: Close the review findings on the protocol catch-up ([0c7ff0e](https://github.com/alis-build/adk-launchers-go/commit/0c7ff0ee955be40e67f56297bfdc48852e2b7695))

Eight defects the track review turned up, none of which the existing tests
could see. Each fix ships with a test proven to fail without it.

Encrypted reasoning only reached the client when the signature rode a thought
part. ADK re-attaches it to the function call that ends a reasoning turn, so
the blob was lost on exactly the tool-calling turns it exists to make
resumable. It is now read from whichever part carries it, and a part that
opened no reasoning message opens one so it is never emitted outside the
bracket. In history it lands on the first message able to carry it, tool calls
first, since that is the message a client sends back.

WithCapabilities never merged the protocol capabilities, and it is the only
writer of the capabilities field, so activityDeltas and encryptedReasoning were
undiscoverable however a host configured the launcher.

The per-turn state delta skipped the strip that guards session creation, so a
client echoing back the _adk it was shown could write launcher-owned keys into
the ADK session every turn, and a planted _agui_pending_interrupts could
overwrite the record resume validation checks against.

Event metadata resolved the node path itself instead of going through
Processor.nodeProvenance, leaving it the one attribution site
WithoutGraphAttribution could not switch off. It also handed every event the
same metadata map, since MergeMetadata returns the existing map untouched when
the event carries none; each event now gets its own copy.

Activity snapshots were recorded by reference, so a converter reusing one
content map diffed it against itself, emitted nothing, and froze the client's
activity block at its first value.

A resume run re-emitted a baseline StateSnapshot built without _adk, erasing
every pre-interrupt node result; those outputs live on the per-run state and
cannot be rebuilt, and the interrupt snapshot already published them.

doc.go had a sentence spliced into the middle of another, and three statements
this change makes untrue.

#### agui: Keep launcher metadata out of the reserved ag-ui key ([4c7853d](https://github.com/alis-build/adk-launchers-go/commit/4c7853d6b0e301c3af09f3301b0a4909b6fa7079))

Token usage shipped under metadata["ag-ui"].tokenUsage. The SDK reserves
that key for AG-UI's own use and states every other key is user space, so
the launcher was writing its own data into protocol space.

Spec FR3 asked for that placement, justified as matching the first-party
SDKs' token-usage shape. That shape does not exist: the Go SDK defines no
tokenUsage anywhere, and the occurrences in its tests are merge fixtures
that use the name both at the top level and under ag-ui. With no shape to
match, claiming the reserved key buys nothing and collides the day AG-UI
defines a field of its own.

Token usage moves to metadata.adk.tokenUsage, alongside invocationId,
author and nodePath, so everything the launcher writes sits under one key
it owns. A test now asserts nothing is ever written under
types.AGUIMetadataKey, so this cannot quietly come back.

Raised by a peer session working on the upstream SDK; verified against
the SDK's own documentation rather than adopted on its word.

#### agui: Emit token usage in the canonical TokenUsage shape ([075fda2](https://github.com/alis-build/adk-launchers-go/commit/075fda2f7725e428737e9474b11bf19996ab70e7))

The spec asked for a shape "matching the first-party SDKs' token-usage
shape". I could not find one and concluded it did not exist, because I
searched only the Go SDK. It exists in TypeScript, Python and .NET: a
TokenUsage type carried as a `usage` array on the terminal event, one
entry per provider and model.

That also means the spec's original location was wrong in a second way.
Canonical usage is not in metadata at all, so metadata["ag-ui"].tokenUsage
was never its home either, and moving off the reserved key stands.

The Go SDK has neither the type nor the field, so usage stays under
metadata.adk. It now uses the canonical shape though: an array container
and the names inputTokens, outputTokens, totalTokens, reasoningTokens and
cachedInputTokens, so adopting `usage` later is a move rather than a
rewrite. The two new counts come from ADK's ThoughtsTokenCount and
CachedContentTokenCount, which were being dropped.

The names matter more than they look. TypeScript's TokenUsageSchema
strips keys it does not recognise, so the previous promptTokens and
completionTokens would have vanished silently on parse there while
Python's extra="allow" kept them.

Zero counts are omitted rather than reported as zero, so "not measured"
stays distinguishable from "measured none". provider and model are absent
because ADK surfaces neither on the event.

### Refactoring

#### agui: Dispatch interrupt-producing function calls by call name ([cb82c3a](https://github.com/alis-build/adk-launchers-go/commit/cb82c3a5d7b8012d657cb544f2d0af1470c961d2))

Replace the hardcoded adk_request_confirmation check in ProcessEvent
with a lookup in interruptCallHandlers, a table keyed by ADK's synthetic
FunctionCall names.

Membership in the table doubles as the "is this call interrupt-producing?"
predicate, so ProcessEvent can recognise an interrupt without knowing how
to emit one. That seam is what the next two changes need: adk_request_input
registers as a second entry, and multi-interrupt collection has to scan
all parts asking only whether each is an interrupt.

Two readability fixes carried in the same change:
- The emitted reason is now interrupt.ReasonToolCall rather than a
  "tool_call" string literal.
- The local variable holding the interrupt is renamed from interrupt to
  intr. It shadowed the interrupt package, which the same composite
  literal calls into; that worked only because a short variable
  declaration's scope starts after the statement.

No behaviour change. The wire baseline added in 62ddae1 compares the full
SSE stream field by field and passes unchanged.

### Documentation

#### agui: Document workflow graph attribution in doc.go ([e270854](https://github.com/alis-build/adk-launchers-go/commit/e270854320d8498beb876d9bf4d43ffcce52878f))

Add a Workflow graphs section covering the four ADK fields read, the step
naming rule and its author fallback, why a workflow node is bracketed
where the root agent is not, the _adk.nodeOutputs key shape, and the
opt-out.

Records the two things a host would otherwise have to discover by
reading the launcher: that workflow membership is decided by the NodeInfo
pointer alone, so a nil NodeInfo produces exactly the pre-attribution
stream, and that inbound _adk is stripped rather than echoed so only the
launcher writes it.

Limitations notes that a live per-node graph view is not implemented,
which is the explicitly undecided scope recorded in the backlog.

### Other

#### deps: Update dependency versions in go.mod and go.sum ([59a7a56](https://github.com/alis-build/adk-launchers-go/commit/59a7a56ab34f2128025c2394c8afb8710661ca0f))

Updates all direct and indirect dependencies, notably ADK v2.2.0 to v2.3.0,
genai v1.67.0 to v1.71.0 and iam/v3 v3.8.0 to v3.9.1. Source-compatible: no
dependency removed or changed a signature this module calls.

Also tidies go.sum, which the update left holding stale hashes for the
superseded versions.

#### Update .gitignore to include .gotmp and conductor/ directories ([340a458](https://github.com/alis-build/adk-launchers-go/commit/340a458e36df2d1fd0fb96a4e3076209fb46e2fb))

## [1.1.0](https://github.com/alis-build/adk-launchers-go/compare/v1.0.17...v1.1.0) (2026-08-10)

### Features

#### evals: Implement session state merging in RunEvalRequest ([ba090c1](https://github.com/alis-build/adk-launchers-go/commit/ba090c1d9f51d3e863343857d9b5487be01436e4))

- Added `session_state` field to `RunEvalRequest` to support run-level session state.
- Implemented merging logic for session state, allowing case-level keys to override run-level keys.
- Updated `runInference` method to utilize merged session state during inference.
- Enhanced documentation to clarify the behavior of session state merging.
- Introduced tests to validate the merging functionality and ensure correct behavior across various scenarios.

### Other

#### deps: Update dependency versions in go.mod and go.sum ([adf2616](https://github.com/alis-build/adk-launchers-go/commit/adf26160903ca87fc974cb6265abab90bfe77bb6))

- Bumped the version of `github.com/ag-ui-protocol/ag-ui/sdks/community/go` to v0.0.0-20260808171141-68b99d8bb891.
- Updated `google.golang.org/genai` to v1.67.0, ensuring compatibility with the latest features and fixes.
- Adjusted the go.sum file to reflect the changes in dependency versions.

## [1.0.17](https://github.com/alis-build/adk-launchers-go/compare/v1.0.16...v1.0.17) (2026-08-05)

### Fixes

#### agui: Enhance text streaming processing ([0bfd4c1](https://github.com/alis-build/adk-launchers-go/commit/0bfd4c14e3f22abcea6f1cc69b4ef64d1ae13096))

- Bumped versions of several dependencies in go.mod and go.sum, including updates to `github.com/ag-ui-protocol/ag-ui/sdks/community/go`, `google.golang.org/api`, and `github.com/googleapis/enterprise-certificate-proxy`.
- Enhanced the text streaming event processing logic to improve handling of partial and final events, ensuring correct deduplication and emission of text messages.
- Introduced new tests for various text streaming scenarios to validate the updated processing logic and ensure robustness in event handling.

## [1.0.16](https://github.com/alis-build/adk-launchers-go/compare/v1.0.15...v1.0.16) (2026-08-03)

### Features

#### evals: Enhance InvocationEvents with toolCalls and toolResponses methods ([b9dffde](https://github.com/alis-build/adk-launchers-go/commit/b9dffde1b5f11b244d9e42e7f5125f37b16a24e5))

- Added `toolCalls` and `toolResponses` methods to the `InvocationEvents` struct to streamline the extraction of function calls and responses from invocation events.
- Updated the `MarshalJSON` method to include these new methods, ensuring that serialized output contains the derived tool uses and responses.
- Refactored existing logic in `GetAllToolCalls` and `GetAllToolResponses` to utilize the new methods, improving code clarity and reducing duplication.
- Introduced comprehensive tests to validate the new functionality, ensuring correct behavior for various scenarios involving tool calls and responses.

## [1.0.15](https://github.com/alis-build/adk-launchers-go/compare/v1.0.14...v1.0.15) (2026-07-31)

### Fixes

#### evals: Normalize intermediate data handling in invocation models ([b52d7b4](https://github.com/alis-build/adk-launchers-go/commit/b52d7b4850c8b79bf8f42d43f0c0e27447536edc))

- Updated the `Invocation` model to ensure `IntermediateData` is always present, preventing null values during serialization.
- Introduced a new structure, `intermediateDataJSON`, to standardize the output of intermediate data, ensuring it always includes `toolUses`, `toolResponses`, and `intermediateResponses` as arrays.
- Enhanced the `MarshalJSON` method for `jsonIntermediate` to normalize null values, ensuring consistent behavior across different scenarios.
- Added tests to validate the new behavior, confirming that intermediate data is correctly serialized and deserialized without losing structure.

## [1.0.14](https://github.com/alis-build/adk-launchers-go/compare/v1.0.13...v1.0.14) (2026-07-31)

### Features

#### evals: Implement session ID handling in inference generation ([affc9b0](https://github.com/alis-build/adk-launchers-go/commit/affc9b024bf1a4e26dcbecfc712cb66a8a6a5301))

- Added support for specifying a session ID in InferenceOptions, allowing users to provide a fixed session ID or allocate a new one if not set.
- Enhanced the evalSessionID function to return the provided session ID or generate a new one as needed.
- Updated GenerateInferences method to utilize the session ID from InferenceOptions, ensuring consistent session management during inference runs.
- Introduced new tests to validate session ID behavior in various scenarios, confirming correct allocation and retrieval of session events.

## [1.0.13](https://github.com/alis-build/adk-launchers-go/compare/v1.0.12...v1.0.13) (2026-07-31)

### Features

#### evals: Enhance legacy evaluation handlers to return JSON arrays ([a09ea7e](https://github.com/alis-build/adk-launchers-go/commit/a09ea7e4d3b53992d9a26de3336bfcf67b78dcde))

- Added tests to ensure legacy evaluation handlers return empty JSON arrays when no results are available.
- Implemented a utility function, StringSliceForJSON, to standardize the output of string slices, ensuring non-nil empty slices are returned for JSON encoding.
- Updated existing evaluation storage methods to utilize the new utility function, improving consistency in JSON responses across different storage backends.
- Introduced tests for the new utility function to validate its behavior with both nil and non-empty slices.

## [1.0.12](https://github.com/alis-build/adk-launchers-go/compare/v1.0.11...v1.0.12) (2026-07-31)

### Other

#### agui: Enhance thread deletion logic ([e0aaa25](https://github.com/alis-build/adk-launchers-go/commit/e0aaa256a805018860f99fb0d1585028b3ed0e8a))

- Bump dependencies in go.mod and go.sum, including updates to google.golang.org/api, google.golang.org/genai, and google.golang.org/grpc.
- Modify the delete thread functionality to purge the associated ADK session, ensuring proper cleanup of resources.
- Update documentation to clarify the implications of thread deletion on session management.

## [1.0.11](https://github.com/alis-build/adk-launchers-go/compare/v1.0.10...v1.0.11) (2026-07-28)

### Other

#### deps: Bump agui/history and migrate AG-UI proto imports ([c98d551](https://github.com/alis-build/adk-launchers-go/commit/c98d551c142d8b65388effd6943e4a7f0b5ea111))

Adopt agui/history v0.1.5 (UpdateThread, WithTitleGenerator) and
agui/scheduler v0.1.2. Replace go.alis.build/common with split proto
modules (common/alis/agui/history v1.4.0, common/alis/agui/scheduler
v1.0.0) and update import paths in agui/threads, threadmeta, and
scheduler. Also bump google.golang.org/adk/v2 to v2.1.0 and Go to 1.26.5.

## [1.0.10](https://github.com/alis-build/adk-launchers-go/compare/v1.0.9...v1.0.10) (2026-07-23)

### Fixes

#### agui: Preserve stream dedup after TurnComplete closes messages ([e7ff735](https://github.com/alis-build/adk-launchers-go/commit/e7ff735f50c1d5aab96c33d7ff0a5a19f7f3bc84))

Remote A2A sub-agents can deliver a single non-partial text event with
no preceding partials; the prior fix emits that remainder correctly.
Local agents still stream partial deltas followed by a trailing final.

Resetting EmittedTextLen and EmittedReasoningLen in closeTextMessage
and closeReasoningMessage broke dedup when ADK sent that trailing final
after TurnComplete had already closed the open message. That could emit
duplicate TEXT_MESSAGE_* or REASONING_* lifecycle events on the SSE
stream even though the UI already had the full content.

Reset emitted-length counters when opening a new message instead of on
close, so trailing finals stay suppressed while non-partial-only remote
replies still render. Add turn-complete regression tests for text and
reasoning streaming.

Also bump module dependencies and ignore local Go build cache dirs.

## [1.0.9](https://github.com/alis-build/adk-launchers-go/compare/v1.0.8...v1.0.9) (2026-07-16)

### Fixes

#### agui: Enhance text streaming event processing and deduplication ([1bcc852](https://github.com/alis-build/adk-launchers-go/commit/1bcc85224e2f74f3ef06da399b5573414d300811))

- Introduced new tests for handling partial and final text events in the `TestProcessEvent_TextStreaming` and `TestProcessEvent_ReasoningPhase` functions, ensuring correct event emission and deduplication logic.
- Updated the processor to manage emitted text lengths, allowing for proper handling of final events from remote sub-agents.
- Revised event emission logic to ensure that only new portions of text are sent, improving the clarity and efficiency of the streaming process.
- Added tests to validate the new behavior, ensuring robustness in the handling of both reasoning and text streaming events.

## [1.0.8](https://github.com/alis-build/adk-launchers-go/compare/v1.0.7...v1.0.8) (2026-07-13)

### Features

#### evals: Refine rationale handling in rubric aggregation ([422f353](https://github.com/alis-build/adk-launchers-go/commit/422f35305409727d41d51fd8858486c6cbfc5b1e))

- Enhanced the `aggregatePerInvocationRubrics` function to concatenate multiple source rationales with invocation prefixes, improving clarity in evaluation outputs.
- Updated the handling of empty and missing rationales to ensure accurate representation in aggregated scores.
- Revised documentation to reflect the new rationale semantics and their implications for downstream consumers.
- Added tests to validate the updated rationale handling logic, ensuring robustness in evaluation results.

## [1.0.7](https://github.com/alis-build/adk-launchers-go/compare/v1.0.6...v1.0.7) (2026-07-13)

### Features

#### evals: Enhance rubric rationale handling in aggregation functions ([2303460](https://github.com/alis-build/adk-launchers-go/commit/230346039b35dd64be88b1330ea25db42671615b))

- Updated `aggregatePerInvocationRubrics` and `aggregateOverallRubrics` to preserve source rationales for single contributions, improving clarity in evaluation results.
- Introduced a new constant, `aggregatedRationaleBoilerplate`, to standardize the rationale for aggregated scores across multiple contributions.
- Added comprehensive tests to validate the new rationale handling logic, ensuring accurate representation of model reasoning in evaluation outputs.
- Updated documentation to reflect changes in rationale semantics and their implications for downstream consumers.

## [1.0.6](https://github.com/alis-build/adk-launchers-go/compare/v1.0.5...v1.0.6) (2026-07-13)

### Refactoring

#### web: Update server timeout configurations to match cloudrun timeouts ([7f1f95b](https://github.com/alis-build/adk-launchers-go/commit/7f1f95b1723f2898da8cb5b5e23418ac9e8a6613))

- Increased server timeout settings for write, read, idle, and shutdown operations to enhance performance and reliability under load.
- Adjusted write and read timeouts to 300 seconds, idle timeout to 300 seconds, and shutdown timeout to 30 seconds, ensuring better handling of long-running requests and graceful shutdowns.
- Reorganized import statements for clarity, moving the test environment import to a different position in the file.

### Other

#### deps: Update dependencies in go.mod and go.sum ([d2dabc2](https://github.com/alis-build/adk-launchers-go/commit/d2dabc25ecc07c12df2aabf49502517689bd3a3d))

- Upgraded `cloud.google.com/go/storage` to v1.63.1 and `github.com/ag-ui-protocol/ag-ui/sdks/community/go` to v0.0.0-20260712053254-4daca4f05164.
- Incremented `go.alis.build/common` to v1.1.11 and updated several indirect dependencies, including `opentelemetry-operations-go` components to their latest versions.
- Removed outdated entries from `go.sum` to maintain consistency with the updated dependencies.

## [1.0.5](https://github.com/alis-build/adk-launchers-go/compare/v1.0.4...v1.0.5) (2026-07-10)

### Features

#### evaluation: Implement overall rubric score aggregation for single-turn evaluations ([f43eed9](https://github.com/alis-build/adk-launchers-go/commit/f43eed9f7a78359d40110de4799ee17d94c74ac8))

- Added functionality to aggregate per-invocation rubric scores into overall scores for single-turn evaluations, ensuring accurate representation of evaluation results.
- Introduced tests to validate the new aggregation logic and ensure correct handling of nil scores and ordering of rubric entries.
- Enhanced existing evaluation metrics to include overall rubric scores, addressing previous issues with empty rubric arrays in evaluation results.
- Updated documentation to reflect changes in rubric evaluation processes and their implications for downstream consumers.

## [1.0.4](https://github.com/alis-build/adk-launchers-go/compare/v1.0.3...v1.0.4) (2026-07-10)

### Refactoring

#### evaluation: Update AppDetails handling and introduce rubric-based evaluation metrics ([255a4d6](https://github.com/alis-build/adk-launchers-go/commit/255a4d6235d12fb50fe07867b7290922b7235453))

- Changed the return type of GetAppDetailsByInvocationID to use *models.AppDetails instead of json.RawMessage for better type safety.
- Updated ConvertEventsToEvalInvocations and convertInvocationEvents functions to reflect the new AppDetails type.
- Introduced new metrics for rubric-based evaluations, including majority vote aggregation and parsing of rubric verdicts.
- Added comprehensive tests for the new rubric evaluation logic and updated existing tests to accommodate changes in AppDetails handling.
- Enhanced documentation to clarify the new evaluation metrics and their usage.

#### stream: Remove deprecated TextMessageStartEvent and update event emission logic ([ad61cb0](https://github.com/alis-build/adk-launchers-go/commit/ad61cb03274036ad9e478c1dbbede061c01d1bea))

- Deleted the TextMessageStartEvent shim, replacing it with direct usage of events.NewTextMessageStartEvent for improved clarity and maintainability.
- Updated the processor to trim whitespace from author names when emitting TEXT_MESSAGE_START events, ensuring correct handling of blank names.
- Revised documentation in doc.go to reflect changes in live SSE text streaming and author name handling.

## [1.0.3](https://github.com/alis-build/adk-launchers-go/compare/v1.0.2...v1.0.3) (2026-07-09)

### Other

#### deps: Update dependencies in go.mod and go.sum ([4d9fe86](https://github.com/alis-build/adk-launchers-go/commit/4d9fe86b52ebebe91bb4090fa763adb45ce24624))

- Upgraded `cloud.google.com/go/storage` to v1.63.0 and `github.com/ag-ui-protocol/ag-ui/sdks/community/go` to v0.0.0-20260706214105-ff60085fd16f.
- Updated `go.alis.build/agui/scheduler` to v0.1.1 and `go.alis.build/common` to v1.1.10.
- Incremented versions for several indirect dependencies, including `google.golang.org/api`, `google.golang.org/genai`, and `cloud.google.com/go/auth`.
- Adjusted versions for `github.com/googleapis/enterprise-certificate-proxy` and `github.com/googleapis/gax-go/v2`.
- Reflected changes in `go.sum` to ensure consistency with updated dependencies.

## [1.0.2](https://github.com/alis-build/adk-launchers-go/compare/v1.0.1...v1.0.2) (2026-07-09)

### Features

#### agui: Populate message name from ADK event author ([7a721dd](https://github.com/alis-build/adk-launchers-go/commit/7a721dd134c96829e45bb4570381649812c3954b))

Map session.Event.Author to types.Message.Name in history conversion
and emit name on live TEXT_MESSAGE_START via a temporary shim until
the AG-UI Go SDK supports it. Close open text streams on author change
so sub-agent attribution stays correct mid-run.

## [1.0.1](https://github.com/alis-build/adk-launchers-go/compare/v1.0.0...v1.0.1) (2026-07-07)

### Fixes

#### scheduler: Update cron failure handling and documentation ([57d54f7](https://github.com/alis-build/adk-launchers-go/commit/57d54f7a3e246ac9cbcf1cd6f2c9104a717feb99))

- Updated the `executeCron` function to persist `last_failure_time` and `last_failure_message` for TYPE_AT jobs on failure, ensuring accurate state management.
- Enhanced the handling of configuration errors to prevent archiving TYPE_AT jobs, maintaining visibility for operators.
- Revised documentation in `doc.go` to clarify the behavior of cron jobs regarding failure persistence and archival.
- Updated `go.mod` and `go.sum` to reflect the new version of the `common` dependency.
- Added unit tests to validate the new failure handling logic and ensure robust operation.

## [1.0.0](https://github.com/alis-build/adk-launchers-go/compare/v0.3.0...v1.0.0) (2026-07-07)

### Features

#### agui: Enhance AG-UI with executor configuration and internal refactoring ([56f5425](https://github.com/alis-build/adk-launchers-go/commit/56f5425261a12b3eed2ced4c236622e23616f449))

- Introduced `WithExecutor` option to allow custom agent executor configurations, enabling enhanced control over the execution pipeline.
- Refactored the executor logic to separate concerns, improving maintainability and extensibility of the AG-UI protocol.
- Updated documentation in `doc.go` and `README.md` to clarify the new executor patterns and usage examples.
- Added unit tests for the new executor functionalities, ensuring robust operation and error handling.
- Removed deprecated functions and cleaned up internal code structure for better clarity.

#### Update ADK references and dependencies for AG-UI enhancements ([3ff373d](https://github.com/alis-build/adk-launchers-go/commit/3ff373d326fe292366a649e1b0a5996d92b2d37d))

- Updated references from `google.golang.org/adk/cmd/launcher/web` to `google.golang.org/adk/v2/cmd/launcher/web` across documentation and code files to reflect the new versioning.
- Updated `go.mod` and `go.sum` to include new versions of dependencies, ensuring compatibility with the latest features and improvements.
- Enhanced documentation in `README.md` and `doc.go` to clarify the changes in ADK integration and usage patterns.
- Removed deprecated functions and cleaned up internal code structure for better clarity and maintainability.
- Added unit tests to validate the integration of new ADK features and ensure robust operation.

#### evals: Introduce dev evaluation API with HTTP routes and JSON handling ([86c09fe](https://github.com/alis-build/adk-launchers-go/commit/86c09fe4cb0585f9c6bdd2a35fb872b7bf919b08))

- Added a new `evals` sublauncher to provide a development-only evaluation HTTP API, mirroring adk-python DevServer endpoints for eval sets, running evaluations, and metrics information.
- Implemented JSON unmarshalling for both snake_case and camelCase request bodies to enhance compatibility with various client formats.
- Created comprehensive documentation in `doc.go` and `README.md` detailing the new API routes, usage patterns, and configuration options.
- Added unit tests to validate the functionality of the new evaluation API, ensuring robust operation and error handling.
- Updated `go.mod` and `go.sum` to include necessary dependencies for the new evaluation features.

#### scheduler: Transition from A2A to AG-UI scheduler with updated documentation and dependencies ([99d727f](https://github.com/alis-build/adk-launchers-go/commit/99d727fc43bdc4bba46e35e666e3d627a4225ae6))

- Updated the `scheduler` sublauncher to reflect the transition from A2A to AG-UI, including changes in HTTP routes and service paths.
- Revised documentation in `README.md` and `doc.go` to clarify the new scheduler integration and migration notes for users.
- Updated `go.mod` and `go.sum` to include new dependencies for the AG-UI scheduler and removed deprecated references.
- Enhanced the console application to support the new scheduler paths and updated asset management for improved performance.
- Added unit tests to validate the new scheduler functionalities and ensure robust operation.

## [0.3.0](https://github.com/alis-build/adk-launchers-go/compare/v0.2.2...v0.3.0) (2026-06-17)

### Features

#### agui: Enhance AG-UI with gRPC support and identity management ([0d4d8c7](https://github.com/alis-build/adk-launchers-go/commit/0d4d8c7091554e45369de8789688d95171ab4365))

- Introduced `WithGRPCRegistrar` option to register the `ThreadService` on the host gRPC server, enabling native gRPC and gRPC-Web clients.
- Updated handlers to utilize identity from the request context, ensuring proper authorization without authentication at the launcher level.
- Refactored existing endpoints to remove authentication requirements, replacing them with authorization checks based on the injected identity.
- Enhanced documentation in `doc.go` and `README.md` to clarify the new identity management and gRPC integration features.
- Added unit tests to validate gRPC registration and identity handling, ensuring robust operation and error management.
- Updated `go.mod` and `go.sum` to reflect new dependencies and versions related to gRPC functionalities.

## [0.2.2](https://github.com/alis-build/adk-launchers-go/compare/v0.2.1...v0.2.2) (2026-06-15)

### Features

#### console: Implement custom SPA distribution handling ([c95505f](https://github.com/alis-build/adk-launchers-go/commit/c95505fa43c3bbd6e896f2a26be2cc3fa91e563d))

- Introduced a new `DistResolver` interface to manage different methods of serving the SPA, including `DefaultDist`, `DirDist`, `EmbedDist`, and `HandlerDist`.
- Enhanced the `console` package to support serving custom production builds from specified directories or embedded files.
- Updated the `README.md` and `doc.go` to document the new distribution options and their usage.
- Added comprehensive unit tests to validate the functionality of the new distribution methods and ensure robust error handling.
- Refactored existing code to integrate the new distribution logic, improving maintainability and flexibility.

## [0.2.1](https://github.com/alis-build/adk-launchers-go/compare/v0.2.0...v0.2.1) (2026-06-08)

### Features

#### Introduce app name resolution and history JSON-RPC support in AG-UI ([6cfb261](https://github.com/alis-build/adk-launchers-go/commit/6cfb2612252e2f79310e48535fe59160bb43e7fe))

- Added `AppNameResolver` functionality to extract app names from `RunAgentInput`, enhancing flexibility in agent identification.
- Implemented `WithAppNameResolver` option for custom app name extraction, improving request handling for multi-agent scenarios.
- Registered history JSON-RPC endpoints for thread service, allowing clients to access historical data through a structured API.
- Updated `agui` package to integrate new app name resolution logic and history JSON-RPC registration, enhancing overall functionality.
- Enhanced documentation in `doc.go` to reflect new features and usage patterns for app name resolution and history handling.
- Added unit tests to validate app name resolution and history JSON-RPC functionalities, ensuring robust operation.

#### console: Implement console branding and asset management ([c8fe336](https://github.com/alis-build/adk-launchers-go/commit/c8fe336b4b9e7c3afb7437213c9da0687d12d464))

- Introduced a new `console` package to manage branding configurations for the web application, including title, display name, logo, and favicon.
- Added asset resolvers: `URLAsset`, `EmbedAsset`, and `DirAsset` for serving branding assets from various sources.
- Implemented `resolveBranding` function to resolve branding assets and generate URLs for runtime configuration.
- Created comprehensive unit tests to validate asset resolution and branding functionality, ensuring robust operation.
- Enhanced documentation in `doc.go` to detail the new branding features and usage patterns.

### Other

#### Add .gitignore file to exclude IDE and system files ([e563b4e](https://github.com/alis-build/adk-launchers-go/commit/e563b4e2b8cfbd07f4463591ace2058063be4423))

- Created a new `.gitignore` file to prevent tracking of IDE-specific directories (.vscode, .idea) and system files (.DS_Store).
- Ensured that the settings.json file in the .vscode directory is not ignored, allowing for shared configuration.

## [0.2.0](https://github.com/alis-build/adk-launchers-go/compare/v0.1.9...v0.2.0) (2026-06-05)

### Features

#### Enhance AG-UI with client-side tool support and predictive state management ([8a3f0e2](https://github.com/alis-build/adk-launchers-go/commit/8a3f0e23df8766f01481c0476372979d4b4aaf02))

- Introduced a new `clienttool` package to manage AG-UI client-defined tools, allowing dynamic tool definitions from the frontend.
- Implemented predictive state updates with `PredictStateMapping`, enabling real-time state previews for tool calls.
- Updated `agui` package to integrate client tools and predictive state features, enhancing user interaction and responsiveness.
- Enhanced documentation in `doc.go` and `README.md` to detail new functionalities and usage patterns for client tools and predictive state.
- Added unit tests to validate the new client tool functionalities and predictive state management, ensuring robust operation.
- Updated `go.mod` and `go.sum` to reflect new dependencies and versions related to the client tool implementation.

## [0.1.9](https://github.com/alis-build/adk-launchers-go/compare/v0.1.8...v0.1.9) (2026-06-04)

### Other

#### Enhance AG-UI interrupt handling ([ab9e8d3](https://github.com/alis-build/adk-launchers-go/commit/ab9e8d3796c5434741c3b0299361c6c40a39af0e))

- Updated `go.mod` and `go.sum` to reflect new versions of dependencies, including `google.golang.org/adk` and `google.golang.org/genai`.
- Enhanced interrupt handling in the `agui` package to support invocation ID persistence for resumed runs.
- Added new functions to manage invocation IDs from pending interrupts and client state, improving the robustness of the AG-UI protocol.
- Updated documentation in `doc.go` to detail changes in interrupt handling and invocation ID management.
- Added unit tests to validate new functionalities related to invocation ID resolution and interrupt processing.

## [0.1.8](https://github.com/alis-build/adk-launchers-go/compare/v0.1.7...v0.1.8) (2026-05-29)

### Features

#### Implement thread message handling in AG-UI protocol ([a9ea75d](https://github.com/alis-build/adk-launchers-go/commit/a9ea75d664bbafe50576798d573379b268ce8d95))

- Added support for retrieving conversation history via the new `/threads/{threadId}/messages` endpoint, allowing authenticated users to access their thread messages.
- Implemented JSON and SSE response formats for the thread messages, enhancing the user experience with real-time updates.
- Updated `agui` package to include necessary handlers and routing for thread message functionality.
- Enhanced documentation in `doc.go` to detail the new endpoint and its usage, including authentication requirements and response formats.
- Added comprehensive unit tests to validate thread message retrieval and error handling scenarios.

#### Enhance AG-UI with thread metadata and CORS improvements ([3b0e75a](https://github.com/alis-build/adk-launchers-go/commit/3b0e75a01d604bd9fef780f8dfd08fe5dfaef0a5))

- Introduced thread metadata handling with new endpoints for retrieving and deleting threads, enhancing user interaction with the AG-UI protocol.
- Updated CORS middleware to handle preflight requests more effectively and ensure proper header management for authenticated requests.
- Refactored existing handlers to utilize new function signatures, improving code clarity and maintainability.
- Enhanced documentation to reflect new features and usage patterns for thread management and CORS configuration.
- Updated dependencies in `go.mod` and `go.sum` to ensure compatibility with the latest versions of required packages.
- Added unit tests to validate new functionalities and ensure robust error handling in thread operations.

## [0.1.7](https://github.com/alis-build/adk-launchers-go/compare/v0.1.6...v0.1.7) (2026-05-27)

### Refactoring

#### Update scheduler launcher to support gRPC registration ([6e01771](https://github.com/alis-build/adk-launchers-go/commit/6e0177116a947cd36f7a45c9f88d74da57c87a1a))

- Modified the `scheduler.NewLauncher` function to accept the service name as the first argument, improving clarity in usage.
- Enhanced documentation in `doc.go` and `README.md` to reflect changes in gRPC registration and usage patterns.
- Added unit tests for the `WithGRPCRegistrar` option to ensure proper registration and handling of gRPC services.
- Updated the launcher implementation to register the SchedulerService with the provided gRPC server during setup.

## [0.1.6](https://github.com/alis-build/adk-launchers-go/compare/v0.1.5...v0.1.6) (2026-05-27)

### Other

#### Update dependencies and improve session ID handling in runtime ([3a48da6](https://github.com/alis-build/adk-launchers-go/commit/3a48da666519b1e6d681ae80555934213f5b9a77))

- Updated `go.mod` and `go.sum` to reflect new versions of dependencies, including `ag-ui` and Google API packages.
- Enhanced session ID generation in `RunSSE` method to ensure compatibility with Vertex AI memory bank by removing hyphens.
- Added comments to clarify the rationale behind session ID modifications and linked to relevant issue for future reference.

## [0.1.5](https://github.com/alis-build/adk-launchers-go/compare/v0.1.4...v0.1.5) (2026-05-26)

### Features

#### Add A2A scheduler extension for ADK web launcher ([f3c9f61](https://github.com/alis-build/adk-launchers-go/commit/f3c9f616a898fe92fc461a80be723e4e306c693c))

- Introduced the `scheduler` package as a new sublauncher for the A2A scheduler extension, enabling cron JSON-RPC and Cloud Tasks callbacks.
- Implemented HTTP handlers for cron CRUD operations and execution callbacks, integrating with the existing ADK web infrastructure.
- Enhanced documentation in `doc.go` and `README.md` to detail the new scheduler functionalities and usage examples.
- Updated `go.mod` and `go.sum` to include new dependencies for the scheduler extension and related services.
- Added unit tests to validate the scheduler's functionality and ensure robust operation within the ADK environment.

### Refactoring

#### Replace runner with adkrun runtime in AG-UI integration ([d8a0120](https://github.com/alis-build/adk-launchers-go/commit/d8a0120511cb90951fc381a7b25978d31b128b62))

- Updated `agui` package to replace the `runner` with the new `adkrun.Runtime` for agent execution.
- Modified `SetupSubrouters` and `runSSEHandler` methods to accommodate the new runtime structure.
- Enhanced session management to ensure compatibility with the new runtime's session handling.
- Updated documentation in `doc.go` to reflect changes in the launcher setup and runtime usage.
- Added unit tests for the `adkrun` package to validate the new runtime functionality and ensure stability.

## [0.1.4](https://github.com/alis-build/adk-launchers-go/compare/v0.1.3...v0.1.4) (2026-05-26)

### Features

#### Update AG-UI protocol integration with new multimodal input handling ([d9c54c5](https://github.com/alis-build/adk-launchers-go/commit/d9c54c5e94273c0a4b982530026381ec8339f69b))

- Enhanced `convertMultimodalInput` function to support both base64 data and URL sources for image inputs.
- Added tests for multimodal input scenarios in `agui_test.go` to ensure correct processing of source data and URLs.
- Introduced new session state management for pending interrupts, including validation and persistence mechanisms.
- Updated `go.mod` and `go.sum` to reflect changes in dependencies related to the AG-UI protocol.
- Added comprehensive documentation for new features and capabilities in `doc.go`.

## [0.1.3](https://github.com/alis-build/adk-launchers-go/compare/v0.1.2...v0.1.3) (2026-05-25)

### Features

#### Introduce ADK web launcher with sublauncher support ([ae2d55d](https://github.com/alis-build/adk-launchers-go/commit/ae2d55dd6f0db1dd7e302204fbd41f4588a062a0))

- Added `web` package to implement an ADK web launcher that integrates multiple sublaunchers using a shared gorilla/mux router.
- Implemented `Sublauncher` and `HostRouteSetup` interfaces for flexible route registration and host-level features.
- Created comprehensive documentation in `doc.go` to guide usage and configuration of the web launcher.
- Updated `go.mod` and `go.sum` to include new dependencies for the web package and related functionalities.

## [0.1.2](https://github.com/alis-build/adk-launchers-go/compare/v0.1.1...v0.1.2) (2026-05-20)

### Refactoring

#### Move launcherutils to internal package and update documentation ([36f99af](https://github.com/alis-build/adk-launchers-go/commit/36f99affcdf2c9feec319ff1ffd60dd2bd75c849))

- Relocated `launcherutils` to the `internal` directory to encapsulate shared helpers used by sublaunchers.
- Updated references in `agui` and `lro` packages to point to the new location of `launcherutils`.
- Revised documentation in `doc.go` and `README.md` to reflect the changes in package structure and clarify the purpose of `launcherutils`.

### Other

#### Enhance documentation for ADK launchers package ([08cea06](https://github.com/alis-build/adk-launchers-go/commit/08cea0664dce965baf4a8b8351fc12cb1433c0ad))

- Expanded `doc.go` to provide comprehensive details about the `launchers` package, including its purpose, subpackages, and usage examples.
- Documented the functionality of each sublauncher, such as `agui` and `lro`, along with configuration and runtime instructions.

## [0.1.1](https://github.com/alis-build/adk-launchers-go/compare/v0.1.0...v0.1.1) (2026-05-20)

### Features

#### Implement AG-UI protocol sublauncher for ADK agents ([4d6d693](https://github.com/alis-build/adk-launchers-go/commit/4d6d693c768d2317c08991de10eb4549bd1125cf))

- Added `agui` package to facilitate AG-UI protocol integration with ADK agents.
- Implemented core functionalities including CORS configuration, event processing, and session message conversion.
- Introduced tests for various components, ensuring robust functionality and adherence to expected behaviors.
- Updated `go.mod` and `go.sum` to include new dependencies for AG-UI SDK and logging.
- Created initial test files for `agui` to validate the implementation.

## 0.1.0 (2026-05-20)

