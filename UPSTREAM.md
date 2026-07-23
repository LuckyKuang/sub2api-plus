# Upstream Mapping

| Custom Release | Official Sub2API Release | Official Commit |
| --- | --- | --- |
| `v1.1.164` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` |

## Repository Topology

- `main`: custom development branch
- `upstream`: `https://github.com/Wei-Shaw/sub2api.git`
- `upstream/v0.1.164`: imported official baseline tag

## Upgrade Workflow

1. Fetch the target official release from `upstream`.
2. Merge the official release into `main`.
3. Resolve only intentional custom conflicts.
4. Update this mapping and version metadata.
5. Run backend and frontend checks, then create the corresponding `v1.*` release tag.
