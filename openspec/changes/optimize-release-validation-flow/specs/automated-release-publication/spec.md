## MODIFIED Requirements

### Requirement: Explicit tag publication must authorize automatic release

The release tool SHALL keep tag publication as an explicit maintainer action.
After the exact annotated tag is transferred, the Release workflow SHALL run a
focused provenance gate that verifies valid release metadata, containment of
the peeled tag target in `main`, and successful push-triggered `CI` and
`Security Scan` runs for `main` at that exact SHA. It SHALL NOT repeat the
complete application matrix for the unchanged tree. `Build and publish` SHALL
start automatically only after provenance succeeds, and publish, monitor,
verify, and finalize MUST remain separately resumable.

#### Scenario: Reviewed release tag targets a proven main commit

- **WHEN** a maintainer explicitly publishes an eligible annotated custom tag
  whose exact target has successful required main workflows
- **THEN** the focused provenance gate SHALL succeed
- **THEN** `Build and publish` SHALL start automatically without repeating Go,
  frontend, lint, integration, deployment, or security application matrices

#### Scenario: Tag target lacks exact successful main evidence

- **WHEN** either required workflow is absent, belongs to another branch or
  SHA, is incomplete, or did not succeed
- **THEN** `Build and publish` MUST remain blocked
- **THEN** the workflow MUST NOT infer success from the tag-triggered event or
  from another commit
