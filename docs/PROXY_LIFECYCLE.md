# Proxy edits and observations across instances

An account's saved proxy binding controls each new provider operation. An
operation already in progress keeps its captured route and credential owner;
editing a proxy does not authorize an in-flight request to connect directly.
Explicitly removing the account binding authorizes later direct requests.

OpenAI quota, reset-credit and referral operations require the proxy snapshot
captured with the credential owner. A missing or invalid bound snapshot fails
before token acquisition or client creation; a later proxy-row lookup cannot
repair that operation or replace its route.

## Shared account and scheduler caches

Saving a proxy edit enqueues a scheduler outbox event for every non-deleted
account still bound to that proxy. The proxy update and event commit in the
same PostgreSQL transaction. An outbox storage failure rolls back the proxy
edit. This applies to all providers and account types, including accounts with
no stored connection or usage probe.

The production outbox consumer reloads affected accounts and rebuilds their
scheduler buckets in Redis. Other instances observe those shared updates and
their process-local snapshot revisions. Proxy name, expiry and egress metadata
are part of the cached proxy object, so edits to those fields also enqueue
invalidation even when the network endpoint remains unchanged. Propagation
uses the existing outbox polling interval; it is not synchronous across nodes.

Account pause, credential replacement, explicit unbinding and rebinding use the
account repository's transactional invalidation path.

Grok OAuth credential acquisition has separate permanent-error and temporary
quarantine writes before inference begins. Both compare the captured login and
the proxy's protocol, address, authentication, status and expiry while locking
that proxy row. A same-ID proxy edit cannot authorize an old failure against the
new route. The missing-proxy error also locks a soft-deleted row before checking
its state, so a concurrent restoration is rechecked after the lock wait. Proxy
display-name changes do not change the failure's route owner.

These quarantine writes commit their account state and scheduler outbox event
together. A caller-owned transaction retains control of visibility and rollback;
managed transactions publish the scheduler snapshot only after commit. An outbox
failure leaves the account and cache unchanged. A recovery-generation change
alone does not erase a real credential failure committed after that recovery.

## Expiry, fallback and restoration

Expiry applies only to active proxies whose saved expiry has been reached.
`none` marks the proxy expired while retaining its account bindings; `direct`
clears each binding; `proxy` moves accounts to the first healthy active backup
in the configured chain. Disabled or expired backup nodes are skipped according
to their own fallback rules. Cycles, missing targets and unresolved chains keep
the original binding. The gateway does not interpret a status or expiry edit as
permission to choose a different route during an in-flight operation.

The sweep locks current proxy rows in ID order before deciding. A renewal,
disable or backup edit committed before those locks are acquired is rechecked;
the former scan cannot override a completed renewal. Expired status, affected
account bindings and scheduler invalidation commit in one transaction. Even
`none` and unresolved fallback invalidate bound account snapshots because the
cached proxy's status changed. Repeating a completed sweep changes no binding.

The first fallback origin is retained across repeated expiry chains. After an
administrator repairs the original route, manual restoration returns to that
origin and clears the recovery marker. Restoration and its scheduler outbox
event commit together; storage failure preserves the old route and recovery
marker. These saved-binding workflows allow later operations to see the new
route without changing an earlier operation's captured route.

## Deletion and concurrent binding

Proxy deletion rechecks live account bindings while holding the proxy row lock.
The admin service's initial count is advisory. Account creation, ordinary edits,
bulk proxy assignment and fallback restoration lock and validate their target
proxy before committing its binding. A concurrent soft deletion therefore
cannot leave a new account bound to a deleted route merely because the SQL
foreign key still accepts the historical row. An in-use proxy returns the
existing `PROXY_IN_USE` error; a deleted target returns `PROXY_NOT_FOUND`.

Account deletion commits the account, group/scheduled-test cleanup and scheduler
event together. Outbox failure leaves the account and caches intact. A caller's
transaction controls visibility and rollback; cache deletion occurs only after
the owning transaction commits. Recreating an account's display name creates a
distinct credential owner and cannot revive the deleted owner's cached route.

The OpenAI Spark credential family is one management unit. Deleting its parent
commits all supported shadow deletions together. Restoring the parent's original
proxy commits the parent's recovery marker and every inherited shadow binding
together. A failed child or parent write preserves the entire prior family and
publishes no intermediate cache changes.

Family deletion locks the live parent before enumerating its shadows. Both
account creation entrypoints lock and recheck a live parent before inserting a
credential-dependent child. A creation committed while deletion waits is
included in the final scan; a creation arriving after that scan waits for the
deletion and fails once the parent is deleted. The historical foreign-key row
never authorizes a new shadow, and rejected creation publishes no child event.

Changing an OpenAI OAuth parent's account type uses the same parent lock and
rechecks live shadows in a fresh database statement after any wait. Shadow
creation also rechecks that the locked parent still has the required OpenAI
OAuth type. Either operation can commit first; a committed shadow prevents type
conversion, and a committed conversion prevents the new shadow. Rolling back
either operation leaves the other valid operation available.

## Administrative recovery

Resetting account state atomically activates the account and clears its error,
account rate/overload limits, temporary cooldown, persisted model rate limits and
Antigravity quota scopes with one scheduler event. Saved credentials, proxy,
manual scheduling permission, account expiration and unrelated metadata retain
their values. The response snapshot is read within the same transaction. A
storage or outbox failure retains the complete prior state and its caches.

Runtime recovery follows the committing transaction. A caller-owned transaction
publishes runtime recovery only after its commit succeeds. Grok account resets
retire that account's local quota and transient model blocks across historical
credential owners. Shared team quota observations continue to apply to the real
team and its sibling accounts.

Grok and OpenAI API Key resets also commit a new repository-managed runtime
recovery generation in the account's metadata. The scheduler snapshot carries
that generation across instances, so a local block from before the reset no
longer applies after the saved reset reaches another instance. A new real
failure can still establish a new block. Grok request outcomes return their
generation from the same SQL mutation and outbox statement; a later reset cannot
cause an earlier outcome to publish under the reset's new generation.

Grok OAuth reauthorization advances the same generation with its successful
credential recovery. Ordinary refresh keeps the generation. Imports, account
edits, metadata patches and bulk edits cannot set or erase this managed value;
they retain the currently stored generation, including after a concurrent reset.
Failed or rolled-back recovery retains the former generation and cache state.

The public rate-limit reset clears account rate and overload state, quota scopes
and temporary/model cooldowns together. The public temporary-cooldown reset
clears temporary and model cooldowns while retaining rate, overload and quota
scope state. Both preserve account status, error text, manual scheduling
permission and expiration; their state changes and one scheduler event commit
atomically. Storage failure cannot expose an earlier step of a partial reset.

Successful-test recovery clears an error only when the saved account is in the
error state, and clears runtime limits only when recoverable limits exist. A
disabled account stays disabled. A no-op recovery writes no scheduler event and
does not advance the runtime generation. Token invalidation, temporary-state
cache deletion and runtime recovery publish only after the owning transaction
commits. Local cleanup retires observations captured before the reset; a newly
observed failure after that reset remains effective.

## Connection and quality observations

Connection tests, quality checks and automatic creation probes keep the saved
proxy snapshot captured before their network operation. Redis observation
keys contain the proxy ID and a SHA-256 digest of that snapshot, including its
saved update time. Credentials are included only inside the digest input;
neither cache keys nor logs expose those values. Timestamps are normalized to
PostgreSQL's microsecond storage precision so a creation probe and another
instance reading the saved row resolve the same generation.

Lists attach observations only for the currently loaded proxy snapshot.
Editing a proxy therefore clears its displayed observations until the edited
generation has been tested. A delayed success, failure or quality result can
write only its original generation, preserving any newer generation's result.
An old result cannot become current again after the proxy is edited back to
the original endpoint. Observation generations expire 24 hours after their
last write. New writes remove the former ID-only cache entry, whose route
ownership cannot be established.

The proxy management page applies the same ownership rule to pending requests
and displayed results. Single and batch tests share an in-progress operation
only for the same proxy ID and saved snapshot. Editing that proxy permits a
new test for its new snapshot. A late completion from the previous snapshot
cannot publish results or clear the new operation's pending state. Connectivity
and quality tests each retain their own operation ownership.

## Regression evidence

`TestSevenProviderManagementEditsReachOtherInstanceBeforeForwarding` exercises
seven API Key Chat providers through real PostgreSQL repositories, the
production outbox consumer, separate Redis clients and scheduler caches, the
production selector/forwarder and actual HTTP proxies. It checks changed
route/authentication, account credentials, pause, direct unbinding, deletion
and recreation/rebinding, as well as rejection of a stale owner observation.

`TestSevenProviderManagementProxyEditOutboxFailureRollsBackRoute` injects an
actual PostgreSQL outbox write failure and checks that the proxy edit rolls
back. `TestSevenProviderManagementLateProxyProbeCannotReplaceRepairedRoute`
uses the production network prober and two admin instances to check delayed
success, failure and quality results. The separate creation-probe positive
control checks cross-instance visibility after PostgreSQL timestamp storage.

`TestSevenProviderExpiredProxyFallbackAndRepairReachOtherInstance` covers seven
providers and three expiry modes through real PostgreSQL/Redis and proxy sends,
including repeated sweeps and repair/restoration. The renewal race and actual
outbox storage-failure tests check lock revalidation and atomicity. The deletion
regressions cover an old admin preflight, all four shared binding writers,
caller rollback, storage failure and seven real delete/recreate forwarding
chains. These management forwarding chains use API Key Chat accounts.

These tests do not compose every native OAuth inference path with every
management operation. The [seven-provider matrix](SEVEN_PROVIDER_TEST_MATRIX.md)
records the other protocol and browser coverage and its remaining boundaries.
