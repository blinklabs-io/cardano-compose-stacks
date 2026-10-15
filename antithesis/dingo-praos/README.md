# Dingo Praos Antithesis Harness

This five pool testnet runs one Antithesis-instrumented Dingo producer with
four cardano-node producers. `testnets/dingo-praos/` is the Moog testnet
definition; the surrounding files provide the pool configurator, transaction
workload, and safety/liveness analyzer.

## Images

The checked-in stack uses the published `main` images:

- `ghcr.io/blinklabs-io/dingo:main-antithesis`
- `ghcr.io/blinklabs-io/dingo-configurator:main`
- `ghcr.io/blinklabs-io/cardano-txpump:main`
- `ghcr.io/blinklabs-io/dingo-analysis:main`

The Dingo repository publishes its instrumented image. This repository builds
the configurator and analyzer in `.github/workflows/antithesis.yml`; the
separate `cardano-txpump` repository publishes the workload image.

## Post-fault convergence check

The analysis image carries `eventually_converged.sh` under
`/opt/antithesis/test/v1/dingo-praos/`. Antithesis runs it after fault
injection stops. It waits briefly, then runs `analysis eventually`, which
reads the node logs once, derives each node's final tip from them, and checks
that every tip is within `ANALYSIS_CONVERGENCE_WINDOW` slots (default 10) of
the longest tip. The command reads the same `ANALYSIS_*` environment as the
daemon, so it uses the same log directory.

What the result means:

- Tips come only from the logs. A node that logged no chain event is not
  observed.
- With fewer than two observed nodes no convergence assertion is emitted, only
  a reachability marker saying the check ran but could not compare.
- With two or more observed nodes, a configured node (`p1` to `pN`) that has no
  tip fails the assertion.
- Chain identity is checked by comparing block hashes at the lagging node's tip
  slot. Where a hash is missing, or the longest chain has no block at that
  slot, the node passes on slot distance alone and the result is marked hash
  unverified; this is not a failed or confirmed identity check.
- Hash prefixes shorter than 8 characters never match.
- If the command times out or exits abnormally before emitting an assertion,
  the wrapper script records a reachability marker with the exit code and
  still exits 0.

## Local Compose run

From this directory:

```sh
./scripts/validate-config.sh
docker compose up -d
docker compose logs -f
docker compose down -v
```

The local compose file uses the Dingo `main-antithesis` image and the
`cardano-txpump:main` image. The Moog definition is under
`testnets/dingo-praos/` and adds fault handling and log analysis.

The published Antithesis helper images support `linux/amd64` and `linux/arm64`.
Docker Compose selects the host architecture when it pulls multi-architecture
images, so the local harness can run natively on ARM64 once the Dingo and
txpump image manifests include that platform.

The Moog definition selects `linux/amd64` and uses `pull_policy: never` for
every service. Antithesis imports the images before boot; its offline test
environment must use those images without contacting the registry.

## Moog workflow

The scheduled and manually dispatched workflow runs from this repository's
`main` branch. Before submission it resolves Dingo's `main-antithesis` image to
an immutable OCI digest, records the image's Dingo source revision in an
automation-branch commit, and submits that commit to Moog. It also publishes
the configurator and analyzer as `main` images. Configure
`MOOG_REQUESTER_WALLET` as a repository secret and set `MOOG_REQUESTER` as a
repository variable. `MOOG_MPFS_HOST` and `MOOG_TOKEN_ID` are optional.
