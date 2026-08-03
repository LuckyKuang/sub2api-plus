## ADDED Requirements

### Requirement: Backend source identifies the Plus repository

The backend Go module declaration and all internal Go package imports SHALL use
`github.com/LuckyKuang/sub2api-plus` as their module prefix.

#### Scenario: Building from a Plus checkout

- **WHEN** a maintainer runs Go tooling from the `backend` directory of the
  Plus repository
- **THEN** all first-party Go packages resolve under the Plus module prefix

### Requirement: Generated Go output follows the module identity

Ent and Wire generated Go files SHALL be regenerated after the module-path
change and SHALL not retain the former upstream module prefix.

#### Scenario: Regenerating backend code

- **WHEN** a maintainer runs the repository's Ent and Wire generation commands
- **THEN** generated package imports resolve under the Plus module prefix

### Requirement: Official upstream provenance remains explicit

The repository SHALL preserve `github.com/Wei-Shaw/sub2api` where it identifies
the official upstream source rather than a Plus source package or distribution
location.

#### Scenario: Maintaining upstream mapping

- **WHEN** a maintainer reads `UPSTREAM.md`
- **THEN** it identifies `github.com/Wei-Shaw/sub2api` as the official upstream
  and `github.com/LuckyKuang/sub2api-plus` as the Plus repository
